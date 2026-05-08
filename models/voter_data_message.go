package models

type VoterDataPayload struct {
	StateName   string `json:"stateName"`
	AcNo        int    `json:"acNo" validate:"required"`
	PartNo      int    `json:"partNo" validate:"required"`
	Slnoinpart  int    `json:"slnoinpart" validate:"required"`
	EpicNo      string `json:"epicNo" validate:"required"`
	MobileNo    string `json:"mobileNo" validate:"required"`
	WhatsappNo  string `json:"whatsappNo" validate:"-"`
	ColorCode   string `json:"colorCode" validate:"required"`
	IsDead      *bool  `json:"isDead" validate:"-"`
	IsNri       *bool  `json:"isNri" validate:"-"`
	HasShifted  *bool  `json:"hasShifted" validate:"-"`
	IsDuplicate *bool  `json:"isDuplicate" validate:"-"`
}

var StateMapping = map[string]string{
	"AP":  "Andhra Pradesh",
	"AR":  "Arunachal Pradesh",
	"AS":  "Assam",
	"BR":  "Bihar",
	"CG":  "Chhattisgarh",
	"GA":  "Goa",
	"GJ":  "Gujarat",
	"HR":  "Haryana",
	"HP":  "Himachal Pradesh",
	"JK":  "Jammu and Kashmir",
	"JH":  "Jharkhand",
	"KA":  "Karnataka",
	"KL":  "Kerala",
	"MP":  "Madhya Pradesh",
	"MH":  "Maharashtra",
	"MN":  "Manipur",
	"ML":  "Meghalaya",
	"MZ":  "Mizoram",
	"NL":  "Nagaland",
	"OD":  "Odisha",
	"PB":  "Punjab",
	"RJ":  "Rajasthan",
	"SK":  "Sikkim",
	"TN":  "Tamil Nadu",
	"TS":  "Telangana",
	"TR":  "Tripura",
	"UK":  "Uttarakhand",
	"UP":  "Uttar Pradesh",
	"WB":  "West Bengal",
	"DL":  "Delhi",
	"PU":  "Puducherry",
	"DNH": "Dadra Nagar Haveli & Daman-Diu",
	"ANI": "Andaman and Nicobar Islands",
	"CH":  "Chandigarh",
	"DD":  "Daman and Diu",
	"LD":  "Lakshadweep",
	"MU":  "Mumbai",
	"LH":  "Ladakh",
	"EN":  "National",
}
