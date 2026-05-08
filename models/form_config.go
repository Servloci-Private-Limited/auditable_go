package models

// FormConfigItem represents a single form in the configuration
type FormConfigItem struct {
	TypeKey     string `json:"typeKey"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Submission  string `json:"submission"`
	BtnText     string `json:"btnText,omitempty"`
	Order       int    `json:"order"`
	Color       string `json:"color,omitempty"`
	Bg          string `json:"bg,omitempty"`
	Size        string `json:"size,omitempty"`
	OkColor     string `json:"ok-color,omitempty"`
	OkBg        string `json:"ok-bg,omitempty"`
}

// FormGroupConfig represents a group of forms
type FormGroupConfig struct {
	GroupName string           `json:"group_name"`
	Forms     []FormConfigItem `json:"forms"`
}

// FormConfig represents the entire form configuration
type FormConfig struct {
	EventFormGroup FormGroupConfig `json:"event_form_group"`
	VoterSlipGroup FormGroupConfig `json:"voter_slip_group"`
	FilterGroup    FormGroupConfig `json:"filter_group"`
}

// FormGroupItem represents a form in the grouped response (for API)
type FormGroupItem struct {
	GroupName string         `json:"group_name"`
	Forms     []CampaignForm `json:"forms"`
}

// GroupedFormsResponseUser represents grouped forms for user endpoint
type GroupedFormsResponseUser struct {
	EventFormGroup FormGroupItem `json:"event_form_group"`
	VoterSlipGroup FormGroupItem `json:"voter_slip_group"`
	FilterGroup    FormGroupItem `json:"filter_group"`
}
