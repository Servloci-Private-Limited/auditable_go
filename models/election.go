package models

import (
	"reflect"
	"strconv"
	"time"
	"unicode"

	"github.com/go-playground/validator/v10"
	"gorm.io/gorm"
)

type ElectionStatus string

const (
	ElectionStatusLive      ElectionStatus = "live"
	ElectionStatusCompleted ElectionStatus = "completed"
)

type ProgramType string

const (
	ProgramTypeVidhansabha      ProgramType = "vidhansabha"
	ProgramTypeMunicipalCorp    ProgramType = "municipal_corporation"
	ProgramTypeMunicipalCouncil ProgramType = "municipal_council"
	ProgramTypeNagarPanchayat   ProgramType = "nagar_panchayat"
	ProgramTypeGramPanchayat    ProgramType = "gram_panchayat"
)

func GetProgramTypes() []ProgramType {
	return []ProgramType{
		ProgramTypeVidhansabha,
		ProgramTypeMunicipalCorp,
		ProgramTypeMunicipalCouncil,
		ProgramTypeNagarPanchayat,
		ProgramTypeGramPanchayat,
	}
}

func (p ProgramType) IsValid() bool {
	switch p {
	case ProgramTypeVidhansabha, ProgramTypeMunicipalCorp, ProgramTypeMunicipalCouncil, ProgramTypeNagarPanchayat, ProgramTypeGramPanchayat:
		return true
	}
	return false
}

type Program struct {
	ID            int            `gorm:"primaryKey;autoIncrement" json:"id"`
	Name          string         `gorm:"type:varchar(200);not null" json:"program_name"`
	ProgramType   ProgramType    `gorm:"type:varchar(50);not null" json:"program_type"`
	ProgramNumber string         `gorm:"type:varchar(50);not null" json:"program_number"`
	ProgramBanner *string        `gorm:"column:banner_image;type:varchar(255)" json:"program_banner"`
	StateID       *int           `gorm:"not null" json:"state_id"`
	StateName     string         `gorm:"type:varchar(50);not null" json:"state_name"`
	Year          int            `gorm:"not null" json:"year"`
	Status        ElectionStatus `gorm:"type:program_status;default:'upcoming'" json:"status"`
	DataSource    *string        `gorm:"type:varchar(100)" json:"data_source" auditable:"false"`
	DataLevel     *string        `gorm:"type:varchar(100)" json:"data_level" auditable:"false"`
	CreatedByID   *int           `json:"created_by_id"`
	DeletedByID   *int           `json:"deleted_by_id" auditable:"false"`
	CreatedAt     time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt     *time.Time     `json:"deleted_at"`
}

func (Program) TableName() string {
	return "public.programs"
}

// Validate validates the Program struct
func (e *Program) Validate() error {
	validate := validator.New()

	// Custom validation for Name field
	_ = validate.RegisterValidation("validName", func(fl validator.FieldLevel) bool {
		name := fl.Field().String()
		if len(name) == 0 || len(name) > 100 {
			return false
		}
		if name[0] == ' ' {
			return false
		}
		// Check for special characters
		for _, r := range name {
			if !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != ' ' {
				return false
			}
		}
		return true
	})

	// Custom validation for ElectionNumber field
	_ = validate.RegisterValidation("validElectionNumber", func(fl validator.FieldLevel) bool {
		num := fl.Field().String()
		if num == "" {
			return true // Not mandatory
		}
		if num[0] == ' ' {
			return false
		}
		// Convert to integer and check if greater than 0
		intNum, err := strconv.Atoi(num)
		if err != nil || intNum <= 0 || intNum > 9999 {
			return false
		}
		return true
	})

	// Custom validation for Year field
	_ = validate.RegisterValidation("validYear", func(fl validator.FieldLevel) bool {
		currentYear := time.Now().Year()
		year := fl.Field().Int()
		return year <= int64(currentYear)+5
	})

	// Custom validation for ElectionBanner field
	_ = validate.RegisterValidation("validBanner", func(fl validator.FieldLevel) bool {
		banner := fl.Field().String()
		// Check if banner is provided
		return banner != ""
	})

	// Add validation tags to struct fields
	validate.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := fld.Tag.Get("json")
		return name
	})

	// Fix the RegisterStructValidation usage
	validate.RegisterStructValidation(func(sl validator.StructLevel) {
		election := sl.Current().Interface().(Program)

		// Validate Name
		if err := validate.Var(election.Name, "required,validName"); err != nil {
			sl.ReportError(election.Name, "program_name", "Name", "validName", "")
		}

		// Validate ProgramType is non-empty and one of the valid enum values
		if election.ProgramType == "" {
			sl.ReportError(election.ProgramType, "program_type", "ProgramType", "required", "")
		} else if !election.ProgramType.IsValid() {
			sl.ReportError(election.ProgramType, "program_type", "ProgramType", "oneof", "")
		}

		// Validate StateID
		if election.StateID == nil || *election.StateID == 0 {
			sl.ReportError(election.StateID, "state_id", "StateID", "required", "")
		}

		// Validate ElectionNumber
		if err := validate.Var(election.ProgramNumber, "validElectionNumber"); err != nil {
			sl.ReportError(election.ProgramNumber, "program_number", "ElectionNumber", "validElectionNumber", "")
		}

		// Validate Year
		if err := validate.Var(election.Year, "required,validYear"); err != nil {
			sl.ReportError(election.Year, "year", "Year", "validYear", "")
		}

		// Validate ElectionBanner
		if election.ProgramBanner == nil || *election.ProgramBanner == "" {
			sl.ReportError(election.ProgramBanner, "program_banner", "ElectionBanner", "required", "")
		}
	}, Program{})

	if err := validate.Struct(e); err != nil {

		return err
	}

	return nil
}

// BeforeCreate sets timestamps before creating a record
func (e *Program) BeforeCreate(tx *gorm.DB) error {
	// Set timestamps
	tx.Statement.SetColumn("created_at", time.Now())
	tx.Statement.SetColumn("updated_at", time.Now())
	tx.Statement.SetColumn("start_date", time.Now())
	tx.Statement.SetColumn("end_date", time.Now().Add(360*24*time.Hour)) // Example: 1 year later

	return nil
}
