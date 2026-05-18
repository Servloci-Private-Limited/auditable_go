package mongoaudit

import (
	"context"

	auditablegorm "github.com/ivikasavnish/auditable_go/v5/postgres-gorm"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// MongoStore implements auditablegorm.AuditStore, persisting GORM audit records
// to a MongoDB collection instead of a SQL table.
//
// This lets you use MongoDB as the single audit backend even when the primary
// data lives in PostgreSQL (or any other GORM-supported database):
//
//	db.Use(auditablegorm.New(auditablegorm.Config{
//	    Store: mongoaudit.NewMongoStore(client.Database("app").Collection("audits")),
//	}))
type MongoStore struct {
	coll *mongo.Collection
}

// NewMongoStore returns a MongoStore that writes to coll.
func NewMongoStore(coll *mongo.Collection) *MongoStore {
	return &MongoStore{coll: coll}
}

// EnsureIndexes creates the recommended indexes on the MongoDB audit
// collection used by this store.
func (s *MongoStore) EnsureIndexes(ctx context.Context) error {
	return EnsureAuditIndexes(ctx, s.coll)
}

// Save serialises the AuditRecord to BSON and inserts it into the MongoDB
// audit collection.
func (s *MongoStore) Save(ctx context.Context, r *auditablegorm.AuditRecord) error {
	doc := bson.M{
		"auditable_id":    r.AuditableID,
		"auditable_type":  r.AuditableType,
		"user_id":         r.UserID,
		"action":          string(r.Action),
		"audited_changes": r.AuditedChanges,
		"version":         r.Version,
		"comment":         r.Comment,
		"created_at":      r.CreatedAt,
	}
	_, err := s.coll.InsertOne(ctx, doc)
	return err
}

// NextVersion returns the next version number for the given entity by querying
// the audit collection.
func (s *MongoStore) NextVersion(ctx context.Context, auditableType, auditableID string) (uint64, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"auditable_type": auditableType,
			"auditable_id":   auditableID,
		}}},
		{{Key: "$group", Value: bson.M{
			"_id":    nil,
			"maxVer": bson.M{"$max": "$version"},
		}}},
	}
	cursor, err := s.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return 1, err
	}
	defer cursor.Close(ctx)
	var result struct {
		MaxVer uint64 `bson:"maxVer"`
	}
	if cursor.Next(ctx) {
		if err := cursor.Decode(&result); err != nil {
			return 1, err
		}
	}
	if err := cursor.Err(); err != nil {
		return 1, err
	}
	return result.MaxVer + 1, nil
}

// compile-time interface check
var _ auditablegorm.AuditStore = (*MongoStore)(nil)
