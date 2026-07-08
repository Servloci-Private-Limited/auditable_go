package auditablegorm

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Model is a GORM base model. Embed it in any domain struct to get
// gorm.Model's fields (ID uint, CreatedAt, UpdatedAt, DeletedAt) plus full
// audit tracking — identical to the Rails `audited` gem pattern:
//
//	type Article struct {
//	    auditablegorm.Model
//	    Title  string `gorm:"not null"`
//	    Status string `gorm:"default:'draft'"`
//	}
//
// Register the plugin once and every create/update/delete on any model that
// embeds auditablegorm.Model is automatically tracked.
type Model struct {
	gorm.Model
}

type Action string

const (
	ActionCreate Action = "create"
	ActionUpdate Action = "update"
	ActionDelete Action = "delete"
)

type Audit struct {
	ID             uint64            `gorm:"primaryKey"`
	AuditableID    string            `gorm:"size:191;index:idx_auditable_lookup,priority:2;uniqueIndex:uniq_auditable_version,priority:2;not null"`
	AuditableType  string            `gorm:"size:191;index:idx_auditable_lookup,priority:1;uniqueIndex:uniq_auditable_version,priority:1;not null"`
	UserID         *string           `gorm:"size:191;index"`
	Action         Action            `gorm:"size:16;index;not null"`
	AuditedChanges datatypes.JSONMap `gorm:"type:json;not null"`
	Version        uint64            `gorm:"uniqueIndex:uniq_auditable_version,priority:3;not null"`
	Comment        *string           `gorm:"size:512"`
	CreatedAt      time.Time         `gorm:"not null"`
}

// AuditVersion is the atomic per-entity sequence used by GormStore. Sequence
// increments are committed in the same transaction as the audit row, so gaps
// are allowed but duplicate versions are not produced by concurrent writers.
type AuditVersion struct {
	Key     string `gorm:"primaryKey;size:64"`
	Version uint64 `gorm:"not null"`
}

func (AuditVersion) TableName() string { return "audit_versions" }

func (Audit) TableName() string {
	return "audits"
}

// Migrate creates or updates the audits table. Call once at startup alongside
// your own db.AutoMigrate(...) — no need to reference &Audit{} directly:
//
//	auditablegorm.Migrate(db)
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(&Audit{}, &AuditVersion{})
}

// MigrateWithTable migrates a custom audit table and its sequence table.
func MigrateWithTable(db *gorm.DB, tableName string) error {
	if tableName == "" {
		tableName = "audits"
	}
	if err := db.Table(tableName).AutoMigrate(&Audit{}); err != nil {
		return err
	}
	return db.Table(tableName + "_versions").AutoMigrate(&AuditVersion{})
}
