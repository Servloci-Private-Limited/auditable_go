# Initial migration

`auditable_go` can persist audit rows in the SQL `audits` table defined by `auditable.Audit`, or in a MongoDB `audits` collection when using the MongoDB helpers.

## Recommended first setup

If you let GORM own schema creation, migrate the audit table alongside your audited models:

```go
plugin := auditable.New(auditable.Config{})
if err := db.Use(plugin); err != nil {
	return err
}

if err := db.AutoMigrate(&auditable.Audit{}, &User{}); err != nil {
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

CREATE INDEX idx_audits_user_id
  ON audits (user_id);

CREATE INDEX idx_audits_action
  ON audits (action);
```

For MySQL or SQLite, keep the same columns and indexes, and use the database's JSON-capable column type.

## MongoDB audit collection

For direct MongoDB auditing or GORM auditing backed by MongoDB, create the recommended indexes with the package helper:

```go
auditor := mongoaudit.NewAuditor(db, "audits", mongoaudit.Config{})
if err := auditor.EnsureIndexes(ctx); err != nil {
	return err
}
```

When using `mongoaudit.NewMongoStore` with the GORM plugin, call `store.EnsureIndexes(ctx)` on the same audit collection.
