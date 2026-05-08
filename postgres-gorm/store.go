package auditablegorm

import (
	"context"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

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

	// NextVersion returns the next monotonically increasing version number for
	// the given (auditableType, auditableID) entity pair.
	NextVersion(ctx context.Context, auditableType, auditableID string) (uint64, error)
}

// GormStore is the default AuditStore. It writes audit records as rows in the
// SQL table managed by the GORM *DB passed at plugin initialisation time.
type GormStore struct {
	db        *gorm.DB
	tableName string
}

// NewGormStore returns a GormStore backed by db. tableName is the SQL table
// used for audit rows; pass "" to use the default ("audits").
func NewGormStore(db *gorm.DB, tableName string) *GormStore {
	if tableName == "" {
		tableName = "audits"
	}
	return &GormStore{db: db, tableName: tableName}
}

// Save converts the AuditRecord to the GORM Audit model and inserts it.
func (s *GormStore) Save(_ context.Context, r *AuditRecord) error {
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
	return s.db.Session(&gorm.Session{NewDB: true}).Table(s.tableName).Create(a).Error
}

// NextVersion queries the SQL audit table for the current maximum version of
// the entity and returns max+1.
func (s *GormStore) NextVersion(_ context.Context, auditableType, auditableID string) (uint64, error) {
	var v uint64
	result := s.db.Session(&gorm.Session{NewDB: true}).
		Model(&Audit{}).
		Table(s.tableName).
		Where("auditable_type = ? AND auditable_id = ?", auditableType, auditableID).
		Select("COALESCE(MAX(version), 0)").
		Scan(&v)
	if result.Error != nil {
		return 1, result.Error
	}
	return v + 1, nil
}
