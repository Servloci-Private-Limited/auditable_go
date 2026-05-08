package models

import (
	"fmt"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type Campaign struct {
	ID                    int            `gorm:"primaryKey;autoIncrement" json:"id"`
	ProgramID             int            `gorm:"not null,column:program_id" json:"program_id" validate:"required"`
	Name                  string         `gorm:"type:varchar(255);not null" json:"name" validate:"required,max=100,alpha_numeric_space,trim_space"`
	ProgramType           string         `gorm:"->;column:program_type;<-:false" json:"program_type" auditable:"false"`
	Description           *string        `gorm:"type:text" json:"description"`
	BannerImage           *string        `gorm:"type:text" json:"logo_url"`
	CampaignType          *string        `gorm:"type:varchar(50)" json:"campaign_type"`
	URL                   *string        `gorm:"type:text" json:"url"`
	Status                string         `gorm:"type:campaign_status;default:'draft';not null" json:"is_published" validate:"required,oneof=draft active paused completed published"`
	OutreachFormPublished bool           `gorm:"-" json:"outreach_form_published" auditable:"false"`
	Metadata              datatypes.JSON `gorm:"type:jsonb" json:"metadata"`
	CreatedByID           *int           `json:"created_by_id"`
	DeletedByID           *int           `json:"deleted_by_id" auditable:"false"`
	CreatedAt             time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt             time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt             gorm.DeletedAt `json:"deleted_at"`
}

func RedisCampaignKey(id int64) string {
	return fmt.Sprintf("voter-outreach-program-campaign:%d", id)
}

// ValidationError represents a single validation error
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidateEdit validates the Campaign struct for edit operations

func (Campaign) TableName() string {
	return "program_campaigns"
}
