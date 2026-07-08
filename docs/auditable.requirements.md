# Auditable — Requirements

## Goal

Track every data-mutating operation on GORM models so the following questions can always be answered:

| Question | Stored as |
|---|---|
| Who made the change? | `user_id` (string, from request context) |
| What kind of change? | `action` — `create`, `update`, or `delete` |
| What exactly changed? | `audited_changes` — JSON map of `field → [old, new]` |
| On which record? | `auditable_type` (model name) + `auditable_id` (primary key) |
| When? | `created_at` timestamp on the audit row |
| Why? (optional) | `comment` — free-text reason attached to the context |

---

## Audit Record Schema

```
audits
├── id               uint64        primary key
├── auditable_type   varchar(191)  model name, e.g. "Campaign"
├── auditable_id     varchar(191)  primary key of the changed record
├── user_id          varchar(191)  nullable — caller supplied via context
├── action           varchar(16)   "create" | "update" | "delete"
├── audited_changes  jsonb         { "field": [old, new], … }
├── version          uint64        monotonically increasing per entity
├── comment          varchar(512)  nullable — reason for the change
└── created_at       timestamp
```

---

## Implementation

The plugin is registered once at DB initialisation and requires **no changes to existing models**:

```go
db.Use(auditable.New(auditable.Config{}))
```

### Mechanism

1. **GORM after-callbacks** — hooks fire after every `Create`, `Update`, and `Delete` statement so no business logic needs to be modified.
2. **Struct tags** — per-field behaviour is controlled with the `auditable` tag:
   - _(absent)_ — field is included in the diff normally.
   - `auditable:"false"` — field is excluded entirely from the audit trail.
   - `auditable:"redact"` — field is included but its value is replaced with the configured redaction string (default `[REDACTED]`). Useful for passwords, tokens, and PII.

### Context Keys

| Helper | Purpose |
|---|---|
| `auditable.WithUserID(ctx, id)` | Attach the acting user's ID to the context |
| `auditable.WithComment(ctx, reason)` | Attach a human-readable reason for the change |

### Config Options

| Field | Default | Description |
|---|---|---|
| `TableName` | `"audits"` | Override the audit table name |
| `RedactedValue` | `"[REDACTED]"` | Replacement string for redacted fields |
| `SkipTables` | `[]` | Additional tables that must never be audited |
| `OnAudit` | `nil` | Optional callback fired after each audit row is persisted |
| `OnError` | `nil` | Reports audit persistence/version errors |
| `FailOnAuditError` | `false` | Attaches audit failures to the GORM result |
| `AllowUnauditedBulk` | `false` | Explicit opt-out for bulk writes without per-row audit identity |

---

## Supported Operations

| GORM call | Audit behaviour |
|---|---|
| `db.Create(&model)` | Records all non-excluded fields as `[nil, new]` |
| `db.Create(&slice)` | One audit row per element in the slice |
| `db.Model(&m).Updates(map)` | Preferred plugin preloads the current row and records `[old, new]` |
| `db.Model(&m).Updates(struct)` | Records fields reported as changed by GORM |
| `db.Model(&m).Update("Field", val)` | Records the single changed field |
| `db.Delete(&model)` | Records all fields as `[old, nil]`; works for both soft and hard deletes |
| `db.Unscoped().Delete(&model)` | Same as above — hard delete is still audited |

Conditional bulk update/delete statements without a concrete primary key are rejected by default. This prevents a successful write from silently lacking per-row audit events.

Per-entity versions are allocated with atomic sequence rows/documents and protected by a unique `(auditable_type, auditable_id, version)` index in preferred stores.
