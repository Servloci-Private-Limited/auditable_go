package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// CampaignActionMeta holds typed display/behaviour config stored as JSONB.
type CampaignActionMeta struct {
	Sticky     string `json:"sticky"`
	Expandable bool   `json:"expandable"`
}

func (m CampaignActionMeta) Value() (driver.Value, error) {
	b, err := json.Marshal(m)
	return string(b), err
}

func (m *CampaignActionMeta) Scan(src interface{}) error {
	var data []byte
	switch v := src.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		return nil
	}
	return json.Unmarshal(data, m)
}

type CampaignAction struct {
	ID uint64 `gorm:"primaryKey;autoIncrement"`

	// Relationships
	ProgramID         int    `gorm:"not null;index"`
	ProgramCampaignID uint64 `gorm:"not null;index"`

	// Grouping & Identity
	GroupName  string `gorm:"type:varchar(100);not null;index" json:"group_name"`
	ActionName string `gorm:"type:varchar(100);not null" json:"title"`

	// Content
	ActionText string `gorm:"type:text" json:"description"`

	// State
	Status string `gorm:"type:varchar(50);default:'active';index" json:"status"`

	// Flexible config
	Meta CampaignActionMeta `gorm:"type:jsonb;default:'{}' " json:"meta" auditable:"false"`

	// Execution control
	Priority       int `gorm:"default:0" json:"priority"`
	ExecutionOrder int `gorm:"default:0" json:"-" auditable:"false"`

	// Audit
	CreatedByID int        `gorm:"not null" json:"created_by_id"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `gorm:"index" json:"deleted_at"`

	// Optional: relation (if you use preload)
	ProgramCampaign Campaign `gorm:"foreignKey:ProgramCampaignID;constraint:OnDelete:CASCADE" json:"-" auditable:"false"`

	// Unique constraint
	// (program_campaign_id, group_name, action_name)
}

func (CampaignAction) TableName() string {
	return "campaign_actions"
}
