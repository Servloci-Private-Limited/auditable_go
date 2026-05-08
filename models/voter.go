package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

var epicRegex = regexp.MustCompile(`^[a-zA-Z0-9/\\-]+$`)

type VoterLBDetails struct {
	ADID          int64  `bson:"ad_id,omitempty" json:"ad_id,omitempty"`
	ADName        string `bson:"ad_name,omitempty" json:"ad_name,omitempty"`
	BlockID       int64  `bson:"block_id,omitempty" json:"block_id,omitempty"`
	BlockName     string `bson:"block_name,omitempty" json:"block_name,omitempty"`
	LocalBodyID   int64  `bson:"local_body_id,omitempty" json:"local_body_id,omitempty"`
	LocalBodyType string `bson:"local_body_type,omitempty" json:"local_body_type,omitempty"`
	LocalBodyName string `bson:"local_body_name,omitempty" json:"local_body_name,omitempty"`
	WardID        int64  `bson:"ward_id,omitempty" json:"ward_id,omitempty"`
	WardName      string `bson:"ward_name,omitempty" json:"ward_name,omitempty"`
	WardNumber    int64  `bson:"ward_number,omitempty" json:"ward_number,omitempty"`
	BoothID       int64  `bson:"booth_id,omitempty" json:"booth_id,omitempty"`
	BoothName     string `bson:"booth_name,omitempty" json:"booth_name,omitempty"`
	BoothNumber   int64  `bson:"booth_number,omitempty" json:"booth_number,omitempty"`
	SerialNumber  int64  `bson:"serial_number,omitempty" json:"serial_number,omitempty"`
}

// VoterADDetails captures the "ad" subdocument written by the newer sheet upload path.
// Booth fields are prefixed "lb_booth_*" (unlike VoterLBDetails which uses "booth_*").
type VoterADDetails struct {
	ADID          int64  `bson:"ad_id,omitempty"`
	ADName        string `bson:"ad_name,omitempty"`
	BlockID       int64  `bson:"block_id,omitempty"`
	BlockName     string `bson:"block_name,omitempty"`
	LocalBodyID   int64  `bson:"local_body_id,omitempty"`
	LocalBodyType string `bson:"local_body_type,omitempty"`
	LocalBodyName string `bson:"local_body_name,omitempty"`
	WardID        int64  `bson:"ward_id,omitempty"`
	WardName      string `bson:"ward_name,omitempty"`
	WardNumber    int64  `bson:"ward_number,omitempty"`
	LBBoothID     int64  `bson:"lb_booth_id,omitempty"`
	LBBoothName   string `bson:"lb_booth_name,omitempty"`
	LBBoothNumber int64  `bson:"lb_booth_number,omitempty"`
}

// VoterLBResult is used when reading LB voter documents from MongoDB.
// Voter documents may be stored in three formats depending on the ingest path:
//  1. Nested under "local_body.*" — written by BulkUpsertMongo (legacy migration)
//  2. Nested under "ad.*"         — written by newer sheet upload path (lb_booth_* fields)
//  3. Flat at document root       — written by voter_copy_pg_to_mongo.go
//
// ResolveLB() coalesces all three, preferring local_body → ad → flat root.
type VoterLBResult struct {
	VoterBase `bson:",inline"`
	// Format 1: local_body.* subdocument
	LocalBody *VoterLBDetails `bson:"local_body,omitempty"`
	// Format 2: ad.* subdocument (newer upload path, lb_booth_* field names)
	AD *VoterADDetails `bson:"ad,omitempty"`
	// Format 3: flat root-level fields (voter_copy_pg_to_mongo.go)
	ADID          int64  `bson:"ad_id,omitempty"`
	ADName        string `bson:"ad_name,omitempty"`
	BlockID       int64  `bson:"block_id,omitempty"`
	BlockName     string `bson:"block_name,omitempty"`
	LocalBodyID   int64  `bson:"local_body_id,omitempty"`
	LocalBodyType string `bson:"local_body_type,omitempty"`
	LocalBodyName string `bson:"local_body_name,omitempty"`
	WardID        int64  `bson:"ward_id,omitempty"`
	WardName      string `bson:"ward_name,omitempty"`
	WardNumber    int64  `bson:"ward_number,omitempty"`
	BoothID       int64  `bson:"booth_id,omitempty"`
	BoothName     string `bson:"booth_name,omitempty"`
	BoothNumber   int64  `bson:"booth_number,omitempty"`
}

// ResolveLB returns the effective LB location details for this voter.
// Priority: local_body subdocument → ad subdocument → flat root fields.
func (v *VoterLBResult) ResolveLB() VoterLBDetails {
	// Format 1: local_body.* (BulkUpsertMongo / legacy migration)
	if v.LocalBody != nil && (v.LocalBody.ADName != "" || v.LocalBody.LocalBodyName != "" || v.LocalBody.BlockName != "" || v.LocalBody.WardName != "") {
		return *v.LocalBody
	}
	// Format 2: ad.* (newer sheet upload path — booth fields are lb_booth_*)
	if v.AD != nil && (v.AD.ADName != "" || v.AD.LocalBodyName != "") {
		return VoterLBDetails{
			ADID:          v.AD.ADID,
			ADName:        v.AD.ADName,
			BlockID:       v.AD.BlockID,
			BlockName:     v.AD.BlockName,
			LocalBodyID:   v.AD.LocalBodyID,
			LocalBodyType: v.AD.LocalBodyType,
			LocalBodyName: v.AD.LocalBodyName,
			WardID:        v.AD.WardID,
			WardName:      v.AD.WardName,
			WardNumber:    v.AD.WardNumber,
			BoothID:       v.AD.LBBoothID,
			BoothName:     v.AD.LBBoothName,
			BoothNumber:   v.AD.LBBoothNumber,
		}
	}
	// Format 3: flat root fields (voter_copy_pg_to_mongo.go)
	return VoterLBDetails{
		ADID:          v.ADID,
		ADName:        v.ADName,
		BlockID:       v.BlockID,
		BlockName:     v.BlockName,
		LocalBodyID:   v.LocalBodyID,
		LocalBodyType: v.LocalBodyType,
		LocalBodyName: v.LocalBodyName,
		WardID:        v.WardID,
		WardName:      v.WardName,
		WardNumber:    v.WardNumber,
		BoothID:       v.BoothID,
		BoothName:     v.BoothName,
		BoothNumber:   v.BoothNumber,
	}
}

type VoterVidhansabhaDetails struct {
	ACID         int64  `bson:"ac_id,omitempty" json:"ac_id,omitempty"`
	ACName       string `bson:"ac_name,omitempty" json:"ac_name,omitempty"`
	ACNumber     int64  `bson:"ac_number,omitempty" json:"ac_number,omitempty"`
	BoothID      int64  `bson:"booth_id,omitempty" json:"booth_id,omitempty"`
	BoothName    string `bson:"booth_name,omitempty" json:"booth_name,omitempty"`
	BoothNumber  int64  `bson:"booth_number,omitempty" json:"booth_number,omitempty"`
	SerialNumber int64  `bson:"serial_number,omitempty" json:"serial_number,omitempty"`
}

type VoterBase struct {
	ID           string `bson:"_id,omitempty" json:"id,omVoterResponseitempty"`
	Name         string `bson:"name,omitempty" json:"name,omitempty"`
	RelationName string `bson:"relation_name,omitempty" json:"relation_name,omitempty"`
	RelationType string `bson:"relation_type,omitempty" json:"relation_type,omitempty"`
	HouseNumber  string `bson:"house_number,omitempty" json:"house_number,omitempty"`
	Age          int    `bson:"age,omitempty" json:"age,omitempty"`
	Gender       string `bson:"gender,omitempty" json:"gender,omitempty"`
	PageNumber   int    `bson:"page_number,omitempty" json:"page_number,omitempty"`
	EpicNumber   string `bson:"epic_number,omitempty" json:"epic_number,omitempty"`
	StateID      int64  `bson:"state_id,omitempty" json:"state_id,omitempty"`
	StateName    string `bson:"state_name,omitempty" json:"state_name,omitempty"`
}

// VoterAtlas represents the DB model with metadata
type VoterAtlas struct {
	Score                   float64 `bson:"search_score,omitempty" json:"search_score,omitempty"`
	VoterBase               `bson:",inline" json:",inline"`
	VoterVidhansabhaDetails `bson:",inline" json:",inline"`
	VoterLBDetails          `bson:"local_body,omitempty" json:"local_body,omitempty"`
	ActionedCampaigns       []string   `bson:"actioned_campaigns,omitempty" json:"actioned_campaigns,omitempty"`
	Tags                    VoterTags  `bson:"tags,omitempty" json:"tags,omitempty"`
	CreatedAt               time.Time  `bson:"created_at,omitempty" json:"created_at,omitempty"`
	UpdatedAt               time.Time  `bson:"updated_at,omitempty" json:"updated_at,omitempty"`
	DeletedAt               *time.Time `bson:"deleted_at,omitempty" json:"deleted_at,omitempty"`
	DeletedBy               *UserRef   `bson:"deleted_by,omitempty" json:"deleted_by,omitempty"`
}

// VoterResponses represents the API response model with extra status fields
type VoterResponseForUser struct {
	VoterResponse
	Forms   []VoterFormInfo   `bson:"forms" json:"forms"`
	Actions []VoterActionInfo `bson:"actions" json:"actions"`
}

type UpsertDetails struct {
	Voter         VoterAtlas
	FieldPresence map[string]bool
}

type UpsertFailure struct {
	EpicNumber string
	ErrorMsg   string
}

type VoterResponse struct {
	VoterBase
	ADID          int64     `bson:"ad_id,omitempty" json:"ad_id,omitempty"`
	ADName        string    `bson:"ad_name,omitempty" json:"ad_name,omitempty"`
	BlockID       int64     `bson:"block_id,omitempty" json:"block_id,omitempty"`
	BlockName     string    `bson:"block_name,omitempty" json:"block_name,omitempty"`
	LocalBodyID   int64     `bson:"local_body_id,omitempty" json:"local_body_id,omitempty"`
	LocalBodyType string    `bson:"local_body_type,omitempty" json:"local_body_type,omitempty"`
	LocalBodyName string    `bson:"local_body_name,omitempty" json:"local_body_name,omitempty"`
	WardID        int64     `bson:"ward_id,omitempty" json:"ward_id,omitempty"`
	WardName      string    `bson:"ward_name,omitempty" json:"ward_name,omitempty"`
	WardNumber    int64     `bson:"ward_number,omitempty" json:"ward_number,omitempty"`
	ACID          int64     `bson:"ac_id,omitempty" json:"ac_id,omitempty"`
	ACName        string    `bson:"ac_name,omitempty" json:"ac_name,omitempty"`
	ACNumber      int64     `bson:"ac_number,omitempty" json:"ac_number,omitempty"`
	BoothID       int64     `bson:"booth_id,omitempty" json:"booth_id,omitempty"`
	BoothName     string    `bson:"booth_name,omitempty" json:"booth_name,omitempty"`
	BoothNumber   int64     `bson:"booth_number,omitempty" json:"booth_number,omitempty"`
	SerialNumber  int64     `bson:"serial_number,omitempty" json:"serial_number,omitempty"`
	Tags          []TagInfo `bson:"tags_info,omitempty" json:"tags_info"`
}

type TagInfo struct {
	Key   string `json:"key"`
	Color string `json:"color"`
}

func (v *VoterAtlas) ValidateVoter(fieldPresence map[string]bool) error {
	// Validate EPIC number (always required)
	if !epicRegex.MatchString(v.EpicNumber) || len(v.EpicNumber) < 10 || len(v.EpicNumber) > 20 {
		return fmt.Errorf("invalid epic number format")
	}

	// Only validate fields that exist in the CSV input
	if fieldPresence["name"] {
		if len(v.Name) < 2 || len(v.Name) > 60 {
			return fmt.Errorf("voter name must be 2-60 characters")
		}
	}

	if fieldPresence["relation_name"] {
		if len(v.RelationName) < 2 || len(v.RelationName) > 60 {
			return fmt.Errorf("guardian name must be 2-60 characters")
		}
	}

	if fieldPresence["relation_type"] {
		if len(v.RelationType) < 2 || len(v.RelationType) > 30 {
			return fmt.Errorf("relation type must be 2-30 characters")
		}
	}

	if fieldPresence["house_number"] {
		if len(v.HouseNumber) < 1 {
			return fmt.Errorf("house number is required")
		}
	}

	if fieldPresence["page_number"] {
		if v.PageNumber <= 0 {
			return fmt.Errorf("page number must be greater than 0")
		}
	}

	if fieldPresence["serial_number"] {
		if v.VoterVidhansabhaDetails.SerialNumber <= 0 {
			return fmt.Errorf("serial number must be greater than 0")
		}
	}

	if fieldPresence["lb_serial_number"] {
		if v.VoterLBDetails.SerialNumber <= 0 {
			return fmt.Errorf("serial number must be greater than 0")
		}
	}

	if fieldPresence["age"] {
		if v.Age < 18 || v.Age > 123 {
			return fmt.Errorf("age must be 18-123")
		}
	}

	if fieldPresence["gender"] {
		if v.Gender != "M" && v.Gender != "F" && v.Gender != "O" {
			return fmt.Errorf("gender must be M/Male, F/Female, or O/Other")
		}
	}

	// -------- Combination Fields --------
	state := fieldPresence["state_name"]

	ac := fieldPresence["ac_number"]
	booth := fieldPresence["booth_number"]

	ad := fieldPresence["ad_name"]
	lbType := fieldPresence["local_body_type"]
	lbName := fieldPresence["local_body_name"]
	ward := fieldPresence["ward_number"]
	lbBooth := fieldPresence["lb_booth_number"]

	// ---- AC combo: check only if AC-specific fields exist ----
	hasACCombo := state && ac && booth
	anyACSpecificField := ac || booth
	if anyACSpecificField && !hasACCombo {
		return fmt.Errorf("partial AC combo detected: must provide full State-AC-Booth")
	}

	// ---- AD combo: check only if AD-specific fields exist ----
	hasADCombo := state && ad && lbType && lbName && ward && lbBooth
	anyADSpecificField := ad || lbType || lbName || ward || lbBooth
	if anyADSpecificField && !hasADCombo {
		return fmt.Errorf("partial AD combo detected: must provide full State-AD-LBType-LBName-Ward-LocalBodyBooth")
	}

	// if only state present
	if state && !hasADCombo && !hasACCombo {
		return fmt.Errorf("Required combinations: State-AC-Booth OR State-AD-LBType-LBName-Ward-LocalBodyBooth")
	}

	if fieldPresence["tags"] {
		if len(v.Tags) > 10 {
			return fmt.Errorf("max 10 tags allowed")
		}

		for i, tag := range v.Tags {
			if len(tag) > 20 {
				n := i + 1
				return fmt.Errorf(
					"max 20 characters allowed per tag: %d%s tag has %d characters",
					n,
					ordinalSuffix(n),
					len(tag),
				)
			}
		}
	}

	return nil
}

func ordinalSuffix(n int) string {
	if n%100 >= 11 && n%100 <= 13 {
		return "th"
	}

	switch n % 10 {
	case 1:
		return "st"
	case 2:
		return "nd"
	case 3:
		return "rd"
	default:
		return "th"
	}
}

// VoterTags represents a slice of tags for a voter
type VoterTags []string

// Scan implements the sql.Scanner interface for database reading
func (vt *VoterTags) Scan(value interface{}) error {
	if value == nil {
		*vt = VoterTags{}
		return nil
	}

	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, vt)
	case string:
		return json.Unmarshal([]byte(v), vt)
	default:
		return fmt.Errorf("cannot scan %T into VoterTags", value)
	}
}

// Value implements the driver.Valuer interface for database writing
func (vt VoterTags) Value() (driver.Value, error) {
	if vt == nil {
		return "[]", nil
	}
	return json.Marshal(vt)
}

type Voter struct {
	ID             int64             `json:"id"`            // Elasticsearch: long
	Name           string            `json:"name"`          // Elasticsearch: keyword
	Age            int               `json:"age"`           // Elasticsearch: integer
	Gender         string            `json:"gender"`        // Elasticsearch: keyword
	HouseNumber    string            `json:"house_number"`  // Elasticsearch: keyword
	RelationName   string            `json:"relation_name"` // Elasticsearch: keyword
	RelationType   string            `json:"-"`             // Elasticsearch: keyword
	SerialNumber   any               `json:"serial_number"` // Elasticsearch: integer
	PageNumber     any               `json:"page_number"`   // Elasticsearch: integer
	AcID           int64             `json:"ac_id"`         // Elasticsearch: long
	AcName         string            `json:"ac_name"`       // Elasticsearch: keyword
	AcNumber       int               `json:"ac_number"`     // Elasticsearch: integer
	BoothID        int64             `json:"booth_id"`      // Elasticsearch: long
	BoothName      string            `json:"booth_name"`    // Elasticsearch: keyword
	BoothNumber    any               `json:"booth_number"`  // Elasticsearch: keyword
	EPICNumber     string            `json:"epic_number"`   // Elasticsearch: keyword
	HasVoted       bool              `json:"-"`             // Elasticsearch: boolean
	IsBJPSupporter bool              `json:"-"`             // Elasticsearch: boolean
	Inclination    int               `json:"-"`             // Elasticsearch: keyword
	PrintedCount   int               `json:"-"`             // Elasticsearch: integer
	StateID        int64             `json:"state_id"`      // Elasticsearch: long
	StateName      string            `json:"state_name"`    // Elasticsearch: keyword
	MarkDeleted    bool              `json:"mark_deleted"`  // Elasticsearch: boolean
	Outreached     bool              `json:"outreached"`    // Elasticsearch: long
	Voted          bool              `json:"voted"`
	Tags           VoterTags         `json:"tags,omitempty" gorm:"type:jsonb;default:'[]'"`
	TagInfo        any               `json:"tags_info,omitempty" gorm:"-"`
	Forms          []VoterFormInfo   `json:"forms" gorm:"-"`
	Actions        []VoterActionInfo `json:"actions" gorm:"-"`
}

func (Voter) TableName() string {
	return "voters"
}

// VoterFormInfo represents form information for a voter
type VoterFormInfo struct {
	FormID    int       `json:"form_id"`
	FormType  *FormType `json:"form_type,omitempty"`
	Disabled  bool      `json:"disabled"`
	Submitted bool      `json:"submitted"`
	Display   string    `json:"display"`
	Color     string    `json:"color,omitempty"`
	Bg        string    `json:"bg,omitempty"`
	Size      string    `json:"size,omitempty"`
	Order     int       `json:"order,omitempty"`
	OkColor   string    `json:"ok-color,omitempty"`
	OkBg      string    `json:"ok-bg,omitempty"`
}

// VoterActionInfo represents action information for a voter
type VoterActionInfo struct {
	FormID    int       `json:"form_id"`
	FormType  *FormType `json:"form_type,omitempty"`
	Disabled  bool      `json:"disabled"`
	Submitted bool      `json:"submitted"`
	Display   string    `json:"display"`
	Color     string    `json:"color,omitempty"`
	Bg        string    `json:"bg,omitempty"`
	Size      string    `json:"size,omitempty"`
	Order     int       `json:"order,omitempty"`
	OkColor   string    `json:"ok-color,omitempty"`
	OkBg      string    `json:"ok-bg,omitempty"`
}
type AcData struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Number string `json:"number"`
}

type VotersSpecificBooth struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Number string `json:"number"`
}

type WardData struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Number string `json:"number"`
}

type LBBoothData struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Number string `json:"number"`
}
