package models

type FormRedirectRequest struct {
	CampaignID     int    `json:"campaign_id" binding:"required"`
	CampaignFormID int    `json:"campaign_form_id" binding:"required"`
	EntityType     string `json:"entity_type" binding:"required"`
	EntityID       string `json:"entity_id"`
}
