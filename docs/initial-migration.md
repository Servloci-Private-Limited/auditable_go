# Initial migration

`auditable_go` can persist audit rows in the SQL `audits` table defined by `auditable.Audit`, or in a MongoDB `audits` collection when using the MongoDB helpers.

## Recommended first setup

If you let GORM own schema creation, migrate the audit table alongside your audited models:

```go
plugin := auditable.New(auditable.Config{})
if err := db.Use(plugin); err != nil {
	return err
}

if err := auditable.Migrate(db); err != nil {
	return err
}
if err := db.AutoMigrate(&User{}); err != nil {
	return err
}
```

## Table shape

The audit model is:

```go
type Audit struct {
	ID             uint64            `gorm:"primaryKey"`
	AuditableID    string            `gorm:"size:191;index:idx_auditable_lookup,priority:2;not null"`
	AuditableType  string            `gorm:"size:191;index:idx_auditable_lookup,priority:1;not null"`
	UserID         *string           `gorm:"size:191;index"`
	Action         Action            `gorm:"size:16;index;not null"`
	AuditedChanges datatypes.JSONMap `gorm:"type:json;not null"`
	Version        uint64            `gorm:"not null"`
  Comment        *string           `gorm:"size:512"`
	CreatedAt      time.Time         `gorm:"not null"`
}
```

The preferred GORM store also uses `audit_versions`:

```go
type AuditVersion struct {
    Key     string `gorm:"primaryKey;size:64"`
    Version uint64 `gorm:"not null"`
}
```

The key is a SHA-256 digest of entity type and ID. Each event atomically increments this row in the same SQL transaction. Versions are monotonic but may contain gaps after fail-open or cross-store failures.

## Example SQL

If you manage schema outside GORM, create an equivalent table for your database. A PostgreSQL version looks like this:

```sql
CREATE TABLE audits (
  id BIGSERIAL PRIMARY KEY,
  auditable_id VARCHAR(191) NOT NULL,
  auditable_type VARCHAR(191) NOT NULL,
  user_id VARCHAR(191),
  action VARCHAR(16) NOT NULL,
  audited_changes JSONB NOT NULL,
  version BIGINT NOT NULL,
  comment VARCHAR(512),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_auditable_lookup
  ON audits (auditable_type, auditable_id);

CREATE UNIQUE INDEX uniq_auditable_version
  ON audits (auditable_type, auditable_id, version);

CREATE INDEX idx_audits_user_id
  ON audits (user_id);

CREATE INDEX idx_audits_action
  ON audits (action);

CREATE TABLE audit_versions (
  key VARCHAR(64) PRIMARY KEY,
  version BIGINT NOT NULL
);
```

For a custom audit table, use `auditable.MigrateWithTable(db, "custom_audits")`; its sequence table is named `custom_audits_versions`.

The atomic sequence implementation uses `ON CONFLICT ... RETURNING` and is verified for PostgreSQL. Validate dialect behavior before using another GORM database.

## MongoDB audit collection

For direct MongoDB auditing or GORM auditing backed by MongoDB, create the recommended indexes with the package helper:

```go
auditor := mongoaudit.NewAuditor(db, "audits", mongoaudit.Config{})
if err := auditor.EnsureIndexes(ctx); err != nil {
	return err
}
```

When using `mongoaudit.NewMongoStore` with the GORM plugin, call `store.EnsureIndexes(ctx)` on the same audit collection.

MongoDB allocates versions with atomic `$inc` operations in `<audit collection>_versions` by default. MongoDB automatically provides the required unique `_id` index for that collection.
