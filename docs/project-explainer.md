# `auditable_go`: architecture, behavior, review guide, and improvement plan

This document explains why the project exists, how the current implementation works, what it can and cannot guarantee, and what the team should resolve before treating it as a production audit system.

It describes the repository state inspected on 2026-07-08. It is intentionally based on the implementation, not only on the claims in the README.

## Executive summary

`auditable_go` is an application-level change logging library. It observes writes made through supported GORM or MongoDB APIs and stores an audit event containing the actor, action, entity, changed fields, version, comment, and timestamp.

The central design is sound for operational history:

- GORM integration is low-touch because callbacks observe model writes globally.
- MongoDB integration is explicit because callers use a wrapped collection.
- Context carries request-specific actor and comment metadata.
- Field tags and configuration provide exclusion and redaction.
- A storage interface allows GORM changes to be audited in SQL or MongoDB.

The current code is not a complete compliance-grade or event-sourcing system. Important limitations are:

- Database-backed transaction, concurrency, and failure-injection coverage still needs to be expanded beyond the current unit suite.
- Audit failures are fail-open by default; `FailOnAuditError` must be enabled deliberately when caller-visible errors are required.
- GORM bulk updates/deletes are rejected by default rather than audited per affected row.
- MongoDB pre-read and write operations can race, and audit writes are not atomic unless the caller supplies an appropriate transaction.
- Writes that bypass GORM callbacks or the MongoDB wrapper are invisible.

Use the project today as a best-effort application audit trail after accepting those constraints. Resolve the P0 items in the improvement plan before claiming complete, tamper-resistant, or compliance-grade auditing.

## Why this project exists

Normal application tables answer what the current state is. They usually cannot answer:

- Who changed it?
- What operation was performed?
- What was requested before and after the change?
- Why was the change made?
- In what order did changes occur?
- Which record did an incident or support case affect?

This library creates a separate, append-oriented history so those questions can be investigated without putting audit code in every service method.

This is useful for debugging, customer support, administrative activity history, internal controls, and incident investigations. It is only one part of a compliance control: access restrictions, retention, tamper protection, monitoring, backup, and evidence that all write paths are covered are still required.

## Repository and package map

| Package | Role | Current recommendation |
|---|---|---|
| Module root, `github.com/ivikasavnish/auditable_go/v5` | Earlier GORM plugin retained for compatibility. It has no pluggable store and records update old values as `nil`. | Keep for existing consumers; do not choose it for a new integration. |
| `postgres-gorm` | More capable GORM plugin with pre-update snapshots and an `AuditStore` interface. Despite its name, the callback layer is GORM-oriented rather than PostgreSQL-only. | Preferred GORM implementation. |
| `mongodb` | MongoDB collection wrapper and GORM `MongoStore` for `go.mongodb.org/mongo-driver` v1. | Use only with MongoDB driver v1. |
| `mongodbv2` | Equivalent wrapper and store for `go.mongodb.org/mongo-driver/v2`. | Use with MongoDB driver v2. |
| `example` | GORM usage demonstration. | Useful as a scenario catalog, not as verification. |
| `example/mongodb` | MongoDB v2 demonstration. | Compiles as part of `go test ./...`. |

The duplicate MongoDB packages are compatibility layers for two major driver APIs. Their logic is otherwise nearly identical. Documentation and examples must never mix `mongodb` with v2 driver types or `mongodbv2` with v1 driver types.

Embedding the supplied `Model` is a convenience for IDs and timestamps; it is not what enables auditing. The GORM plugin audits eligible GORM statements globally. The MongoDB wrapper audits writes made through the wrapper, including maps and structs that do not embed `Model`.

## Audit event model

Both main integrations produce this conceptual event:

| Field | Meaning | Important caveat |
|---|---|---|
| `auditable_id` | String representation of the affected primary key or `_id`. | Only the first GORM primary key is used; composite keys are not represented correctly. |
| `auditable_type` | GORM model type name or MongoDB collection name. | Renaming a Go type or collection divides the apparent history. |
| `user_id` | Actor resolved from `context.Context`. | Optional; a missing actor does not stop a write. |
| `action` | `create`, `update`, or `delete`. | MongoDB soft delete and restore are recorded as `update`. |
| `audited_changes` | Map of field name to `[old, new]`. | Operator updates can contain requested intent rather than actual old/new state. |
| `version` | Monotonic sequence within one entity history. | Preferred stores allocate atomically; custom stores must provide the same guarantee. |
| `comment` | Optional caller-supplied reason. | Not a structured or authenticated approval record. |
| `created_at` | Time at which the library built the audit event. | It is not necessarily the database commit time. |

An event such as:

```json
{
  "auditable_type": "User",
  "auditable_id": "42",
  "user_id": "admin-7",
  "action": "update",
  "audited_changes": {
    "email": ["old@example.com", "new@example.com"]
  },
  "version": 3,
  "comment": "support request 1842",
  "created_at": "2026-07-08T10:00:00Z"
}
```

means the library observed a supported update path. It does not by itself prove that no other write path exists or that the audit record cannot be altered.

## How the GORM integration works

The preferred implementation is in `postgres-gorm/plugin.go` and `postgres-gorm/store.go`.

```text
request context
      |
      v
GORM create/update/delete
      |
      +-- update: read current row before the update
      |
      v
GORM performs the mutation
      |
      v
after-callback builds field changes
      |
      +-- AuditStore.NextVersion (atomic sequence increment)
      +-- AuditStore.Save
      +-- optional OnAudit callback
```

### Registration

`db.Use(auditablegorm.New(config))` installs create, update, and delete callbacks once on that GORM DB. The audit table and configured `SkipTables` are excluded to prevent recursion.

The plugin does not require models to embed `auditablegorm.Model`. Any mutation with a usable GORM schema and concrete primary key can be considered. Raw SQL, `Exec`, schema-less `Table` operations, and writes made outside this GORM instance are not covered.

### Actor and comment

The plugin reads the statement context. Actor resolution uses `Config.UserIDResolver` when supplied, otherwise `WithUserID`. Comments use `WithComment`.

Context helpers are package-specific. A value stored with the root package helper is not guaranteed to be visible to the `postgres-gorm` helper because their private context key types differ. Use helpers from the same package as the active plugin, or configure `UserIDResolver` against the application's existing auth context.

### Create

After a successful create with affected rows, the plugin walks schema fields and emits `[nil, newValue]`. It skips standard timestamps and applies inclusion/redaction tags. Slice creates are processed one element at a time.

### Update

Before an update, the preferred plugin attempts to load the current row using the model's primary key. After the update it handles two broad shapes:

- Map update: map keys are recorded with the snapshot value and the requested map value.
- Struct, single-column update, or `Save`: fields are selected with GORM's `Changed` information and, when needed, comparison with the snapshot.

This adds a read before the update. It only works when `db.Statement.Model` is a concrete struct with a non-zero primary key. A bulk statement such as `db.Model(&User{}).Where(...).Updates(...)` has no single model ID and returns `ErrBulkMutationUnsupported` unless `AllowUnauditedBulk` is explicitly enabled. Expression values can describe the requested expression rather than the final database value.

### Delete

After a delete, the plugin records available model fields as `[oldValue, nil]`. Standard timestamps are skipped. Conditional bulk deletion using an empty model is rejected by default.

### Persistence and transactions

The default `GormStore` receives the callback DB through context and attempts to use its transaction connection. This is the intended path for keeping a SQL mutation and its SQL audit row in the same GORM transaction.

There are still important semantic limits:

- Store errors call `OnError`; `FailOnAuditError` also attaches them to the GORM result so the default transaction can roll back.
- `OnAudit` runs after the audit insert but before the surrounding transaction is necessarily committed. It must not be treated as a committed event notification.
- A custom store, especially MongoDB, cannot participate atomically in the SQL transaction. The audit can exist for a later SQL rollback, or be absent for a committed SQL change.
- Database-specific behavior after a failed audit statement can differ, so failure-injection tests are required.

The root compatibility plugin is simpler: it writes directly through GORM, has no `AuditStore`, and records update old values as `nil`.

## How the direct MongoDB integration works

The MongoDB implementation wraps collection handles rather than installing a database-wide hook.

```text
caller uses AuditableCollection
      |
      +-- optional pre-read of ID or old document(s)
      |
      v
MongoDB collection mutation
      |
      v
derive requested changes or snapshot diff
      |
      +-- atomically increment version sequence document
      +-- insert audit document
      +-- optional OnAudit callback
```

### Coverage boundary

Only methods called through `AuditableCollection` are audited. The underlying collection is exposed by `Collection()`, so code can bypass auditing. Mongo shell changes, migrations, other services, triggers, and direct driver collection calls are also invisible.

The wrapper provides common methods but is not a complete replacement for every MongoDB write API. For example, bulk writes, find-and-modify methods, and pipeline-specific behavior are not fully represented.

### Inserts

`InsertOne` and `InsertMany` set zero-valued `CreatedAt` and `UpdatedAt` fields on pointer-to-struct documents, perform the insert, then create one audit event per document. Struct tags are read for inserted structs. Map documents rely on global `Config` field rules.

If `InsertMany` returns an error after a partial unordered insert, the wrapper returns immediately and creates no audits for the successful subset.

### Operator updates

`UpdateOne` pre-reads only `_id`, performs the update, and records the requested operator payload. The old side is `nil`; arithmetic and array operations are represented as operator intent, for example `[nil, {"$inc": 1}]`.

`UpdateMany` preloads matching IDs, performs one update, then writes one audit per preloaded ID. This is best effort:

- Read errors call `OnError`; `FailOnAuditError` stops before mutation, while fail-open mode continues.
- The matched set can change between the pre-read and mutation.
- Every preloaded ID gets the same requested change payload.
- Matched but unchanged documents can receive events.
- Pipeline updates are not reliably decoded into changes.
- Upserts with zero matches do not produce create events.

### Replacement and deletion

`ReplaceOne`, `DeleteOne`, and `DeleteMany` read documents before mutation. Replacements compare snapshots; deletes record `[oldValue, nil]`. Read errors follow the configured failure policy, but concurrent writes between read and mutation can still make the audit snapshot differ from the record actually replaced or deleted.

### Soft delete

`SoftDelete` and `SoftDeleteMany` set `deleted_at`; `Restore` unsets it. They are update operations in the audit log. Reads do not automatically exclude soft-deleted documents. Callers must use `NotDeleted` consistently.

`NotDeleted` preserves arbitrary filter values by combining them with the deleted-at condition through `$and`.

### MongoDB transactions

Without a caller-managed MongoDB transaction, the business mutation and audit insert are separate operations. A process crash or audit error can leave a committed mutation without an audit.

A session transaction may allow both operations to use the same transaction when all handles share the client and the session context is passed through, but the library does not begin, commit, or abort that transaction. Enable `FailOnAuditError` so the caller sees audit failures and can abort.

## Field filtering and redaction

### Intended tag language

| Tag | Intended behavior |
|---|---|
| no tag | Include unless whitelist mode is active. |
| `auditable:"true"` or `auditable:"only"` | Enable whitelist mode and include this field. |
| `auditable:"redact"` | Include the field but replace sensitive values. |
| `auditable:"-"` or `auditable:"false"` | Exclude the field. |

### Current behavior differences

- The preferred GORM plugin applies this language across create, update, and delete.
- The root compatibility plugin supports only `false` and `redact` and has different behavior.
- MongoDB `CollectionFor` retains the document field policy and applies it to create, update, replace, and delete paths.
- Redacted fields remain included safely when whitelist mode is active.
- MongoDB configuration protects exact top-level field names. It does not recursively redact sensitive nested paths.

Configure nested sensitive paths in a custom `ChangeComputer`/deferred handler because built-in policy is top-level.

Redaction is irreversible masking, not encryption. A redacted event can prove that a protected field was part of an operation, but it cannot show how the secret changed. The current replacement diff can also mark a redacted field as changed even when its value is unchanged.

## What works, and under what conditions

The table below is the useful contract for the current implementation.

| Scenario | Current status |
|---|---|
| GORM single create using the registered DB | Implemented; event contains non-skipped fields. Failure/no-op behavior needs tests. |
| GORM slice create | Implemented as one event per reflected element. Partial failure behavior is untested. |
| GORM update on a loaded model with primary key | Implemented in `postgres-gorm`, including a best-effort old snapshot. |
| GORM conditional/bulk update | Rejected by default; explicit opt-out allows the unaudited mutation. |
| GORM delete of a loaded model | Implemented. |
| GORM conditional/bulk delete | Rejected by default; mutate loaded models individually. |
| GORM raw SQL or writes through another DB handle | Not audited. |
| SQL audit store in the same GORM transaction | Intended and structurally supported; error propagation and rollback guarantees require integration tests. |
| GORM with MongoDB audit store | API is implemented; cross-database atomicity is impossible without a larger consistency pattern. |
| MongoDB full-success insert | Implemented. |
| MongoDB operator update | Records requested intent, usually without actual old value. |
| MongoDB replacement | Best-effort before/after diff with a race between read and write. |
| MongoDB delete | Best-effort old snapshot with a race between read and delete. |
| MongoDB many-update/delete | Emits per-prefetched-document events but is expensive and race-prone. |
| MongoDB upsert | Not represented correctly as a create. |
| MongoDB update pipeline | Not reliably audited. |
| MongoDB soft delete | Opt-in helper; recorded as update; read filtering remains caller responsibility. |
| Concurrent per-entity versioning | Atomic in the preferred SQL and MongoDB stores; custom stores own this contract. |
| Immutable/tamper-evident storage | Not implemented. |
| Reconstructing exact entity state from audit history | Not guaranteed. This is not event sourcing. |

The repository contains unit coverage for policy, complexity, custom computation, filter preservation, and context handling. `go test ./...` and `go vet ./...` pass; database-backed concurrency and rollback tests remain necessary.

## Performance characteristics

Auditing is synchronous and adds database calls to the application write path.

Approximate extra work per event:

| Operation | Additional work |
|---|---|
| GORM create/delete with SQL store | Atomic sequence upsert plus audit insert. |
| GORM update with SQL store | Old-row read, atomic sequence upsert, and audit insert. |
| MongoDB `UpdateOne` | ID read, atomic sequence update, and audit insert. |
| MongoDB replace/delete one | Full-document read, atomic sequence update, and audit insert. |
| MongoDB many operations | Pre-read plus roughly one atomic sequence update and one audit insert per document. |
| MongoDB `InsertMany` | One business insert plus two audit calls per inserted document. |

`OnAudit` and `OnError` execute inline. Preferred packages contain callback panics, but slow logging, network calls, or message publishing still increases request latency.

Atomic per-entity sequences serialize concurrent events for the same entity. Benchmarks should cover hot entities, high-cardinality batch operations, and sequence/audit transaction ordering.

## Failure and consistency model

The team should explicitly choose one of these product policies:

| Policy | Meaning | Appropriate use |
|---|---|---|
| Fail closed | A business mutation must fail or roll back if its audit cannot be durably stored. | Regulated or security-sensitive changes. |
| Fail open | The business mutation may succeed; audit failure is separately alerted and repaired. | Availability-first operational logging. |
| Transactional outbox | The business transaction writes an audit/outbox row atomically; asynchronous workers deliver secondary copies. | Strong SQL consistency plus scalable downstream delivery. |

The default remains fail-open for compatibility. `FailOnAuditError` selects caller-visible errors; with the default GORM SQL transaction this is intended to roll back the business mutation. MongoDB requires a caller-managed transaction for rollback.

For GORM-to-Mongo storage, a transactional outbox in the SQL database is the safer pattern. Writing directly to MongoDB from a SQL callback creates a dual-write problem that retries alone cannot solve.

## Pros

- Small integration surface for common CRUD paths.
- A consistent conceptual event shape across SQL and MongoDB.
- Actor and reason metadata flow naturally through request context.
- Field-level payloads are easier to inspect than whole-record snapshots.
- Redaction and exclusion are built into event construction.
- Pluggable GORM storage separates capture from persistence.
- Separate MongoDB driver packages reduce forced migrations for consumers.
- Index helpers and migration models provide an initial operational setup.

## Cons and risks

- Application-level interception cannot see bypass writes.
- Global GORM callbacks can audit more models than intended, while MongoDB wrappers can be bypassed accidentally.
- Old values are incomplete or race-prone for several update forms.
- Audit errors are fail-open by default and require an explicit production policy.
- Synchronous extra queries increase latency and database load.
- Batch behavior differs substantially between GORM and MongoDB.
- Hot-entity version rows/documents can become contention points.
- Cross-store persistence is a non-atomic dual write.
- Redaction behavior differs by package and operation.
- Stable logical entity names, tenant identity, request correlation, and service identity are absent.
- No built-in retention, archival, integrity verification, replay, revert, or audit query service exists.
- The audit store is not protected from update or delete by database permissions or append-only controls.

## Recommended patterns

### Integration patterns

- Register exactly one GORM audit plugin during database construction and pass that DB through repositories.
- For MongoDB, inject `AuditableCollection` into repositories and keep the raw collection private.
- Use one actor resolver tied to authenticated middleware; include service-account identities for background jobs.
- Require a reason for high-risk administrative actions at the service boundary.
- Define a field-classification review for every model change and test redaction explicitly.
- Prefer loaded-model GORM mutations when a per-row audit is required.
- Use a transaction/outbox when an audit must be inseparable from the business write.
- Restrict database credentials so application code can insert audit records but cannot update or delete them.
- Monitor audit failure counts, event lag, missing actors, duplicate versions, and audit volume.
- Query history by stable `(entity_type, entity_id)` and document rename/migration procedures.

### Data patterns

- Add `event_id`, `tenant_id`, `request_id` or `trace_id`, `source_service`, and optional `impersonator_id` where relevant.
- Store a stable logical entity type instead of deriving it only from the Go type name.
- Make the meaning of `[old, new]` explicit per operation. If a value is unknown, distinguish `unknown` from JSON `null`.
- Use UTC consistently and distinguish event creation time from commit/ingest time.
- Keep the unique `(auditable_type, auditable_id, version)` constraint/index as defense in depth around atomic allocation.
- Define retention and legal-hold behavior before audit volume becomes large.

## Anti-patterns

- Calling `Collection()` and then mutating the returned raw MongoDB collection.
- Mixing the v1 MongoDB wrapper with v2 driver handles.
- Assuming `OnAudit` means the business transaction committed.
- Publishing directly to a remote message broker inside `OnAudit` without an outbox.
- Ignoring `OnError` or merely logging it without alerting and a repair process.
- Treating `nil` old values as proof that the old database value was null.
- Using bulk GORM writes when the requirement is one reliable audit event per row.
- Assuming top-level MongoDB policy recursively protects nested secrets.
- Recording credentials, tokens, personal data, or large blobs without explicit classification.
- Using audit logs as a cache, event-sourcing stream, or rollback mechanism.
- Allowing the same application role to rewrite or delete audit history.
- Claiming “every change is audited” without enumerating and testing every write path.

## Questions likely to arise from the team and reviewers

### Product and behavior

**Does this record every database change?**

No. It records supported writes made through the configured GORM instance or wrapped MongoDB collection. Raw SQL, raw MongoDB handles, external tools, other services, and unsupported APIs are outside its visibility.

**Can we rebuild an entity exactly from the audit log?**

No. Skipped/redacted fields, unknown old values, operator intent, no-op events, and missing bypass writes prevent reliable replay. The library provides audit history, not event sourcing.

**Can users undo a change from an event?**

There is no revert API, and incomplete old values make automatic reversal unsafe.

**Are soft deletes represented consistently?**

No. GORM delete callbacks use action `delete`; MongoDB soft delete is an update to `deleted_at` and uses action `update`.

**Do models have to embed the supplied base model?**

No for auditing itself. Embedding provides conventional IDs and timestamps. Audit capture is enabled by plugin registration or collection wrapping.

### Reliability and concurrency

**What happens when audit storage is down?**

`OnError` is invoked. With the default fail-open policy the mutation result is unchanged. `FailOnAuditError` returns/attaches the failure; rollback still requires the data and audit operations to share a transaction.

**Are entity versions strictly increasing and unique?**

Preferred SQL and MongoDB stores use atomic per-entity sequence records. A custom `AuditStore.NextVersion` implementation must provide the same atomic guarantee.

**Are the data change and audit atomic?**

Only potentially when both use the same database transaction, and even that needs tests. SQL-to-Mongo storage is not atomic. Direct MongoDB calls are not atomic with their audit inserts unless the caller manages a suitable transaction.

**What happens on retries?**

There is no idempotency key. A retried mutation can generate another event, and an audit retry strategy is not built in.

### Security and compliance

**Does redaction cover nested secrets?**

No. MongoDB's built-in policy is top-level, though it is now consistent across mutation types. GORM field tags operate at schema-field level, not arbitrary nested JSON paths.

**Is the history immutable or tamper evident?**

No. That must be enforced with database privileges, append-only infrastructure, change-stream monitoring, signatures/hash chains, or external immutable storage.

**Is this sufficient for SOC 2, ISO 27001, HIPAA, or another framework?**

Not by itself. It can support a control, but reviewers will also require coverage evidence, access control, retention, alerting, integrity, incident procedures, and tests.

**What if no actor is present?**

The event is still stored with a null user. The application must decide which operations may be anonymous and enforce that policy before the database call.

**Could audit logs create a second sensitive-data store?**

Yes. Audit payloads can multiply retention and breach exposure. Field classification, redaction tests, encryption, access control, and retention are mandatory.

### Operations and scale

**What is the latency cost?**

Common single-row writes add two or three synchronous database operations. MongoDB many operations can add two operations for every affected document.

**How should we monitor it?**

At minimum: audit save/version errors, callback and diff duration, deferred-diff queue age, audit events per mutation type, events without actors, duplicate versions, write latency, audit-store growth, and retention outcomes.

**How are schema or type renames handled?**

They are not. Because type names and collection names become identity fields, the migration must either retain a stable configured name or rewrite/alias history.

**Why are there `mongodb` and `mongodbv2` packages?**

They support MongoDB Go driver major versions 1 and 2. Applications should import exactly the one matching their driver.

### Code review

**Why is the package named `postgres-gorm` if GORM supports other SQL databases?**

The implementation is mostly GORM-generic. The name creates an avoidable product boundary and should be clarified or replaced in a future major version.

**Are callback registration errors visible?**

Yes. Plugin initialization returns registration errors so duplicate or invalid callback setup fails visibly.

**How are concurrent versions allocated?**

Preferred stores atomically increment a per-entity SQL row or MongoDB document. This avoids duplicate versions without process mutexes, at the cost of contention for hot entities.

**Why does `TableName` not affect `Migrate`?**

`Migrate` creates the default `audits` and `audit_versions` tables. Custom table users call `MigrateWithTable`.

**What proves the examples and README are accurate?**

Unit tests cover core policy and complexity behavior, and all examples compile in `go test ./...`. Database-backed integration tests should be the next source-of-truth layer.

## Improvement plan

### P0: correctness and release readiness

1. Add PostgreSQL and MongoDB integration tests for transaction rollback, concurrent versions, and audit-store failure.
2. Handle MongoDB pre-read errors and eliminate read/write races where correctness is promised.
3. Detect and correctly audit upserts and partial `InsertMany` success, or explicitly reject those options.
4. Add a unique audit-event ID and idempotent retry contract.
5. Prove `FailOnAuditError` semantics for default and explicit transactions.
6. Add integration benchmarks for hot entities, many-document writes, and deferred payloads.

### P1: production hardening

1. Extend operation/result handling for upserts, partial many-writes, and no-op distinctions.
2. Use `FindOneAndUpdate`, `FindOneAndReplace`, or `FindOneAndDelete` patterns where they can return the actual affected document atomically.
3. Add caller-controlled transaction helpers and integration tests for SQL and MongoDB rollback behavior.
4. Use a SQL transactional outbox for GORM-to-Mongo or message-bus delivery.
5. Separate “audit persisted” from “transaction committed”; do not expose pre-commit `OnAudit` as a delivery guarantee.
6. Add stable entity naming and support composite identifiers.
7. Add structured metadata: tenant, request/trace, source service, job, impersonator, and reason code.
8. Make migration honor custom table names and add the correct indexes/constraints.
9. Validate configuration at startup, including audit collection/table accidentally matching a business collection/table.
10. Add metrics, structured error types, and latency benchmarks.

### P2: compliance and operability

1. Add append-only database permissions and documented role separation.
2. Add optional tamper evidence, such as chained hashes or signed batches, backed by protected key management.
3. Add retention, archival, legal hold, and deletion-policy tooling.
4. Add a query API for entity timeline, actor activity, and request correlation.
5. Add nested-path filtering/redaction with a single policy language across backends.
6. Add idempotency/event IDs and a repair workflow for fail-open gaps.
7. Add load tests and storage sizing guidance.

## Minimum test matrix

The repository should not make stronger guarantees than this matrix proves.

| Area | Required cases |
|---|---|
| GORM create | single, slice, generated ID, DB default, validation failure, unique violation, transaction rollback |
| GORM update | map, struct, `Update`, `Save`, expression, zero rows, stale model, soft-delete restore, bulk condition |
| GORM delete | loaded model, soft delete, hard delete, zero rows, batch condition, rollback |
| GORM schema | custom table, skipped table, composite key, string/UUID/integer key, custom audit table |
| MongoDB insert | struct, map, generated ID, ordered/unordered partial failure, transaction rollback |
| MongoDB update | every supported operator, pipeline, upsert, no-op, zero match, broad filter, concurrent change |
| MongoDB replace/delete | missing pre-read, concurrent change, upsert, many operation, transaction rollback |
| Field policy | untagged, only, redact, skip, nested value, config-plus-tag, every mutation type |
| Context | string/numeric user, custom resolver, missing actor, comment, cancellation/deadline |
| Versioning | two simultaneous writers, retry after duplicate, store read failure |
| Failure policy | primary DB failure, audit DB failure, callback panic, process interruption between writes |
| Compatibility | MongoDB driver v1 package and v2 package compile independently |

Use real PostgreSQL and MongoDB containers for transaction, driver, serialization, and concurrency tests. Pure unit tests are insufficient for the central guarantees.

## Production-readiness checklist

Before a service adopts this library, verify:

- [ ] The repository builds with no merge markers and the relevant package has passing tests.
- [ ] Every application write path is inventoried and classified as covered, intentionally excluded, or blocked.
- [ ] Audit failure policy is explicit and tested.
- [ ] Version uniqueness is concurrency-safe.
- [ ] Actor identity is required where policy demands it.
- [ ] Sensitive fields are tested for every operation, including delete and replacement.
- [ ] Bulk operations and upserts have defined behavior.
- [ ] SQL/Mongo transaction behavior is integration-tested.
- [ ] Audit credentials cannot modify or delete existing history.
- [ ] Retention, backups, restore, and legal hold are documented.
- [ ] Metrics and alerts detect audit gaps quickly.
- [ ] Load tests demonstrate acceptable latency and storage growth.
- [ ] Reviewers agree that this is an audit trail, not an event store or sole compliance control.

## Bottom line

The project has a useful foundation and a clear integration model. Its strongest current use case is best-effort operational auditing of ordinary single-record CRUD operations through controlled repository paths.

The immediate correctness blockers—repository conflicts, atomic preferred-store versions, field-policy consistency, unsafe silent GORM bulk writes, and caller-selectable audit errors—are addressed. The next milestone is database-backed proof: transaction rollback, concurrent writers, upserts/partial failures, pre-read races, hot-entity benchmarks, and deferred object-storage recovery.
