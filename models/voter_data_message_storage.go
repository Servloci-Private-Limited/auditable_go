package models

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"
)

type EpicVoter struct {
	ID         int             `json:"id" db:"id"`
	EpicNumber string          `json:"epic_number" db:"epic_number"`
	StateName  string          `json:"state_name" db:"state_name"`
	RawData    json.RawMessage `json:"raw_data" db:"raw_data"`
	CreatedAt  time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at" db:"updated_at"`
}

func (EpicVoter) TableName() string {
	return "epic_voters"
}

type VoterDataMessage struct {
	Data      VoterDataPayload `json:"data"`
	Timestamp time.Time        `json:"timestamp"`
	Source    string           `json:"source"`
}

type EnrichedVoterData struct {
	VoterDataPayload
	Color string `json:"color"`
}

var ColorCodeMappingDefault = map[string]string{}

var ColorCodeMappingBihar = map[string]string{
	"1": "BJP",
	"2": "JDU",
	"3": "LJP",
	"4": "RJD",
	"5": "INC",
	"6": "Other",
	"7": "Undecided",
	"8": "Neutral",
	"0": "Unknown",
}

var ColorCodeMappingWB = map[string]string{
	"1": "Unknown",
	"2": "BJP",
	"3": "TMC",
	"4": "INC",
	"5": "CPIM",
	"6": "AIMIM",
	"7": "Others",
}

var ColorCodeMappingAS = map[string]string{
	"1":  "BJP",
	"2":  "AJP",
	"3":  "UPPL",
	"4":  "BPF",
	"5":  "INC",
	"6":  "AGL",
	"7":  "Undecided",
	"8":  "Neutral",
	"9":  "AIUDF",
	"10": "RD",
	"11": "CPI(M)",
	"12": "Other",
	"0":  "Unknown",
}

// getColorMappingForState returns the color code mapping for a given state name.
func getColorMappingForState(stateName string) map[string]string {
	cleanedStateName := strings.TrimSpace(strings.ToLower(stateName))
	cleanedStateName = strings.ReplaceAll(cleanedStateName, " ", "")

	switch cleanedStateName {
	case "bihar":
		return ColorCodeMappingBihar
	case "westbengal":
		return ColorCodeMappingWB
	case "assam":
		return ColorCodeMappingAS
	default:
		return ColorCodeMappingDefault
	}
}

// GetOrderedPartyNamesForState returns the party/color names for a state sorted by color code.
// Sorting is required because Go maps have random iteration order; without it, CSV column
// headers would shuffle on every run. Order: color codes 1, 2, 3, ... with 0 (Unknown) last.
func GetOrderedPartyNamesForState(stateName string) []string {
	colorMapping := getColorMappingForState(stateName)

	if len(colorMapping) == 0 {
		return nil
	}

	type codeEntry struct {
		code int
		name string
	}
	var entries []codeEntry
	for code, name := range colorMapping {
		c, _ := strconv.Atoi(code)
		entries = append(entries, codeEntry{code: c, name: name})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].code == 0 {
			return false
		}
		if entries[j].code == 0 {
			return true
		}
		return entries[i].code < entries[j].code
	})

	var names []string
	for _, e := range entries {
		names = append(names, e.name)
	}
	return names
}

func GetColorNameForState(colorCode string, stateName string) string {
	colorMapping := getColorMappingForState(stateName)

	if color, exists := colorMapping[colorCode]; exists {
		return color
	}
	return ""
}
