package auditable

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Model is a portable base model that works with both a GORM SQL database
// and MongoDB. Embed it in any domain struct — that is the only step required
// to get full audit tracking, exactly like the Rails `audited` gem:
//
//	type Article struct {
//	    auditable.Model
//	    Title  string `gorm:"not null" bson:"title"`
//	    Status string `gorm:"default:'draft'" bson:"status"`
//	}
//
// ID is a string UUID auto-assigned by BeforeCreate. CreatedAt / UpdatedAt
// are managed automatically by GORM tags and by the MongoDB auditor on insert.
type Model struct {
	ID        string     `gorm:"primaryKey;size:36"   bson:"_id,omitempty"        json:"id"`
	CreatedAt time.Time  `gorm:"autoCreateTime"       bson:"created_at"           json:"created_at"`
	UpdatedAt time.Time  `gorm:"autoUpdateTime"       bson:"updated_at"           json:"updated_at"`
	DeletedAt *time.Time `gorm:"index"                bson:"deleted_at,omitempty" json:"deleted_at,omitempty"`
}

// BeforeCreate is called by GORM before every INSERT. It assigns a new UUID
// to ID when it is empty.
func (m *Model) BeforeCreate(_ *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	return nil
}
