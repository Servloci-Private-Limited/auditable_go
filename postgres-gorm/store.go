package auditablegorm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// gormTxKey is the context key used to propagate the callback *gorm.DB
// (which carries an active transaction) to GormStore so that audit writes
// participate in the same transaction as the data change.
type gormTxKey struct{}

// withGormTx injects db into ctx so the GormStore can join the transaction.
func withGormTx(ctx context.Context, db *gorm.DB) context.Context {
	return context.WithValue(ctx, gormTxKey{}, db)
}

// AuditRecord is a storage-neutral snapshot of a single audit event produced
// by the GORM plugin. It contains no GORM tags so any store backend can consume
// it without a GORM dependency.
type AuditRecord struct {
	AuditableID    string
	AuditableType  string
	UserID         *string
	Action         Action
	AuditedChanges map[string]interface{}
	Version        uint64
	Comment        *string
	CreatedAt      time.Time
}

// AuditStore is the persistence backend for the GORM audit plugin.
// Implement this interface to write audit records to any storage system —
// MongoDB, Elasticsearch, a message queue, or a plain file.
//
// Inject a custom store via Config.Store:
//
//	db.Use(auditablegorm.New(auditablegorm.Config{
//	    Store: mongoaudit.NewMongoStore(client.Database("app").Collection("audits")),
//	}))
type AuditStore interface {
	// Save persists one audit record. Called synchronously after every mutation.
	Save(ctx context.Context, r *AuditRecord) error

	// NextVersion atomically allocates the next monotonically increasing version
	// for the entity. Custom stores must be safe across processes, not only
	// goroutines in one application instance.
	NextVersion(ctx context.Context, auditableType, auditableID string) (uint64, error)
}

// GormStore is the default AuditStore. It writes audit records as rows in the
// SQL table managed by the GORM *DB passed at plugin initialisation time.
type GormStore struct {
	db           *gorm.DB
	tableName    string
	versionTable string
}

// NewGormStore returns a GormStore backed by db. tableName is the SQL table
// used for audit rows; pass "" to use the default ("audits").
func NewGormStore(db *gorm.DB, tableName string) *GormStore {
	if tableName == "" {
		tableName = "audits"
	}
	return &GormStore{db: db, tableName: tableName, versionTable: tableName + "_versions"}
}

// conn returns a clean *gorm.DB for a store operation.
// When a transaction db was injected via withGormTx, a new session is created
// from it so the statement is reset while the transaction connection is kept.
func (s *GormStore) conn(ctx context.Context) *gorm.DB {
	if tx, ok := ctx.Value(gormTxKey{}).(*gorm.DB); ok && tx != nil {
		return tx.Session(&gorm.Session{NewDB: true})
	}
	return s.db.Session(&gorm.Session{NewDB: true})
}

// Save converts the AuditRecord to the GORM Audit model and inserts it.
func (s *GormStore) Save(ctx context.Context, r *AuditRecord) error {
	a := &Audit{
		AuditableID:    r.AuditableID,
		AuditableType:  r.AuditableType,
		UserID:         r.UserID,
		Action:         r.Action,
		AuditedChanges: datatypes.JSONMap(r.AuditedChanges),
		Version:        r.Version,
		Comment:        r.Comment,
		CreatedAt:      r.CreatedAt,
	}
	return s.conn(ctx).Table(s.tableName).Create(a).Error
}

// NextVersion atomically increments the entity's sequence row. The sequence
// table must be migrated with Migrate or MigrateWithTable.
func (s *GormStore) NextVersion(ctx context.Context, auditableType, auditableID string) (uint64, error) {
	digest := sha256.Sum256([]byte(auditableType + "\x00" + auditableID))
	seq := AuditVersion{Key: hex.EncodeToString(digest[:]), Version: 1}
	result := s.conn(ctx).Table(s.versionTable).Clauses(
		clause.OnConflict{
			Columns:   []clause.Column{{Name: "key"}},
			DoUpdates: clause.Assignments(map[string]interface{}{"version": gorm.Expr("version + 1")}),
		},
		clause.Returning{Columns: []clause.Column{{Name: "version"}}},
	).Create(&seq)
	if result.Error != nil {
		return 0, result.Error
	}
	return seq.Version, nil
}
