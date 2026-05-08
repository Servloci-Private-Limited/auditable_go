package models

import (
	"time"

	"gorm.io/gorm"
)

type FormSubmissionMultiple struct {
	ID                int              `gorm:"primaryKey;autoIncrement" json:"id"`
	ProgramID         int              `gorm:"not null" json:"program_id" validate:"required"`
	ProgramCampaignID int64            `gorm:"not null" json:"program_campaign_id" validate:"required"`
	CampaignFormID    int64            `gorm:"not null" json:"campaign_form_id" validate:"required"`
	SubmissionID      string           `gorm:"type:varchar(255);not null;unique" json:"submission_id" validate:"required"`
	FormID            string           `gorm:"type:varchar(255);not null" json:"form_id" validate:"required"`
	PersistentData    *PersistentData  `gorm:"type:jsonb" json:"persistent_data"`
	SubmissionData    *SubmissionData  `gorm:"type:jsonb" json:"submission_data"`
	CreatedByID       *int             `json:"created_by_id"`
	Latitude          float64          `gorm:"column:latitude" json:"latitude"`
	Longitude         float64          `gorm:"column:longitude" json:"longitude"`
	CreatedAt         time.Time        `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time        `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt         gorm.DeletedAt   `json:"deleted_at"`
	Status            SubmissionStatus `gorm:"type:varchar(255);not null" json:"status"`
	StatusAt          *time.Time       `gorm:"column:status_at" json:"status_at"` // Optional, can be null

	// Relationships (optional - for eager loading)
	Program         *Campaign     `gorm:"foreignKey:ProgramID" json:"program,omitempty"`
	ProgramCampaign *Campaign     `gorm:"foreignKey:ProgramCampaignID" json:"program_campaign,omitempty"`
	CampaignForm    *CampaignForm `gorm:"foreignKey:CampaignFormID" json:"campaign_form,omitempty"`
	StateID         *int          `gorm:"-" json:"state_id"`
}

func (FormSubmissionMultiple) TableName() string {
	return "public.form_submissions_multiple"
}
func (fs *FormSubmissionMultiple) BeforeCreate(tx *gorm.DB) error {
	if fs.Status == "" {
		fs.Status = StatusPending
	}
	return nil
}
