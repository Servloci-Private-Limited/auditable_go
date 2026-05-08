package models

import (
	"testing"
)

func TestElection_Validate(t *testing.T) {
	tests := []struct {
		name     string
		election Program
		wantErr  bool
	}{
		{
			name: "Valid election",
			election: Program{
				Name:           "State Assembly Program",
				ProgramType:   "Assembly",
				StateID:        intPtr(1),
				StateName:      "Maharashtra",
				ProgramNumber: "1234",
				Year:           2025,
				ProgramBanner: strPtr("banner.jpg"),
			},
			wantErr: false,
		},
		{
			name: "Invalid name - starts with space",
			election: Program{
				Name:           " State Assembly Program",
				ProgramType:   "Assembly",
				StateID:        intPtr(1),
				StateName:      "Maharashtra",
				ProgramNumber: "1234",
				Year:           2025,
				ProgramBanner: strPtr("banner.jpg"),
			},
			wantErr: true,
		},
		{
			name: "Invalid name - special character",
			election: Program{
				Name:           "State Assembly Program!",
				ProgramType:   "Assembly",
				StateID:        intPtr(1),
				StateName:      "Maharashtra",
				ProgramNumber: "1234",
				Year:           2025,
				ProgramBanner: strPtr("banner.jpg"),
			},
			wantErr: true,
		},
		{
			name: "Invalid name - too long",
			election: Program{
				Name:           "This name is way too long and exceeds the maximum character limit of one hundred characters so it should fail validation testing",
				ProgramType:   "Assembly",
				StateID:        intPtr(1),
				StateName:      "Maharashtra",
				ProgramNumber: "1234",
				Year:           2025,
				ProgramBanner: strPtr("banner.jpg"),
			},
			wantErr: true,
		},
		{
			name: "Missing election type",
			election: Program{
				Name:           "State Assembly Program",
				ProgramType:   "",
				StateID:        intPtr(1),
				StateName:      "Maharashtra",
				ProgramNumber: "1234",
				Year:           2025,
				ProgramBanner: strPtr("banner.jpg"),
			},
			wantErr: true,
		},
		{
			name: "Missing state ID",
			election: Program{
				Name:           "State Assembly Program",
				ProgramType:   "Assembly",
				StateID:        nil,
				StateName:      "Maharashtra",
				ProgramNumber: "1234",
				Year:           2025,
				ProgramBanner: strPtr("banner.jpg"),
			},
			wantErr: true,
		},
		{
			name: "Invalid election number - starts with space",
			election: Program{
				Name:           "State Assembly Program",
				ProgramType:   "Assembly",
				StateID:        intPtr(1),
				StateName:      "Maharashtra",
				ProgramNumber: " 1234",
				Year:           2025,
				ProgramBanner: strPtr("banner.jpg"),
			},
			wantErr: true,
		},
		{
			name: "Invalid election number - non-numeric",
			election: Program{
				Name:           "State Assembly Program",
				ProgramType:   "Assembly",
				StateID:        intPtr(1),
				StateName:      "Maharashtra",
				ProgramNumber: "123A",
				Year:           2025,
				ProgramBanner: strPtr("banner.jpg"),
			},
			wantErr: true,
		},
		{
			name: "Invalid election number - negative",
			election: Program{
				Name:           "State Assembly Program",
				ProgramType:   "Assembly",
				StateID:        intPtr(1),
				StateName:      "Maharashtra",
				ProgramNumber: "-123",
				Year:           2025,
				ProgramBanner: strPtr("banner.jpg"),
			},
			wantErr: true,
		},
		{
			name: "Invalid election number - too large",
			election: Program{
				Name:           "State Assembly Program",
				ProgramType:   "Assembly",
				StateID:        intPtr(1),
				StateName:      "Maharashtra",
				ProgramNumber: "12345",
				Year:           2025,
				ProgramBanner: strPtr("banner.jpg"),
			},
			wantErr: true,
		},
		{
			name: "Invalid year - too early",
			election: Program{
				Name:           "State Assembly Program",
				ProgramType:   "Assembly",
				StateID:        intPtr(1),
				StateName:      "Maharashtra",
				ProgramNumber: "1234",
				Year:           2024,
				ProgramBanner: strPtr("banner.jpg"),
			},
			wantErr: true,
		},
		{
			name: "Invalid year - too late",
			election: Program{
				Name:           "State Assembly Program",
				ProgramType:   "Assembly",
				StateID:        intPtr(1),
				StateName:      "Maharashtra",
				ProgramNumber: "1234",
				Year:           2031,
				ProgramBanner: strPtr("banner.jpg"),
			},
			wantErr: true,
		},
		{
			name: "Missing banner",
			election: Program{
				Name:           "State Assembly Program",
				ProgramType:   "Assembly",
				StateID:        intPtr(1),
				StateName:      "Maharashtra",
				ProgramNumber: "1234",
				Year:           2025,
				ProgramBanner: nil,
			},
			wantErr: true,
		},
		{
			name: "Empty banner",
			election: Program{
				Name:           "State Assembly Program",
				ProgramType:   "Assembly",
				StateID:        intPtr(1),
				StateName:      "Maharashtra",
				ProgramNumber: "1234",
				Year:           2025,
				ProgramBanner: strPtr(""),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.election.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Program.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// Helper functions for creating pointers
func intPtr(i int) *int {
	return &i
}

func strPtr(s string) *string {
	return &s
}
