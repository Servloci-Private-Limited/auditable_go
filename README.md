# auditable_go

`auditable_go` adds Rails-style audit logging to Go applications. It records who changed a record, what action happened, which fields changed, which record was affected, when it happened, and an optional human-readable reason.

The package supports both main integration paths in this repository:

- GORM auditing for SQL-backed models, with audit records stored in SQL by default.
- MongoDB collection auditing for applications that use the MongoDB Go driver directly.
- GORM auditing with MongoDB as the audit-record store, useful when application data lives in SQL but audit logs should live in MongoDB.

## Features

- Audits create, update, and delete operations.
- Stores field-level change payloads as `field: [oldValue, newValue]`.
- Reads `user_id` and optional `comment` from `context.Context`.
- Supports field redaction for secrets and PII.
- Supports field/table/collection skipping to reduce audit noise.
- Supports synchronous `OnAudit` callbacks for logging, streaming, or forwarding audit events.
- Supports `OnError` callbacks for audit-only failures without changing existing CRUD method signatures.
- Provides MongoDB audit indexes through `EnsureIndexes` helpers.
- Keeps integration small: register a GORM plugin or wrap MongoDB collections once, then use normal create/update/delete calls.

## Packages

| Package | Use case |
|---|---|
| `github.com/vikasavnish/auditable_go/v3/postgres-gorm` | Recommended GORM plugin package. Supports SQL audit storage and custom stores such as MongoDB. |
| `github.com/vikasavnish/auditable_go/v3/mongodb` | MongoDB collection wrapper and MongoDB-backed GORM audit store. |
| `github.com/vikasavnish/auditable_go/v3` | Root GORM plugin kept for compatibility with the earlier API. |

For new code, prefer `postgres-gorm` for GORM integrations because it supports pluggable audit storage.

## Installation

```bash
go get github.com/vikasavnish/auditable_go/v3
```

The module currently uses:

- `gorm.io/gorm`
- `gorm.io/datatypes`
- `go.mongodb.org/mongo-driver/v2`

## Audit Record Shape

All integrations write the same conceptual audit event.

| Field | Description |
|---|---|
| `auditable_id` | Primary key or MongoDB `_id` of the changed record. |
| `auditable_type` | GORM model name or MongoDB collection name. |
| `user_id` | Acting user from context, nullable. |
| `action` | `create`, `update`, or `delete`. |
| `audited_changes` | Field-level changes. Usually `field: [oldValue, newValue]`. |
| `version` | Monotonically increasing version per entity. |
| `comment` | Optional reason from context, nullable. |
| `created_at` | Audit timestamp. |

Example payload:

```json
{
  "auditable_id": "EPIC001",
  "auditable_type": "voters",
  "user_id": "user-101",
  "action": "update",
  "audited_changes": {
    "tags": [null, [{ "key": "supporter", "color": "#4CAF50" }]]
  },
  "version": 2,
  "comment": "field team tagged voter",
  "created_at": "2026-05-08T10:15:00Z"
}
```

For GORM map updates and MongoDB operator updates, old values are not loaded before the update, so the old side is recorded as `nil`. For deletes and MongoDB replacements, the wrapper captures available old values.

## Context Helpers

Attach audit metadata to the context used by your database operation.

```go
ctx := context.Background()
ctx = auditable.WithUserID(ctx, "user-101")
ctx = auditable.WithComment(ctx, "approved by programme manager")
```

MongoDB uses the same helper names from the `mongoaudit` package:

```go
ctx := context.Background()
ctx = mongoaudit.WithUserID(ctx, "user-101")
ctx = mongoaudit.WithComment(ctx, "duplicate record removed")
```

`WithUserID` accepts any value. Strings, `fmt.Stringer`, byte slices, integers, and other values are converted to a non-empty string.

## GORM Integration With SQL Audit Storage

Use this when your application writes data through GORM and you want audit records in the SQL `audits` table.

```go
package main

import (
	"context"
	"log"

	auditable "github.com/vikasavnish/auditable_go/v3/postgres-gorm"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type User struct {
	ID       uint
	Name     string
	Email    string
	Password string `auditable:"redact"`
	Internal string `auditable:"false"`
}

func setup() (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open("postgres://postgres:postgres@localhost:5434/postgres?sslmode=disable"), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	plugin := auditable.New(auditable.Config{
		OnAudit: func(a *auditable.Audit) {
			log.Printf("audit: %s %s#%s v%d", a.Action, a.AuditableType, a.AuditableID, a.Version)
		},
		OnError: func(err error) {
			log.Printf("audit error: %v", err)
		},
	})

	if err := db.Use(plugin); err != nil {
		return nil, err
	}

	if err := db.AutoMigrate(&auditable.Audit{}, &User{}); err != nil {
		return nil, err
	}

	return db, nil
}

func run(db *gorm.DB) error {
	ctx := auditable.WithUserID(context.Background(), "user-101")
	ctx = auditable.WithComment(ctx, "profile update from admin panel")

	user := User{Name: "Alice", Email: "alice@example.com", Password: "secret"}
	if err := db.WithContext(ctx).Create(&user).Error; err != nil {
		return err
	}

	if err := db.WithContext(ctx).Model(&user).Update("name", "Bob").Error; err != nil {
		return err
	}

	return db.WithContext(ctx).Delete(&user).Error
}
```

### GORM Config

| Field | Default | Description |
|---|---|---|
| `RedactedValue` | `[REDACTED]` | Replacement value for fields tagged `auditable:"redact"`. |
| `SkipTables` | empty | Additional SQL table names that must never be audited. The audit table itself is skipped automatically. |
| `TableName` | `audits` | SQL audit table name. |
| `OnAudit` | `nil` | Called after an audit record is persisted. |
| `OnError` | `nil` | Called when audit persistence or version lookup fails. |
| `Store` | SQL store | Custom audit store. Available in `postgres-gorm`. |

### GORM Field Tags

| Tag | Behavior |
|---|---|
| No tag | Field is audited normally. |
| `auditable:"false"` | Field is excluded from audit records. |
| `auditable:"redact"` | Field is included, but values are replaced with `RedactedValue`. |

Timestamps named `CreatedAt`, `UpdatedAt`, and `DeletedAt` are skipped by the plugin to avoid noisy audit entries.

### GORM Supported Operations

| GORM call | Audit behavior |
|---|---|
| `db.Create(&model)` | One create audit with persisted fields as `[nil, new]`. |
| `db.Create(&slice)` | One create audit per element. |
| `db.Model(&m).Updates(map[string]interface{}{...})` | One update audit with map keys as `[nil, new]`. |
| `db.Model(&m).Updates(struct)` | Update audit for fields GORM reports as changed. |
| `db.Model(&m).Update("Field", value)` | Update audit for the changed field. |
| `db.Delete(&model)` | Delete audit with fields as `[old, nil]`. |
| `db.Unscoped().Delete(&model)` | Same audit behavior for hard deletes. |

## MongoDB Integration

Use this when your application writes directly with the MongoDB Go driver.

Create one `Auditor` per database and use it to wrap as many collections as needed. The wrapped collection exposes common read methods and mutating methods with the same driver-style signatures.

```go
package main

import (
	"context"
	"log"
	"time"

	mongoaudit "github.com/vikasavnish/auditable_go/v3/mongodb"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func setupMongo(ctx context.Context, uri string) (*mongoaudit.AuditableCollection, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}

	db := client.Database("auditdemo")
	auditor := mongoaudit.NewAuditor(db, "audits", mongoaudit.Config{
		RedactedFields: map[string]struct{}{
			"password":    {},
			"token":       {},
			"epic_number": {},
		},
		OnAudit: func(a *mongoaudit.Audit) {
			log.Printf("audit: %s %s/%s v%d", a.Action, a.AuditableType, a.AuditableID, a.Version)
		},
		OnError: func(err error) {
			log.Printf("audit error: %v", err)
		},
	})

	if err := auditor.EnsureIndexes(ctx); err != nil {
		return nil, err
	}

	return auditor.Collection("voters"), nil
}

func runMongo(ctx context.Context, voters *mongoaudit.AuditableCollection) error {
	ctx = mongoaudit.WithUserID(ctx, "user-101")

	_, err := voters.InsertOne(ctx, bson.M{
		"_id":        "EPIC001",
		"name":       "Ravi Kumar",
		"created_at": time.Now(),
		"updated_at": time.Now(),
	})
	if err != nil {
		return err
	}

	updateCtx := mongoaudit.WithComment(ctx, "field team tagged voter")
	_, err = voters.UpdateOne(updateCtx,
		bson.M{"_id": "EPIC001"},
		bson.M{"$set": bson.M{"tags": bson.A{bson.M{"key": "supporter"}}}},
	)
	if err != nil {
		return err
	}

	deleteCtx := mongoaudit.WithComment(ctx, "duplicate record removed")
	_, err = voters.DeleteOne(deleteCtx, bson.M{"_id": "EPIC001"})
	return err
}
```

### MongoDB Config

| Field | Default | Description |
|---|---|---|
| `RedactedValue` | `[REDACTED]` | Replacement value for redacted fields. |
| `RedactedFields` | empty | Top-level field names whose values should never appear in the audit trail. |
| `AuditCollectionName` | `audits` | Used by `NewAuditor` when the audit collection name argument is empty. |
| `SkipFields` | `created_at`, `updated_at` | Top-level fields excluded from every audit entry. Caller values are merged with the defaults. |
| `OnAudit` | `nil` | Called after an audit document is inserted. |
| `OnError` | `nil` | Called for audit-only failures such as audit insert or version lookup errors. |

### MongoDB Supported Operations

| Method | Audit behavior |
|---|---|
| `InsertOne` | Create audit for the inserted document. |
| `InsertMany` | One create audit per inserted document. |
| `UpdateOne` | One update audit for the matched document. |
| `UpdateMany` | One update audit per matched document ID found before the update. |
| `ReplaceOne` | Update audit with a before/after diff. |
| `DeleteOne` | Delete audit using the document captured before deletion. |
| `DeleteMany` | One delete audit per document captured before deletion. |

Read-only pass-through methods include `FindOne`, `Find`, `CountDocuments`, and `Aggregate`. You can access the original collection with `Collection()`.

### MongoDB Update Payloads

For operator updates, the wrapper records the requested change without loading the full old document:

```go
bson.M{
	"status": bson.A{nil, "active"},
	"score":  bson.A{nil, bson.M{"$inc": 1}},
}
```

For `ReplaceOne`, the wrapper compares the old and new document and records true before/after values.

## GORM With MongoDB Audit Storage

Use this when business data is stored through GORM, but audit logs should be stored in MongoDB.

```go
package main

import (
	"context"
	"log"

	mongoaudit "github.com/vikasavnish/auditable_go/v3/mongodb"
	auditable "github.com/vikasavnish/auditable_go/v3/postgres-gorm"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"gorm.io/gorm"
)

func useMongoAuditStore(ctx context.Context, db *gorm.DB, mongoClient *mongo.Client) error {
	auditCollection := mongoClient.Database("auditdemo").Collection("audits")
	store := mongoaudit.NewMongoStore(auditCollection)

	if err := store.EnsureIndexes(ctx); err != nil {
		return err
	}

	return db.Use(auditable.New(auditable.Config{
		Store: store,
		OnError: func(err error) {
			log.Printf("audit error: %v", err)
		},
	}))
}
```

This integration does not require model changes. Your application keeps using GORM normally, while the plugin sends audit records to the configured MongoDB collection.

## Indexes

For SQL audit storage, migrate `auditable.Audit` with GORM or create an equivalent table manually. The key lookup index is:

```sql
CREATE INDEX idx_auditable_lookup
  ON audits (auditable_type, auditable_id);
```

For MongoDB audit storage, call one of the helpers:

```go
auditor := mongoaudit.NewAuditor(db, "audits", mongoaudit.Config{})
if err := auditor.EnsureIndexes(ctx); err != nil {
	return err
}
```

or:

```go
store := mongoaudit.NewMongoStore(db.Collection("audits"))
if err := store.EnsureIndexes(ctx); err != nil {
	return err
}
```

The MongoDB helper creates indexes for entity/version lookup, `created_at`, and `user_id`.

## Running The Examples

### GORM Example

The GORM example is in [example/main.go](example/main.go).

It expects `PRIMARY_DSN_2` in `.env` or in your environment:

```bash
PRIMARY_DSN_2='postgres://postgres:postgres@localhost:5434/postgres?sslmode=disable' go run ./example
```

### MongoDB Example

The MongoDB example is in [example/mongodb/main.go](example/mongodb/main.go).

Start MongoDB from the example directory:

```bash
cd example/mongodb
docker compose up -d
go run .
```

From the repository root, you can also run:

```bash
go run ./example/mongodb
```

Use `MONGO_URI` to override the default URI:

```bash
MONGO_URI='mongodb://localhost:27025' go run ./example/mongodb
```

The example currently leaves the destructive collection-drop block commented out so repeated runs can preserve existing demo data. Uncomment that block only when you want a clean demo database.

## Initial Migration

For SQL-backed audit rows, the simplest first setup is:

```go
plugin := auditable.New(auditable.Config{})
if err := db.Use(plugin); err != nil {
	return err
}

if err := db.AutoMigrate(&auditable.Audit{}, &User{}); err != nil {
	return err
}
```

If you manage schema outside GORM, create an `audits` table that matches the `auditable.Audit` model. A reference migration is available in [docs/initial-migration.md](docs/initial-migration.md).

## Practical Notes

- Register the GORM plugin once during database initialization.
- Wrap MongoDB collections once and pass the wrapped collection through your repository/service layer.
- Use `OnError` in production so audit-only failures are visible in logs or metrics.
- Keep `RedactedFields` and `auditable:"redact"` updated as your data model evolves.
- Avoid auditing high-churn infrastructure fields unless they are meaningful to your compliance story.
- The current MongoDB wrapper audits top-level fields. Nested values are preserved as values, but nested field-specific redaction is not expanded recursively.
- Audit callbacks run synchronously. Keep `OnAudit` and `OnError` lightweight, or forward work to a queue.

## Verification

Useful commands while developing this package:

```bash
go test . ./mongodb ./postgres-gorm ./example/mongodb ./example
go test ./...
```

At the time this README was updated, the focused audit package verification passes. The full repository suite may still fail if unrelated model validation tests are out of sync with their enum definitions.
