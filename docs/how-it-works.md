# How it works: full anatomy for developers

This document walks the implementation function-by-function, with file references, so a developer can predict exactly what happens for a given call without reading every package from scratch. [Project explainer and review guide](project-explainer.md) covers guarantees, risk, and review questions; this document covers *where the code lives and what it does, in order*.

Everything here reflects the repository state on 2026-07-09.

## Package map

| Path | Role |
|---|---|
| `audit.go`, `context.go`, `model.go`, `plugin.go` (module root) | Original GORM plugin. No pluggable store; kept for compatibility. |
| `postgres-gorm/plugin.go` | Preferred GORM plugin: callback registration and dispatch. |
| `postgres-gorm/store.go` | `AuditStore` interface + default `GormStore` (SQL persistence, atomic versions). |
| `postgres-gorm/audit.go` | `Audit`/`AuditVersion` GORM models, `Migrate`/`MigrateWithTable`. |
| `postgres-gorm/context.go` | `WithUserID`/`WithComment`/`ExtractUserID` for this package. |
| `mongodb/collection.go` | `Auditor` + `AuditableCollection`: wraps `*mongo.Collection`, dispatches per-method. |
| `mongodb/diff.go` | `ChangeComputer`, `DiffLimits`, complexity checks, default diff algorithm. |
| `mongodb/store.go` | `MongoStore`: implements `postgres-gorm`'s `AuditStore` over MongoDB, for GORM-to-Mongo setups. |
| `mongodb/audit.go`, `mongodb/model.go`, `mongodb/context.go` | Audit document shape, base `Model`, context helpers. |
| `mongodbv2/*` | Same design as `mongodb/*`, built against MongoDB Go driver v2 types. |
| `example/`, `example/mongodb/` | Compiled usage demonstrations exercised by `go test ./...`. |

`mongodb` and `mongodbv2` are structurally identical — read one and you understand the other, but never mix their types.

## GORM path: anatomy of a call

### Registration

`postgres-gorm/plugin.go`, `New()` builds a `*plugin` holding `Config`, the audit table name, and a skip-set. `Initialize(db)` runs once, at `db.Use(...)` time, and registers five GORM callbacks:

```text
Create  .After("gorm:create")   -> afterCreate
Update  .Before("gorm:update")  -> beforeUpdate
Update  .After("gorm:update")   -> afterUpdate
Delete  .Before("gorm:delete")  -> beforeDelete
Delete  .After("gorm:delete")   -> afterDelete
```

It also decides the `AuditStore`: `p.config.Store` if supplied, otherwise `NewGormStore(db, tableName)` (`postgres-gorm/store.go`).

### Create

```text
afterCreate(db)
  -> audit(db, ActionCreate)
       shouldSkip? (audit table itself, or db.Error/RowsAffected==0) -> return
       resolveUserID / resolveComment  (context helpers)
       ReflectValue is Slice/Array -> auditSingle() per element
       ReflectValue is Struct      -> auditSingle() once
            -> walk schema.Fields, skip CreatedAt/UpdatedAt/DeletedAt
            -> includeField(tag, onlyMode) decides inclusion (see "Field tags" below)
            -> record [nil, value], or [nil, RedactedValue] for auditable:"redact"
            -> nextVersion(db, type, id)  -- delegates to store.NextVersion
            -> persist(db, audit)         -- delegates to store.Save, then OnAudit
```

### Update

Two callbacks cooperate because the "before" snapshot has to be read *before* GORM overwrites the row, but the diff can only be computed *after* GORM reports what changed:

```text
beforeUpdate(db)
  -> pkStr(db, model) == "" ?
       AllowUnauditedBulk? no  -> db.AddError(ErrBulkMutationUnsupported), abort
       AllowUnauditedBulk? yes -> proceed unaudited
  -> otherwise: First(freshStructPtr, pk) on a brand-new session
  -> store the loaded struct in db.Statement.Context under oldValuesKey{}

afterUpdate(db)
  -> audit(db, ActionUpdate)
       db.Statement.Dest is a map[string]interface{}?
         yes -> auditFromMap    (db.Model(&m).Updates(map{...}))
         no  -> auditFromChangedCols (db.Model(&m).Update(...), .Updates(struct), or .Save(&m))
```

`auditFromMap` reads the pre-update snapshot back out of the context, looks up each map key's schema field for its `auditable` tag, and records `[oldValue, newValue]` per key (or `[Redacted, Redacted]`).

`auditFromChangedCols` walks every schema field and includes it when either GORM's own `db.Statement.Changed(f.Name)` says it changed, *or* — because `Save(&m)` sets `Model == Dest` and `Changed()` always reports false in that case — the field's new value differs from the pre-update snapshot via `reflect.DeepEqual`. This fallback is what makes `db.Save(&m)` produce a correct diff instead of an empty one.

### Delete

```text
beforeDelete(db)
  -> pkStr(db, model) == "" && !AllowUnauditedBulk -> db.AddError(ErrBulkMutationUnsupported)

afterDelete(db)
  -> audit(db, ActionDelete) -> auditSingle(..., ActionDelete)
       records [value, nil] per included field
```

### Bulk-mutation guard

`pkStr(db, rv)` (`postgres-gorm/plugin.go`) returns the string form of the model's first non-zero primary key, or `""`. A conditional statement like `db.Model(&User{}).Where("...").Updates(...)` has no populated struct primary key, so `pkStr` returns `""` and the operation is rejected with `ErrBulkMutationUnsupported` unless `Config.AllowUnauditedBulk` is set. This is what turns "a bulk write silently produced zero audit rows" into a visible, opt-out-only error.

### Field tags: `includeField` and `schemaOnlyMode`

`schemaOnlyMode(stmt)` scans `stmt.Schema.Fields` once per call and returns `true` if any field carries `auditable:"only"` or `auditable:"true"` — that flips the field set from "everything except excluded" to "nothing except whitelisted."

`includeField(tag, onlyMode)`:

```text
tag == "-" or "false"              -> false, always
onlyMode && tag not in {only,true,redact} -> false
otherwise                          -> true
```

`redact` fields are always included even in whitelist mode, with the value replaced by `Config.RedactedValue`.

### Persistence and versions

`postgres-gorm/store.go` defines the seam:

```go
type AuditStore interface {
    Save(ctx context.Context, r *AuditRecord) error
    NextVersion(ctx context.Context, auditableType, auditableID string) (uint64, error)
}
```

The default `GormStore.NextVersion` hashes `type + "\x00" + id` with SHA-256, then does an atomic upsert against `<table>_versions`:

```sql
INSERT INTO audit_versions (key, version) VALUES (?, 1)
ON CONFLICT (key) DO UPDATE SET version = version + 1
RETURNING version
```

`GormStore.Save`/`NextVersion` both call `s.conn(ctx)`, which looks for a `*gorm.DB` stashed in `ctx` under `gormTxKey{}` (put there by `plugin.persist`/`plugin.nextVersion` via `withGormTx`). If present, it starts a new statement session *from that transaction connection* — this is the mechanism that lets the audit row commit or roll back atomically with the business row when both use the default `GormStore`. A custom cross-database `Store` (e.g. `mongoaudit.MongoStore`) cannot participate in that transaction; see [Custom diffs and large documents](custom-diff-and-large-documents.md) and the consistency notes in [Project explainer](project-explainer.md).

`OnAudit` runs through `invokeAuditCallback`, which recovers a panic and converts it to an error so a bad callback can't crash the request goroutine; `handleError` similarly recovers a panic thrown by `OnError` itself. The module-root plugin (`plugin.go`) does **not** recover `OnError` panics — that's a real behavioral difference between the two GORM plugins, not just a naming one.

## MongoDB path: anatomy of a call

### Wrapping

`mongodb/collection.go`:

- `NewAuditor(db, auditCollectionName, cfg)` opens the audit collection and its `_versions` sibling, normalizes `Config` (default redaction string, merged skip fields, default `ChangeComputer`, default `DiffLimits`).
- `Auditor.Collection(name)` wraps every operation.
- `Auditor.CollectionFor(name, &Struct{})` additionally reads a CRUD-scoped tag from the embedded model field — `auditable:"create,update"` — via `auditTagsFromStruct`/`collectStructTags`/`parseOpsTag`, so (for example) deletes on that collection are silently left unaudited.
- `Auditor.Wrap(existingColl)` reuses an already-obtained `*mongo.Collection` handle.

`structAuditInfo` (redacted/skip/only/ops field sets, parsed from `bson`+`auditable` struct tags) is computed once per Go type and cached in `auditInfoCache` (a `sync.Map` keyed by `reflect.Type`), so repeated calls don't re-walk struct tags.

### Insert

```text
InsertOne(ctx, doc)
  -> setDocumentTimestamps(doc)   // fills CreatedAt/UpdatedAt if zero
  -> real coll.InsertOne
  -> allowsOp(ActionCreate)?      // false only via CollectionFor's ops tag
  -> toMap(doc) via BSON round-trip
  -> writeAudit(id, ActionCreate, ChangeCreate, nil, docMap, nil, tags)
```

`InsertMany` is the same per document; a partial unordered-insert failure (some documents inserted, an error returned) still gets to `return result, err` — the wrapper returns immediately and audits nothing for that call, which is a known best-effort gap (see [Project explainer](project-explainer.md)).

### Update

```text
UpdateOne(ctx, filter, update)
  -> FindOne with projection {_id: 1}     // cheap ID resolution only, no full document read
  -> real coll.UpdateOne
  -> MatchedCount == 0 ? return, no audit
  -> writeAudit(id, ActionUpdate, ChangeOperators, nil, nil, update, {})
```

The changes recorded come from `extractUpdateChanges` (`mongodb/diff.go`), which parses the update document's operators directly — no document read of the actual before/after state:

```text
$set   k:v      -> changes[k] = [nil, v]
$unset k        -> changes[k] = [nil, nil]
anything else   -> changes[k] = [nil, {"$op": v}]     // requested intent, not resulting value
```

`UpdateMany` does the same but first collects every matching `_id` (projection-only `Find`), then writes one audit event per pre-collected ID using the *same* requested-change payload — matched-but-unchanged documents still get an event, and documents that start matching between the pre-read and the mutation are missed.

### Replace / delete

```text
ReplaceOne(ctx, filter, replacement)
  -> findOne(filter)              // full document read, before the mutation
  -> real coll.ReplaceOne
  -> diffDocs(oldDoc, newDoc, ...)  // full field-by-field before/after comparison
  -> writeAudit(id, ActionUpdate, ChangeReplace, oldDoc, newDoc, nil, tags)

DeleteOne / DeleteMany
  -> findOne / findMany            // full document read(s), before the mutation
  -> real delete
  -> deleteChanges(oldDoc, ...)    // [value, nil] per field
  -> writeAudit(..., ActionDelete, ChangeDelete, oldDoc, nil, nil, {})
```

Because the read and the mutation are two separate driver calls, a concurrent write between them can make the audited snapshot differ from what was actually replaced or deleted — use a MongoDB session transaction when that race matters.

`SoftDelete`/`SoftDeleteMany`/`Restore` are thin wrappers around `UpdateOne`/`UpdateMany` with a `$set`/`$unset` on `deleted_at` — they are recorded with `ActionUpdate`, never `ActionDelete`. `NotDeleted(filter)` ANDs a caller filter with `{deleted_at: {$exists: false}}`; nothing in the library applies it automatically.

### Change computation pipeline

Every path above funnels through `AuditableCollection.computeChanges` (`mongodb/collection.go`), which is the seam described in depth in [Custom diffs and large documents](custom-diff-and-large-documents.md):

```text
computeChanges(...)
  -> requestComplexity(req, limits)     // byte size + depth check on Before/After
       over limit -> deferChanges(...)  // sanitize + Config.DeferredChanges.Defer, or bubble the error
  -> ctx with ComputeTimeout deadline
  -> Config.ChangeComputer.Compute(ctx, req)   // defaultChangeComputer unless overridden
  -> result too big (MaxChangedFields) -> deferChanges(...)
  -> Status defaults to DiffInline
```

`defaultChangeComputer.Compute` just dispatches on `req.Mode` to `createChanges` / `extractUpdateChanges` / `diffDocs` / `deleteChanges` — the same four functions described above.

### Versions

`AuditableCollection.nextVersion` (and `MongoStore.NextVersion` in `mongodb/store.go`, used when MongoDB is the audit backend for the *GORM* plugin) both hash `type + "\x00" + id` with SHA-256 and run:

```text
versionColl.FindOneAndUpdate(
    {_id: key}, {$inc: {version: 1}},
    upsert: true, returnDocument: After,
)
```

`AuditableCollection.nextVersion` retries once on a duplicate-key error from a racing first upsert; `MongoStore.NextVersion` does not retry.

## Context propagation anatomy

Every package (`auditable` at the module root, `auditablegorm` in `postgres-gorm`, `mongoaudit` in both `mongodb` and `mongodbv2`) defines its **own** `WithUserID`/`WithComment`/`UserIDFromContext`/`CommentFromContext`, each keyed by an unexported `contextKey` type local to that package. A value stored with the root package's `WithUserID` is invisible to `postgres-gorm`'s resolver, and vice versa, because Go context lookups compare key *type* as well as value — this is intentional isolation, not a bug, but it means: **use the context helper from the same package as the plugin/auditor you registered**, or wire `Config.UserIDResolver` (GORM) / `Config.UserIDResolver` (Mongo) directly to your own auth middleware's context key via `ExtractUserID`.

`ExtractUserID`/the internal `userIDFromContext`/`valueFromContext` all accept `string`, `fmt.Stringer`, `[]byte`, or fall back to `fmt.Sprint` — so an integer user ID from an existing auth context works without a manual conversion.

## File-by-file quick reference

| File | Exported surface worth knowing |
|---|---|
| `plugin.go` | `Config`, `New`, module-root plugin (`Name() == "auditable"`) |
| `audit.go` | `Action`, `Audit` (module-root shape, no `Comment` field variant differences vs. `postgres-gorm`) |
| `context.go` | `WithUserID`, `UserIDFromContext`, `WithComment`, `CommentFromContext` |
| `model.go` | `Model` (UUID `BeforeCreate` hook) |
| `postgres-gorm/plugin.go` | `Config`, `New`, `ErrBulkMutationUnsupported`, callback dispatch (`Name() == "auditablegorm"`) |
| `postgres-gorm/store.go` | `AuditRecord`, `AuditStore`, `GormStore`, `NewGormStore`, `withGormTx` |
| `postgres-gorm/audit.go` | `Model`, `Action`, `Audit`, `AuditVersion`, `Migrate`, `MigrateWithTable` |
| `postgres-gorm/context.go` | Same helper shape as root, plus `ExtractUserID` |
| `mongodb/collection.go` | `Config`, `Auditor`, `NewAuditor`, `AuditableCollection`, `Wrap`, `EnsureAuditIndexes`, `NotDeleted` |
| `mongodb/diff.go` | `ChangeComputer`, `ChangeComputerFunc`, `DeferredChangeHandler`, `DiffLimits`, `ErrChangeTooComplex` |
| `mongodb/store.go` | `MongoStore` — implements `postgres-gorm.AuditStore` over MongoDB |
| `mongodb/audit.go` | `Audit` document shape (`ChangesRef`, `DiffStatus` for the deferred-diff path) |
| `mongodb/model.go` | `Model` (MongoDB-native base fields) |
| `mongodb/context.go` | Same helper shape as `postgres-gorm/context.go` |
| `mongodbv2/*` | Identical surface to `mongodb/*`, MongoDB driver v2 types |

## Developer runbook: where to look

| Question | Start here |
|---|---|
| A field isn't showing up in the audit log | `includeField`/`schemaOnlyMode` (GORM) or `collectStructTags`/`includedField` (Mongo) |
| An update recorded the wrong (or no) old value | `beforeUpdate`/`oldValuesKey` (GORM); Mongo `UpdateOne`/`UpdateMany` never read the old document at all — only `ReplaceOne`/`DeleteOne`/`DeleteMany` do |
| A bulk write raised `ErrBulkMutationUnsupported` | `pkStr` returned `""`; load and mutate rows individually, or set `AllowUnauditedBulk` |
| I need audit rows in a different store | Implement `postgres-gorm.AuditStore` (see `mongodb/store.go`'s `MongoStore` as a working example) |
| I need a custom diff algorithm or to handle huge documents | `Config.ChangeComputer` / `Config.DeferredChanges` in `mongodb/diff.go`, detailed in [Custom diffs and large documents](custom-diff-and-large-documents.md) |
| `OnAudit`/`OnError` panicked | `invokeAuditCallback` recovers `OnAudit` panics everywhere; `handleError` recovers `OnError` panics in `postgres-gorm` and `mongodb`/`mongodbv2`, but **not** in the module-root plugin |
| My actor/comment isn't visible to the plugin | Context helpers are package-scoped — see "Context propagation anatomy" above |
