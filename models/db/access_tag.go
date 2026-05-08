package db

import (
	"fmt"
	"time"
)

type ProgramRuleMapping struct {
	ID               int        `gorm:"column:id"`
	ElectionID       int        `gorm:"column:program_id"`
	RuleID           int        `gorm:"column:rule_id"`
	CountryStateID   int        `gorm:"column:country_state_id"`
	CountryStateName string     `gorm:"column:country_state_name"`
	DataTypeName     string     `gorm:"column:data_type_name"`
	DataLevelID      int        `gorm:"column:data_level_id"`
	DataLevelName    string     `gorm:"column:data_level_name"`
	DataUnitName     string     `gorm:"column:data_unit_name"`
	SubUnitName      string     `gorm:"column:sub_unit_name"`
	DesignationName  string     `gorm:"column:designation_name"`
	CreatedByID      int        `gorm:"column:created_by_id"`
	DeletedByID      *int       `gorm:"column:deleted_by_id;default:null"`
	Slug             string     `gorm:"column:slug"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
	DeletedAt        *time.Time `gorm:"column:deleted_at;default:null"`
	UpdatedAt        time.Time  `gorm:"column:updated_at"`
}

func CreateSlug(dataTypeID int, dataLevelID int, dataUnitID int, subUnitID int, designationID int) string {
	return fmt.Sprintf("%d-%d-%d-%d-%d", dataTypeID, dataLevelID, dataUnitID, subUnitID, designationID)
}
