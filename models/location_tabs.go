package models

import (
	"regexp"
	"strings"
)

//////////////////////////////////////////////////////////////
// Models
//////////////////////////////////////////////////////////////

type LocationTabName string
type LocationLevel string

const (
	TabVidhansabha          LocationTabName = "Vidhansabha"
	TabMunicipalCorporation LocationTabName = "Municipal Corporation"
	TabMunicipalCouncil     LocationTabName = "Municipal Council"
	TabNagarPanchayat       LocationTabName = "Nagar Panchayat"
	TabGramPanchayat        LocationTabName = "Gram Panchayat"
)

const (
	LevelState                  LocationLevel = "State"
	LevelAdministrativeDistrict LocationLevel = "Administrative District"
	LevelUrbanLocalBody         LocationLevel = "Local Body"
	LevelWard                   LocationLevel = "Ward"
	LevelULBBooth               LocationLevel = "Booth"
	LevelBooth                  LocationLevel = "Booth"
	LevelVidhansabha            LocationLevel = "Vidhansabha"
	LevelBlock                  LocationLevel = "Block"
	LevelGramPanchayat          LocationLevel = "Local Body"
	LevelGramPanchayatWard      LocationLevel = "Ward"
	LevelGramPanchayatBooth     LocationLevel = "Booth"
)

// DataLevelName represents the name values stored in the data_levels table.
type DataLevelName string

const (
	DataLevelNational               DataLevelName = "National"
	DataLevelPradesh                DataLevelName = "Pradesh"
	DataLevelVibhag                 DataLevelName = "Vibhag"
	DataLevelAdministrativeDistrict DataLevelName = "Administrative District"
	DataLevelLokSabha               DataLevelName = "Lok Sabha"
	DataLevelZila                   DataLevelName = "Zila"
	DataLevelVidhanSabha            DataLevelName = "Vidhan Sabha"
	DataLevelMunicipalCorporation   DataLevelName = "Municipal Corporation"
	DataLevelMunicipalCouncil       DataLevelName = "Municipal Council"
	DataLevelNagarPanchayat         DataLevelName = "Nagar Panchayat"
	DataLevelMandal                 DataLevelName = "Mandal"
	DataLevelShaktiKendra           DataLevelName = "Shakti Kendra"
	DataLevelBooth                  DataLevelName = "Booth"
	DataLevelWard                   DataLevelName = "Ward"
	DataLevelTalukaPanchayat        DataLevelName = "Taluka Panchayat"
	DataLevelBlock                  DataLevelName = "Block"
	DataLevelGramPanchayatName      DataLevelName = "Gram Panchayat"
	DataLevelPanna                  DataLevelName = "Panna"
	DataLevelGramPanchayatWard      DataLevelName = "Gram Panchayat Ward"
)

// Allowed data level names per program type, used for permission rule filtering.
var (
	VidhansabhaDataLevelNames      = []string{string(DataLevelPradesh), string(DataLevelLokSabha), string(DataLevelZila), string(DataLevelVidhanSabha), string(DataLevelBooth)}
	MunicipalCorpDataLevelNames    = []string{string(DataLevelPradesh), string(DataLevelAdministrativeDistrict), string(DataLevelMunicipalCorporation), string(DataLevelWard)}
	MunicipalCouncilDataLevelNames = []string{string(DataLevelPradesh), string(DataLevelAdministrativeDistrict), string(DataLevelMunicipalCouncil), string(DataLevelWard)}
	NagarPanchayatDataLevelNames   = []string{string(DataLevelPradesh), string(DataLevelAdministrativeDistrict), string(DataLevelNagarPanchayat), string(DataLevelWard)}
	GramPanchayatDataLevelNames    = []string{string(DataLevelPradesh), string(DataLevelAdministrativeDistrict), string(DataLevelBlock), string(DataLevelGramPanchayatName), string(DataLevelGramPanchayatWard)}
)

type LocationTab struct {
	Name LocationTabName `json:"name"`
	Key  string          `json:"key"`
	Path []LocationLevel `json:"path"`
}

type LocationTabConfig struct {
	Tabs []LocationTab `json:"tabs"`
}

type LocationTabResponseItem struct {
	Name LocationTabName `json:"name"`
	Path []LocationLevel `json:"path"`
}

type LocationTabResponse struct {
	Tabs []LocationTabResponseItem `json:"tabs"`
}

//////////////////////////////////////////////////////////////
// Auto Casing Utilities
//////////////////////////////////////////////////////////////

var nonAlnum = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// AutoKey converts strings into safe map/index keys
// "Municipal Corporation" -> "municipal_corporation"
func AutoKey(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = nonAlnum.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	return s
}

// NormalizePaths auto-case all hierarchy names
func NormalizePaths(paths []LocationLevel) []LocationLevel {
	out := make([]LocationLevel, len(paths))
	for i, p := range paths {
		out[i] = LocationLevel(AutoKey(string(p)))
	}
	return out
}

// GenerateKeys fills missing keys + normalize paths
func (c *LocationTabConfig) GenerateKeys() {
	for i := range c.Tabs {
		if c.Tabs[i].Key == "" {
			c.Tabs[i].Key = AutoKey(string(c.Tabs[i].Name))
		}
		c.Tabs[i].Path = NormalizePaths(c.Tabs[i].Path)
	}
}

//////////////////////////////////////////////////////////////
// Default Config
//////////////////////////////////////////////////////////////

var ULBPath = []LocationLevel{
	LevelState,
	LevelAdministrativeDistrict,
	LevelUrbanLocalBody,
	LevelWard,
	LevelULBBooth,
}

// Gram Panchayat has a different path structure
var GPPath = []LocationLevel{
	LevelState,
	LevelAdministrativeDistrict,
	LevelBlock,
	LevelGramPanchayat,
	LevelGramPanchayatWard,
	LevelGramPanchayatBooth,
}

var DefaultTabConfig = LocationTabConfig{
	Tabs: []LocationTab{
		{Name: TabVidhansabha, Path: []LocationLevel{LevelState, LevelVidhansabha, LevelBooth}},
		{Name: TabMunicipalCorporation, Path: ULBPath},
		{Name: TabMunicipalCouncil, Path: ULBPath},
		{Name: TabNagarPanchayat, Path: ULBPath},
		{Name: TabGramPanchayat, Path: GPPath},
	},
}

func StaticLocationTabConfig() LocationTabConfig {
	cfg := DefaultTabConfig
	cfg.GenerateKeys()
	return cfg
}

func StaticLocationTabResponse() LocationTabResponse {
	cfg := DefaultTabConfig
	items := make([]LocationTabResponseItem, 0, len(cfg.Tabs))
	for _, tab := range cfg.Tabs {
		items = append(items, LocationTabResponseItem{
			Name: tab.Name,
			Path: tab.Path,
		})
	}
	return LocationTabResponse{Tabs: items}
}
