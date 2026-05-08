package models

import (
	"time"

	"gorm.io/gorm"

	"gorm.io/datatypes"
)

type Location struct {
	ID            int64          `gorm:"primaryKey;autoIncrement" json:"id"`
	Name          string         `gorm:"type:varchar;default:null" json:"name"`
	IsDeleted     bool           `gorm:"not null;default:false" json:"-"`
	CreatedAt     time.Time      `gorm:"not null" json:"-"`
	UpdatedAt     time.Time      `gorm:"not null" json:"-"`
	StateCode     string         `gorm:"type:varchar;default:null" json:"state_code"`
	Latitude      float64        `gorm:"type:double precision;default:null" json:"-"`
	Longitude     float64        `gorm:"type:double precision;default:null" json:"-"`
	Zoom          float64        `gorm:"type:double precision;default:null" json:"-"`
	SvgPath       string         `gorm:"type:varchar;default:null" json:"-"`
	Capital       string         `gorm:"type:varchar;default:null" json:"-"`
	OfficeAddress string         `gorm:"type:varchar;default:null" json:"-"`
	Preferences   datatypes.JSON `gorm:"type:jsonb;default:'{}'" json:"-"`
	DeletedAt     *time.Time     `gorm:"type:timestamp;default:null" json:"-"`
	OuterSvgPath  string         `gorm:"type:varchar;default:null" json:"-"`
}

func (Location) TableName() string {
	return "country_states"
}

type Vidhansabha struct {
	ID                    int64          `gorm:"primaryKey;autoIncrement" json:"id"`
	Name                  string         `gorm:"type:varchar;default:null" json:"name"`
	SaralLocatableStateID int64          `gorm:"column:country_state_id;not null" json:"state_id"`
	VidhansabhaNumber     string         `gorm:"column:number;type:int;default:null" json:"vidhansabha_number"`
	IsDeleted             bool           `gorm:"not null;default:false" json:"-"`
	CreatedAt             time.Time      `gorm:"not null" json:"-"`
	UpdatedAt             time.Time      `gorm:"not null" json:"-"`
	DeletedAt             gorm.DeletedAt `gorm:"type:timestamp;default:null" json:"-"`
}

func (Vidhansabha) TableName() string {
	return "assembly_constituencies"
}

type Booth struct {
	ID                                   int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	Name                                 string     `gorm:"type:varchar;default:null" json:"name"`
	BoothNumber                          string     `gorm:"column:number;type:varchar;default:null" json:"booth_number"`
	IsDeleted                            bool       `gorm:"not null;default:false" json:"-"`
	SaralLocatableStateID                int64      `gorm:"default:null" json:"state_id"`
	CreatedAt                            time.Time  `gorm:"not null" json:"-"`
	UpdatedAt                            time.Time  `gorm:"not null" json:"-"`
	SaralLocatableAssemblyConstituencyID int64      `gorm:"default:null" json:"ac_id"`
	VillageName                          string     `gorm:"type:varchar;default:null" json:"village_name"`
	IsUpdating                           bool       `gorm:"not null;default:false" json:"-"`
	AreaUnderBooth                       string     `gorm:"type:varchar;default:null" json:"-"`
	Panchayat                            string     `gorm:"type:varchar;default:null" json:"-"`
	WardNumber                           string     `gorm:"type:varchar;default:null" json:"-"`
	PatwarCircle                         string     `gorm:"type:varchar;default:null" json:"-"`
	KanungoCircle                        string     `gorm:"type:varchar;default:null" json:"-"`
	HadbastSankhya                       string     `gorm:"type:varchar;default:null" json:"-"`
	PinCode                              string     `gorm:"type:varchar;default:null" json:"pin_code"`
	PostOffice                           string     `gorm:"type:varchar;default:null" json:"-"`
	PoliceStation                        string     `gorm:"type:varchar;default:null" json:"-"`
	Block                                string     `gorm:"type:varchar;default:null" json:"-"`
	RevenueDivision                      string     `gorm:"type:varchar;default:null" json:"-"`
	Tehsil                               string     `gorm:"type:varchar;default:null" json:"-"`
	SubDivision                          string     `gorm:"type:varchar;default:null" json:"-"`
	District                             string     `gorm:"type:varchar;default:null" json:"district"`
	DeletedAt                            *time.Time `gorm:"type:timestamp;default:null" json:"-"`
}

func (Booth) TableName() string {
	return "booths"
}

type AdministrativeDistrict struct {
	ID                    int64          `gorm:"primaryKey;autoIncrement" json:"id"`
	Name                  string         `gorm:"type:varchar;default:null" json:"name"`
	SaralLocatableStateID int64          `gorm:"column:country_state_id;not null" json:"state_id"`
	CreatedAt             time.Time      `gorm:"not null" json:"-"`
	UpdatedAt             time.Time      `gorm:"not null" json:"-"`
	DeletedAt             gorm.DeletedAt `gorm:"type:timestamp;default:null" json:"-"`
	// is_deleted, svg_path, total_estimate, metadata other key is db
}

func (AdministrativeDistrict) TableName() string {
	return "administrative_districts"
}

type Block struct {
	ID                    int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Name                  string    `gorm:"type:varchar;default:null" json:"name"`
	SaralLocatableStateID int64     `gorm:"column:country_state_id;not null" json:"state_id"`
	ADID                  int64     `gorm:"column:administrative_district_id;not null" json:"administrative_district_id"`
	CreatedAt             time.Time `gorm:"not null" json:"-"`
	UpdatedAt             time.Time `gorm:"not null" json:"-"`
	// translated_name
}

func (Block) TableName() string {
	return "blocks"
}

type LocalBody struct {
	ID                    int64          `gorm:"primaryKey;autoIncrement" json:"id"`
	Name                  string         `gorm:"type:varchar;default:null" json:"name"`
	SaralLocatableStateID int64          `gorm:"column:country_state_id;not null" json:"state_id"`
	LocalBodyType         string         `gorm:"-" json:"type"`
	CreatedAt             time.Time      `gorm:"not null" json:"-"`
	UpdatedAt             time.Time      `gorm:"not null" json:"-"`
	DeletedAt             gorm.DeletedAt `gorm:"type:timestamp;default:null" json:"-"`
	// is_deleted, allowed_ward_members, translated_name, mappings, mapping_updated_at other key is db
}

type Ward struct {
	ID                    int64          `gorm:"primaryKey;autoIncrement" json:"id"`
	Number                int64          `gorm:"column:number;not null" json:"number"`
	Name                  string         `gorm:"type:varchar;default:null" json:"name"`
	SaralLocatableStateID int64          `gorm:"column:country_state_id;not null" json:"state_id"`
	CreatedAt             time.Time      `gorm:"not null" json:"-"`
	UpdatedAt             time.Time      `gorm:"not null" json:"-"`
	DeletedAt             gorm.DeletedAt `gorm:"type:timestamp;default:null" json:"-"`
	// translated_name, mappings, mapping_updated_at other key is db
}

func (Ward) TableName() string {
	return "wards"
}

type LocalBodyBooth struct {
	ID                    int64          `gorm:"primaryKey;autoIncrement" json:"id"`
	Number                int64          `gorm:"column:number;not null" json:"number"`
	Name                  string         `gorm:"type:varchar;default:null" json:"name"`
	SaralLocatableStateID int64          `gorm:"column:country_state_id;not null" json:"state_id"`
	CreatedAt             time.Time      `gorm:"not null" json:"-"`
	UpdatedAt             time.Time      `gorm:"not null" json:"-"`
	DeletedAt             gorm.DeletedAt `gorm:"type:timestamp;default:null" json:"-"`
	// administrative_district_id, regional_name, local_body_type, local_body_id, ward_type, ward_id, pin_code other key is db
}

func (LocalBodyBooth) TableName() string {
	return "local_body_booths"
}

func LocalBodyTypeMappings(localBodyType string) string {
	switch localBodyType {
	case "municipal_corporation", "MunicipalCorporation":
		return "MunicipalCorporation"
	case "municipal_council", "MunicipalCouncil":
		return "MunicipalCouncil"
	case "nagar_panchayat", "NagarPanchayat":
		return "NagarPanchayat"
	case "gram_panchayat", "GramPanchayat":
		return "GramPanchayat"
	}
	return localBodyType
}
