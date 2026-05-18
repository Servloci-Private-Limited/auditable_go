package mongoaudit

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Model is a MongoDB-native base model. Embed it with `bson:",inline"` so
// _id, created_at, updated_at, and deleted_at are promoted to the top-level
// document — the only step needed for full audit tracking, like Mongoid's
// `include Mongoid::History::Trackable`:
//
//	type Voter struct {
//	    mongoaudit.Model `bson:",inline"`
//	    Name        string `bson:"name"`
//	    EpicNumber  string `bson:"epic_number" auditable:"redact"`
//	}
//
// CreatedAt / UpdatedAt are set automatically by the auditor on InsertOne and
// InsertMany when their values are zero — no manual assignment needed.
type Model struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"        json:"id"`
	CreatedAt time.Time          `bson:"created_at"           json:"created_at"`
	UpdatedAt time.Time          `bson:"updated_at"           json:"updated_at"`
	DeletedAt *time.Time         `bson:"deleted_at,omitempty" json:"deleted_at,omitempty"`
}
