package mongoaudit

import (
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
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
	ID             primitive.ObjectID `bson:"_id,omitempty"`
	AuditableID    string             `bson:"auditable_id"`
	AuditableType  string             `bson:"auditable_type"`
	UserID         *string            `bson:"user_id,omitempty"`
	Action         Action             `bson:"action"`
	AuditedChanges bson.M             `bson:"audited_changes"`
	ChangesRef     *string            `bson:"changes_ref,omitempty"`
	DiffStatus     DiffStatus         `bson:"diff_status,omitempty"`
	Version        int64              `bson:"version"`
	Comment        *string            `bson:"comment,omitempty"`
	CreatedAt      time.Time          `bson:"created_at"`
}
