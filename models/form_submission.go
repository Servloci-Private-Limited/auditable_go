package models

import (
	"reflect"
	"time"

	"database/sql/driver"
	"encoding/json"
	"errors"

	"gorm.io/gorm"
)

type PersistentData map[string]any
type SubmissionStatus string

const (
	StatusPending        SubmissionStatus = "pending"
	StatusSubmitted      SubmissionStatus = "submitted"
	StatusInvalidBySelf  SubmissionStatus = "invalid_self"
	StatusInvalidByOther SubmissionStatus = "invalid_other"
)

type FormWebHook struct {
	Eventmeta struct {
		Formid       string `json:"formid"`
		Submissionid string `json:"submissionid"`
		EntityType   string `json:"entity_type"`
		EntityID     int64  `json:"entity_id"`
	} `json:"eventmeta"`
	Eventdata []struct {
		Question []string `json:"question"`
		Answer   []string `json:"answer"`
	} `json:"eventdata"`
}

// FormSubmission represents a form submission record
type FormSubmission struct {
	ID                int              `gorm:"primaryKey;autoIncrement" json:"id"`
	ProgramID         int              `gorm:"not null" json:"program_id" validate:"required"`
	ProgramCampaignID int64            `gorm:"not null" json:"program_campaign_id" validate:"required"`
	CampaignFormID    int64            `gorm:"not null" json:"campaign_form_id" validate:"required"`
	SubmissionID      string           `gorm:"type:varchar(255);not null;unique" json:"submission_id" validate:"required"`
	FormID            string           `gorm:"type:varchar(255);not null" json:"form_id" validate:"required"`
	AcID              *int64           `gorm:"type:int" json:"ac_id"`
	BoothID           *int64           `gorm:"type:int" json:"booth_id"`
	StateID           *int64           `gorm:"-" json:"state_id" auditable:"false"`
	PersistentData    PersistentData   `gorm:"type:jsonb" json:"persistent_data" auditable:"redact"`
	SubmissionData    SubmissionData   `gorm:"type:jsonb" json:"submission_data"`
	ValidTill         *time.Time       `gorm:"column:valid_till" json:"valid_till"`
	CreatedByID       *int             `gorm:"column:created_by_id" json:"created_by_id"`
	Latitude          float64          `gorm:"column:latitude" json:"latitude" auditable:"false"`
	Longitude         float64          `gorm:"column:longitude" json:"longitude" auditable:"false"`
	CreatedAt         time.Time        `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time        `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt         gorm.DeletedAt   `json:"deleted_at"`
	Status            SubmissionStatus `gorm:"type:varchar(255);not null" json:"status"`
	StatusAt          *time.Time       `gorm:"column:status_at" json:"status_at"`

	// Relationships (not persisted as own columns — excluded from audit)
	Program         *Campaign     `gorm:"foreignKey:ProgramID" json:"program,omitempty" auditable:"false"`
	ProgramCampaign *Campaign     `gorm:"foreignKey:ProgramCampaignID" json:"program_campaign,omitempty" auditable:"false"`
	CampaignForm    *CampaignForm `gorm:"foreignKey:CampaignFormID" json:"campaign_form,omitempty" auditable:"false"`
	EntityType      string        `gorm:"type:varchar(255)" json:"entity_type"`
	EntityID        string        `gorm:"type:varchar(255)" json:"entity_id"`
}

func (FormSubmission) TableName() string {
	return "public.form_submissions"
}

func (fs *FormSubmission) BeforeCreate(tx *gorm.DB) error {
	if fs.Status == "" {
		fs.Status = StatusPending
	}
	return nil
}

type FormSubmissionStatus struct {
	SubmissionId   string `json:"submission_id"`
	FormType       string `json:"form_type"`
	CampaignFormId int64  `json:"campaign_form_id,omitempty"`
	Status         bool   `json:"status"`
	Message        string `json:"message"`
}

type SubmissionStatusResponse struct {
	Data        map[string]SubmissionStatusData `json:"data"`
	StatusCount []map[string]int                `json:"statusCount"`
	Message     string                          `json:"message"`
}

type SubmissionStatusData struct {
	Status       string        `json:"status"`
	SubmissionID string        `json:"submissionId"`
	CreatedAt    string        `json:"createdAt"`
	UpdatedAt    string        `json:"updatedAt"`
	Locations    []interface{} `json:"locations"`
	Images       []interface{} `json:"images"`
}

type SubmissionData struct {
	NormalQuestions []struct {
		BindingKey *string `json:"bindingKey"`
		Question   []struct {
			Id    string `json:"id"`
			Value string `json:"value"`
		} `json:"question"`
		AnswerKeyTypes [][]string `json:"answerKeyTypes"`
		AnswerKeys     []struct {
			QuestionId string `json:"questionId"`
			Value      string `json:"value"`
		} `json:"answerKeys"`
		Answer []struct {
			QuestionId string   `json:"questionId"`
			Value      []string `json:"value"`
		} `json:"answer"`
		Section []interface{} `json:"section"`
	} `json:"normalQuestions"`
	PersistantData  map[string]interface{} `json:"persistantData"`
	MatrixQuestions []struct {
		BindingKey interface{} `json:"bindingKey"`
		Question   []struct {
			Id    string `json:"id"`
			Value string `json:"value"`
		} `json:"question"`
		AnswerKeyTypes [][]string `json:"answerKeyTypes"`
		AnswerKeys     []struct {
			QuestionId string `json:"questionId"`
			Value      string `json:"value"`
		} `json:"answerKeys"`
		Answer [][]struct {
			QuestionId *string  `json:"questionId"`
			Value      []string `json:"value"`
		} `json:"answer"`
	} `json:"matrixQuestions"`
}

type FormResponse struct {
	FormId         string         `json:"formId"`
	SubmissionId   string         `json:"submissionId"`
	SubmissionData SubmissionData `json:"submissionData"`
	Status         string         `json:"status"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

// Value implements the driver.Valuer interface for SubmissionData
func (m SubmissionData) Value() (driver.Value, error) {
	if reflect.DeepEqual(m, SubmissionData{}) {
		return nil, nil
	}
	return json.Marshal(m)
}

// Scan implements the sql.Scanner interface for SubmissionData
func (m *SubmissionData) Scan(value interface{}) error {
	if value == nil {
		*m = SubmissionData{}
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("failed to unmarshal JSONB value: not a []byte")
	}

	var temp SubmissionData
	err := json.Unmarshal(bytes, &temp)
	if err != nil {
		return err
	}

	*m = temp
	return nil
}

// Value implements the driver.Valuer interface for JSONMap
func (jsonb PersistentData) Value() (driver.Value, error) {
	if len(jsonb) == 0 {
		return nil, nil
	}
	return json.Marshal(jsonb)
}

// Scan implements the sql.Scanner interface for JSONMap
func (jsonb *PersistentData) Scan(value interface{}) error {
	if value == nil {
		*jsonb = make(PersistentData)
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("failed to unmarshal JSONB value: not a []byte")
	}

	result := make(PersistentData)
	err := json.Unmarshal(bytes, &result)
	if err != nil {
		return err
	}

	*jsonb = result
	return nil
}
