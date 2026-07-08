package mongoaudit

import (
	"context"
	"crypto/sha256"
	"fmt"

	auditablegorm "github.com/ivikasavnish/auditable_go/v5/postgres-gorm"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
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
	coll        *mongo.Collection
	versionColl *mongo.Collection
}

// NewMongoStore returns a MongoStore that writes to coll.
func NewMongoStore(coll *mongo.Collection) *MongoStore {
	return &MongoStore{coll: coll, versionColl: coll.Database().Collection(coll.Name() + "_versions")}
}

// NewMongoStoreWithVersionCollection uses an explicit atomic sequence
// collection, useful when collection naming is controlled externally.
func NewMongoStoreWithVersionCollection(coll, versionColl *mongo.Collection) *MongoStore {
	return &MongoStore{coll: coll, versionColl: versionColl}
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
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(auditableType+"\x00"+auditableID)))
	var result struct {
		Version uint64 `bson:"version"`
	}
	err := s.versionColl.FindOneAndUpdate(ctx, bson.M{"_id": key}, bson.M{"$inc": bson.M{"version": 1}}, options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)).Decode(&result)
	return result.Version, err
}

// compile-time interface check
var _ auditablegorm.AuditStore = (*MongoStore)(nil)
