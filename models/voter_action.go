package models

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Actions represents allowed actions in voter_actions table (ENUM ACTIONS)
// Valid values: 'vote', 'print', 'whatsapp'
type Actions string

const (
	ActionVote     Actions = "mark_as_voted"
	ActionPrint    Actions = "print_voter_slip"
	ActionWhatsapp Actions = "send_to_whatsapp"
)

// VoterAction maps to the voter_actions table
// Corresponds to database/migrations/20251006143059_voter_actions.up.sql
type VoterAction struct {
	ID                int            `gorm:"column:id;primaryKey"`
	ProgramID         int            `gorm:"column:program_id;not null"`
	ProgramCampaignID int64          `gorm:"column:program_campaign_id;not null"`
	VoterID           string         `gorm:"column:voter_id;not null"`
	StateName         string         `gorm:"column:state_name;type:varchar(255);not null"`
	EpicNumber        string         `gorm:"column:epic_number;type:varchar(255);not null"`
	CountryStateID    int64          `gorm:"column:country_state_id"`
	AcID              int64          `gorm:"column:ac_id"`
	BoothID           int64          `gorm:"column:booth_id"`
	CreatedByID       int            `gorm:"column:created_by_id;not null"`
	Action            Actions        `gorm:"column:action;type:ACTIONS;not null"`
	Payload           datatypes.JSON `gorm:"column:payload;type:jsonb"`
	Latitude          float64        `gorm:"column:latitude"`
	Longitude         float64        `gorm:"column:longitude"`
	CreatedAt         time.Time      `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt         time.Time      `gorm:"column:updated_at;autoUpdateTime"`
	DeletedAt         gorm.DeletedAt `gorm:"column:deleted_at;index"`
}

func (VoterAction) TableName() string {
	return "voter_actions"
}

// Based on Single and Multiple Voter Actions

var (
	SingleVoterAction   = []Actions{ActionVote}
	MultipleVoterAction = []Actions{ActionPrint, ActionWhatsapp}
)

const (
	SingleVoterActionClass   = 1
	MultipleVoterActionClass = 2
)

// model helpers

//GetSubmissionType - Single or Multiple

func (a Actions) GetSubmissionType() int {
	switch a {
	case ActionVote:
		return SingleVoterActionClass
	case ActionPrint, ActionWhatsapp:
		return MultipleVoterActionClass
	default:
		return 0
	}
}
