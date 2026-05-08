package models

type CampaignInteraction interface {
	GetDefaultList() ([]CampaignInteraction, error)
	GetID() uint64
	List() ([]CampaignInteraction, error)
	Create() error
	Update() error
	Delete() error
	IsSingleInteraction() bool
}
