package models

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

type FormType string

const (
	OutreachFormType    FormType = "outreach"
	DeleteVoterFormType FormType = "request_to_delete_voter"
	AddVoterFormType    FormType = "request_to_add_voter"
	WhatsappFormType    FormType = "send_to_whatsapp"
	VoterPrintFormType  FormType = "print_voter_slip"
	MarkAsVotedFormType FormType = "mark_as_voted"
	TagFilterFormType   FormType = "tag_filter"
	PrintFormType       FormType = "print_voter_slip"
)

var ActionForms = []FormType{
	WhatsappFormType,
	MarkAsVotedFormType,
	PrintFormType,
}

var (
	SingleFormType   = "single"
	MultipleFormType = "multiple"
)

type BindingKeyType string

const (
	EpicNumberType BindingKeyType = "epicNumber"
)

var (
	DeleteVoterFormBindKeys = []BindingKeyType{EpicNumberType}
)

func SingleFormSubmissionTypes() []FormType {
	return []FormType{OutreachFormType, DeleteVoterFormType}
}

func MultipleFormSubmissionTypes() []FormType {
	return []FormType{AddVoterFormType}
}

type Form struct {
	ID          int       `json:"id" gorm:"primaryKey"`
	Name        string    `json:"name" gorm:"size:255;not null"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

type CampaignForm struct {
	ID                int            `json:"id" gorm:"primaryKey;autoIncrement"`
	ProgramID         int            `json:"program_id" gorm:"not null"`
	ProgramCampaignID int64          `json:"program_campaign_id" gorm:"not null"`
	FormType          *FormType      `json:"form_type" gorm:"type:varchar(255)"`
	FormID            *string        `json:"form_id" gorm:"type:uuid"`
	Title             *string        `json:"title" gorm:"type:varchar(255)"`
	Description       *string        `json:"description" gorm:"type:varchar(255)"`
	IsPublished       bool           `json:"is_published" gorm:"default:false"`
	Enabled           int            `json:"enabled" gorm:"type:smallint"`
	Order             int            `json:"order,omitempty" gorm:"column:order;check:\"order\" > 0"`
	SubmissionType    *string        `json:"submission_type,omitempty" gorm:"-" auditable:"false"`
	MaxSubmission     int            `json:"max_submission,omitempty" gorm:"default:1"`
	CreateLink        string         `json:"create_link,omitempty" gorm:"-" auditable:"false"`
	SubmitLink        string         `json:"-" gorm:"-" auditable:"false"`
	CreatedByID       *int           `json:"-"`
	DeletedByID       *int           `json:"-" auditable:"false"`
	CreatedAt         time.Time      `json:"-" gorm:"autoCreateTime"`
	UpdatedAt         time.Time      `json:"-" gorm:"autoUpdateTime"`
	DeletedAt         gorm.DeletedAt `json:"-"`
}

type CampaignFormPublicView struct {
	ID                int       `json:"id" gorm:"primaryKey;autoIncrement"`
	ProgramID         int       `json:"program_id" gorm:"not null"`
	ProgramCampaignID int64     `json:"program_campaign_id" gorm:"not null"`
	FormType          *FormType `json:"form_type" gorm:"type:varchar(255)"`
	FormID            *string   `json:"form_id" gorm:"type:uuid"`

	IsPublished bool `json:"is_published" gorm:"default:false"`
	Enabled     int  `json:"enabled" gorm:"type:smallint"`
}

func (f *CampaignForm) TableName() string {
	return "campaign_forms"
}

func (f *CampaignFormPublicView) TableName() string {
	return "campaign_forms"
}

func (f *CampaignForm) GenerateCreateToken() string {
	eventID := fmt.Sprintf("%d-%d", f.ProgramCampaignID, f.FormID)

	return eventID
}

func (f *CampaignForm) GenerateSubmissionToken() string {
	eventID := fmt.Sprintf("%d-%d", f.ProgramCampaignID, f.FormID)

	return eventID
}

type User struct {
	Name string `json:"name"`
}

type EventMeta struct {
	RedirectionLinkCreate string `json:"createRedirectionLink,omitempty"`
	RedirectionLinkSubmit string `json:"redirectionLink,omitempty"`
}

type TokenData struct {
	EventId       string    `json:"eventId"`
	FormId        string    `json:"formId"`
	EventName     string    `json:"eventName"`
	IsFormCreator bool      `json:"isFormCreator"`
	SubmissionID  string    `json:"submissionId"`
	EventMeta     EventMeta `json:"eventMeta,omitempty"`
}

type CampaignFormEnable struct {
	Enabled int `json:"enabled" gorm:"type:smallint"`
}

type CampaignFormDisable struct {
	Enabled int `json:"enabled" gorm:"type:smallint"`
}
