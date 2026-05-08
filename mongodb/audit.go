package mongoaudit

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Action represents the type of mutation that was audited.
type Action string

const (
	ActionCreate Action = "create"
	ActionUpdate Action = "update"
	ActionDelete Action = "delete"
)

// Audit is the document written to the audit collection for every mutation.
// AuditedChanges stores a map of field → [oldValue, newValue].
// For create operations oldValue is nil; for delete operations newValue is nil.
type Audit struct {
	ID             bson.ObjectID `bson:"_id,omitempty"`
	AuditableID    string        `bson:"auditable_id"`
	AuditableType  string        `bson:"auditable_type"`
	UserID         *string       `bson:"user_id,omitempty"`
	Action         Action        `bson:"action"`
	AuditedChanges bson.M        `bson:"audited_changes"`
	Version        int64         `bson:"version"`
	Comment        *string       `bson:"comment,omitempty"`
	CreatedAt      time.Time     `bson:"created_at"`
}
