package auditablegorm

import (
	"time"

	"gorm.io/datatypes"
)

type Action string

const (
	ActionCreate Action = "create"
	ActionUpdate Action = "update"
	ActionDelete Action = "delete"
)

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

func (Audit) TableName() string {
	return "audits"
}
