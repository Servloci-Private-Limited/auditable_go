// Package mongoaudit provides an auditing wrapper around *mongo.Collection.
// It intercepts InsertOne, InsertMany, UpdateOne, UpdateMany, ReplaceOne,
// DeleteOne, and DeleteMany, capturing a before/after diff and writing an
// Audit document to a dedicated audit collection after every mutation.
//
// Usage:
//
//	auditor := mongoaudit.NewAuditor(db, "audits", mongoaudit.Config{})
//	audited := auditor.Collection("users")
//
//	ctx = mongoaudit.WithUserID(ctx, "user-123")
//	audited.InsertOne(ctx, newUser)
package mongoaudit

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// defaultSkipFields are always omitted from every audit entry.
// Timestamps that change on every write add noise without signal.
var defaultSkipFields = map[string]struct{}{
	"updated_at": {},
	"created_at": {},
}

// Config controls audit behaviour for an AuditableCollection.
type Config struct {
	// RedactedValue replaces values for fields listed in RedactedFields.
	// Defaults to "[REDACTED]".
	RedactedValue string

	// RedactedFields is a set of top-level field names whose values must never
	// appear in the audit trail (e.g. "password", "token").
	RedactedFields map[string]struct{}

	// AuditCollectionName overrides the collection used by NewAuditor when its
	// auditCollectionName argument is empty. Defaults to "audits".
	AuditCollectionName string

	// SkipFields lists top-level field names that should never appear in any
	// audit entry. Merged with the built-in defaults (updated_at, created_at).
	SkipFields map[string]struct{}

	// OnAudit, when non-nil, is called synchronously after each Audit document
	// is written. Use it to forward events to a log or message bus.
	OnAudit func(*Audit)

	// OnError, when non-nil, is called when writing an audit document or looking
	// up audit metadata fails. Mutating MongoDB methods keep their original
	// driver signatures, so audit-only errors are reported here.
	OnError func(error)
}

// Auditor owns the audit collection and configuration for a MongoDB database.
// Use it when multiple collections should write to the same audit collection.
type Auditor struct {
	db       *mongo.Database
	auditCol *mongo.Collection
	config   Config
}

// NewAuditor returns a reusable MongoDB auditor. auditCollectionName defaults
// to "audits" when empty.
func NewAuditor(db *mongo.Database, auditCollectionName string, cfg Config) *Auditor {
	if auditCollectionName == "" {
		auditCollectionName = cfg.AuditCollectionName
	}
	if auditCollectionName == "" {
		auditCollectionName = "audits"
	}
	return &Auditor{
		db:       db,
		auditCol: db.Collection(auditCollectionName),
		config:   normalizeConfig(cfg),
	}
}

// Collection wraps db.Collection(name) with auditing enabled.
func (a *Auditor) Collection(name string) *AuditableCollection {
	return a.Wrap(a.db.Collection(name))
}

// Wrap wraps an existing collection with this auditor's audit collection and
// config. It is useful when the caller already has collection handles.
func (a *Auditor) Wrap(coll *mongo.Collection) *AuditableCollection {
	return &AuditableCollection{coll: coll, auditColl: a.auditCol, config: a.config}
}

// AuditCollection returns the MongoDB collection where audit documents are
// stored.
func (a *Auditor) AuditCollection() *mongo.Collection { return a.auditCol }

// EnsureIndexes creates the recommended indexes on the audit collection.
func (a *Auditor) EnsureIndexes(ctx context.Context) error {
	return EnsureAuditIndexes(ctx, a.auditCol)
}

// AuditableCollection wraps a *mongo.Collection and writes an Audit document
// to the audit collection after every mutating operation.
type AuditableCollection struct {
	coll      *mongo.Collection
	auditColl *mongo.Collection
	config    Config
}

// Wrap returns an AuditableCollection backed by coll. Audit documents are
// written to auditColl.
func Wrap(coll, auditColl *mongo.Collection, cfg Config) *AuditableCollection {
	return &AuditableCollection{coll: coll, auditColl: auditColl, config: normalizeConfig(cfg)}
}

func normalizeConfig(cfg Config) Config {
	if cfg.RedactedValue == "" {
		cfg.RedactedValue = "[REDACTED]"
	}
	if cfg.RedactedFields == nil {
		cfg.RedactedFields = map[string]struct{}{}
	}
	// Merge caller-supplied SkipFields with the built-in defaults.
	merged := make(map[string]struct{}, len(defaultSkipFields)+len(cfg.SkipFields))
	for k := range defaultSkipFields {
		merged[k] = struct{}{}
	}
	for k := range cfg.SkipFields {
		merged[k] = struct{}{}
	}
	cfg.SkipFields = merged
	return cfg
}

// EnsureAuditIndexes creates the recommended indexes for an audit collection.
// It can be used by both direct MongoDB auditing and GORM auditing backed by
// NewMongoStore.
func EnsureAuditIndexes(ctx context.Context, auditColl *mongo.Collection) error {
	_, err := auditColl.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "auditable_type", Value: 1}, {Key: "auditable_id", Value: 1}, {Key: "version", Value: -1}},
			Options: options.Index().SetName("idx_auditable_lookup_version"),
		},
		{
			Keys:    bson.D{{Key: "created_at", Value: 1}},
			Options: options.Index().SetName("idx_audits_created_at"),
		},
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}},
			Options: options.Index().SetName("idx_audits_user_id"),
		},
	})
	return err
}

// Collection returns the underlying unwrapped *mongo.Collection.
func (a *AuditableCollection) Collection() *mongo.Collection { return a.coll }

// -------------------------------------------------------------------
// Mutating operations
// -------------------------------------------------------------------

// InsertOne inserts a single document and records a create audit entry.
func (a *AuditableCollection) InsertOne(ctx context.Context, document interface{}, opts ...options.Lister[options.InsertOneOptions]) (*mongo.InsertOneResult, error) {
	result, err := a.coll.InsertOne(ctx, document, opts...)
	if err != nil {
		return result, err
	}

	docMap, merr := toMap(document)
	if merr == nil {
		id := fmt.Sprint(result.InsertedID)
		changes := createChanges(docMap, a.config.SkipFields, a.config.RedactedFields, a.config.RedactedValue)
		a.persist(ctx, a.buildAudit(ctx, id, ActionCreate, changes))
	} else {
		a.handleError(merr)
	}
	return result, nil
}

// InsertMany inserts multiple documents and records one create audit entry per
// inserted document.
func (a *AuditableCollection) InsertMany(ctx context.Context, documents []interface{}, opts ...options.Lister[options.InsertManyOptions]) (*mongo.InsertManyResult, error) {
	result, err := a.coll.InsertMany(ctx, documents, opts...)
	if err != nil {
		return result, err
	}

	for i, doc := range documents {
		docMap, merr := toMap(doc)
		if merr != nil {
			a.handleError(merr)
			continue
		}
		id := ""
		if i < len(result.InsertedIDs) {
			id = fmt.Sprint(result.InsertedIDs[i])
		}
		changes := createChanges(docMap, a.config.SkipFields, a.config.RedactedFields, a.config.RedactedValue)
		a.persist(ctx, a.buildAudit(ctx, id, ActionCreate, changes))
	}
	return result, nil
}

// UpdateOne updates the first document matching filter and records an update
// audit entry. Changes are extracted directly from the update operators
// (no pre/post read round-trips).
func (a *AuditableCollection) UpdateOne(ctx context.Context, filter interface{}, update interface{}, opts ...options.Lister[options.UpdateOneOptions]) (*mongo.UpdateResult, error) {
	// Resolve the _id of the matched document so the audit entry is tied to a
	// specific record. A single FindOne (projection: {_id:1}) is cheaper than
	// a full before-snapshot.
	var idDoc bson.M
	_ = a.coll.FindOne(ctx, filter,
		options.FindOne().SetProjection(bson.M{"_id": 1}),
	).Decode(&idDoc)

	result, err := a.coll.UpdateOne(ctx, filter, update, opts...)
	if err != nil {
		return result, err
	}
	if result.MatchedCount == 0 {
		return result, nil
	}

	changes := extractUpdateChanges(update, a.config.SkipFields, a.config.RedactedFields, a.config.RedactedValue)
	if len(changes) > 0 {
		id := fmt.Sprint(idDoc["_id"])
		a.persist(ctx, a.buildAudit(ctx, id, ActionUpdate, changes))
	}
	return result, nil
}

// UpdateMany updates all documents matching filter and records one update audit
// entry per affected document. Changes are extracted from the update operators
// directly — no per-document pre/post reads are performed.
func (a *AuditableCollection) UpdateMany(ctx context.Context, filter interface{}, update interface{}, opts ...options.Lister[options.UpdateManyOptions]) (*mongo.UpdateResult, error) {
	// Collect only _ids before the update (minimal projection, no full snapshots).
	cursor, _ := a.coll.Find(ctx, filter,
		options.Find().SetProjection(bson.M{"_id": 1}),
	)
	var ids []interface{}
	if cursor != nil {
		var idDocs []bson.M
		_ = cursor.All(ctx, &idDocs)
		cursor.Close(ctx)
		for _, d := range idDocs {
			ids = append(ids, d["_id"])
		}
	}

	result, err := a.coll.UpdateMany(ctx, filter, update, opts...)
	if err != nil {
		return result, err
	}
	if result.MatchedCount == 0 {
		return result, nil
	}

	changes := extractUpdateChanges(update, a.config.SkipFields, a.config.RedactedFields, a.config.RedactedValue)
	if len(changes) == 0 {
		return result, nil
	}
	for _, rawID := range ids {
		a.persist(ctx, a.buildAudit(ctx, fmt.Sprint(rawID), ActionUpdate, changes))
	}
	return result, nil
}

// ReplaceOne replaces the first document matching filter and records an update
// audit entry with the full before/after diff.
func (a *AuditableCollection) ReplaceOne(ctx context.Context, filter interface{}, replacement interface{}, opts ...options.Lister[options.ReplaceOptions]) (*mongo.UpdateResult, error) {
	oldDoc, _ := a.findOne(ctx, filter)

	result, err := a.coll.ReplaceOne(ctx, filter, replacement, opts...)
	if err != nil {
		return result, err
	}
	if result.MatchedCount == 0 {
		return result, nil
	}

	newDoc, _ := toMap(replacement)
	if id, ok := oldDoc["_id"]; ok && newDoc != nil {
		newDoc["_id"] = id
	}

	changes := diffDocs(oldDoc, newDoc, a.config.SkipFields, a.config.RedactedFields, a.config.RedactedValue)
	if len(changes) > 0 {
		id := fmt.Sprint(oldDoc["_id"])
		a.persist(ctx, a.buildAudit(ctx, id, ActionUpdate, changes))
	}
	return result, nil
}

// DeleteOne deletes the first document matching filter and records a delete
// audit entry with all field values captured before deletion.
func (a *AuditableCollection) DeleteOne(ctx context.Context, filter interface{}, opts ...options.Lister[options.DeleteOneOptions]) (*mongo.DeleteResult, error) {
	oldDoc, _ := a.findOne(ctx, filter)

	result, err := a.coll.DeleteOne(ctx, filter, opts...)
	if err != nil {
		return result, err
	}
	if result.DeletedCount == 0 {
		return result, nil
	}

	if len(oldDoc) > 0 {
		changes := deleteChanges(oldDoc, a.config.SkipFields, a.config.RedactedFields, a.config.RedactedValue)
		id := fmt.Sprint(oldDoc["_id"])
		a.persist(ctx, a.buildAudit(ctx, id, ActionDelete, changes))
	}
	return result, nil
}

// DeleteMany deletes all documents matching filter and records one delete audit
// entry per removed document.
func (a *AuditableCollection) DeleteMany(ctx context.Context, filter interface{}, opts ...options.Lister[options.DeleteManyOptions]) (*mongo.DeleteResult, error) {
	oldDocs, _ := a.findMany(ctx, filter)

	result, err := a.coll.DeleteMany(ctx, filter, opts...)
	if err != nil {
		return result, err
	}
	if result.DeletedCount == 0 {
		return result, nil
	}

	for _, doc := range oldDocs {
		changes := deleteChanges(doc, a.config.SkipFields, a.config.RedactedFields, a.config.RedactedValue)
		id := fmt.Sprint(doc["_id"])
		a.persist(ctx, a.buildAudit(ctx, id, ActionDelete, changes))
	}
	return result, nil
}

// -------------------------------------------------------------------
// Read-only pass-throughs
// -------------------------------------------------------------------

func (a *AuditableCollection) FindOne(ctx context.Context, filter interface{}, opts ...options.Lister[options.FindOneOptions]) *mongo.SingleResult {
	return a.coll.FindOne(ctx, filter, opts...)
}

func (a *AuditableCollection) Find(ctx context.Context, filter interface{}, opts ...options.Lister[options.FindOptions]) (*mongo.Cursor, error) {
	return a.coll.Find(ctx, filter, opts...)
}

func (a *AuditableCollection) CountDocuments(ctx context.Context, filter interface{}, opts ...options.Lister[options.CountOptions]) (int64, error) {
	return a.coll.CountDocuments(ctx, filter, opts...)
}

func (a *AuditableCollection) Aggregate(ctx context.Context, pipeline interface{}, opts ...options.Lister[options.AggregateOptions]) (*mongo.Cursor, error) {
	return a.coll.Aggregate(ctx, pipeline, opts...)
}

// -------------------------------------------------------------------
// Internal helpers
// -------------------------------------------------------------------

func (a *AuditableCollection) persist(ctx context.Context, audit *Audit) {
	_, err := a.auditColl.InsertOne(ctx, audit)
	if err != nil {
		a.handleError(err)
		return
	}
	if a.config.OnAudit != nil {
		a.config.OnAudit(audit)
	}
}

func (a *AuditableCollection) handleError(err error) {
	if err != nil && a.config.OnError != nil {
		a.config.OnError(err)
	}
}

func (a *AuditableCollection) buildAudit(ctx context.Context, id string, action Action, changes bson.M) *Audit {
	uid := resolveUserID(ctx)
	comment := resolveComment(ctx)
	return &Audit{
		AuditableID:    id,
		AuditableType:  a.coll.Name(),
		UserID:         uid,
		Action:         action,
		AuditedChanges: changes,
		Version:        a.nextVersion(ctx, a.coll.Name(), id),
		Comment:        comment,
		CreatedAt:      time.Now(),
	}
}

// nextVersion returns the current max version for the entity plus one.
func (a *AuditableCollection) nextVersion(ctx context.Context, auditableType, auditableID string) int64 {
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
	cursor, err := a.auditColl.Aggregate(ctx, pipeline)
	if err != nil {
		a.handleError(err)
		return 1
	}
	defer cursor.Close(ctx)
	var result struct {
		MaxVer int64 `bson:"maxVer"`
	}
	if cursor.Next(ctx) {
		if err := cursor.Decode(&result); err != nil {
			a.handleError(err)
		}
	}
	return result.MaxVer + 1
}

func (a *AuditableCollection) findOne(ctx context.Context, filter interface{}) (bson.M, error) {
	var doc bson.M
	err := a.coll.FindOne(ctx, filter).Decode(&doc)
	return doc, err
}

func (a *AuditableCollection) findMany(ctx context.Context, filter interface{}) ([]bson.M, error) {
	cursor, err := a.coll.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []bson.M
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// -------------------------------------------------------------------
// Context helpers
// -------------------------------------------------------------------

func resolveUserID(ctx context.Context) *string {
	s, ok := UserIDFromContext(ctx)
	if !ok {
		return nil
	}
	return &s
}

func resolveComment(ctx context.Context) *string {
	c, ok := CommentFromContext(ctx)
	if !ok {
		return nil
	}
	return &c
}

// -------------------------------------------------------------------
// Change detection
// -------------------------------------------------------------------

// diffDocs compares two bson.M snapshots (used for ReplaceOne) and returns a
// changes map of "fieldName": [oldValue, newValue]. Identical fields and
// fields in skip are omitted. "_id" is always excluded.
func diffDocs(old, new bson.M, skip map[string]struct{}, redacted map[string]struct{}, redactedVal string) bson.M {
	changes := bson.M{}

	for k, newVal := range new {
		if k == "_id" {
			continue
		}
		if _, s := skip[k]; s {
			continue
		}
		if _, r := redacted[k]; r {
			changes[k] = bson.A{redactedVal, redactedVal}
			continue
		}
		oldVal, exists := old[k]
		if !exists {
			changes[k] = bson.A{nil, newVal}
		} else if !reflect.DeepEqual(oldVal, newVal) {
			changes[k] = bson.A{oldVal, newVal}
		}
	}

	// Fields present in old but absent in new were removed.
	for k, oldVal := range old {
		if k == "_id" {
			continue
		}
		if _, s := skip[k]; s {
			continue
		}
		if _, exists := new[k]; !exists {
			if _, r := redacted[k]; r {
				changes[k] = bson.A{redactedVal, nil}
			} else {
				changes[k] = bson.A{oldVal, nil}
			}
		}
	}

	return changes
}

// extractUpdateChanges parses a MongoDB update document and returns the set of
// field-level changes being applied without requiring a document read.
//
// Supported operators: $set, $unset, $inc, $mul, $push, $addToSet, $pull.
// For $set the value is stored as-is. For $unset the value is nil.
// For arithmetic/array operators the value is recorded as {"$op": operand}.
func extractUpdateChanges(update interface{}, skip map[string]struct{}, redacted map[string]struct{}, redactedVal string) bson.M {
	updateMap, _ := toMap(update)
	changes := bson.M{}

	record := func(field string, value interface{}) {
		if _, s := skip[field]; s {
			return
		}
		if _, r := redacted[field]; r {
			changes[field] = bson.A{redactedVal, redactedVal}
			return
		}
		changes[field] = bson.A{nil, value}
	}

	for op, raw := range updateMap {
		fields, _ := toMap(raw)
		switch op {
		case "$set":
			for k, v := range fields {
				record(k, v)
			}
		case "$unset":
			for k := range fields {
				record(k, nil)
			}
		default:
			// $inc, $mul, $push, $addToSet, $pull, etc.
			for k, v := range fields {
				record(k, bson.M{op: v})
			}
		}
	}
	return changes
}

// createChanges builds the audited_changes map for an insert.
// Each field is recorded as [nil, value], matching the GORM audit payload.
func createChanges(doc bson.M, skip map[string]struct{}, redacted map[string]struct{}, redactedVal string) bson.M {
	changes := make(bson.M, len(doc))
	for k, v := range doc {
		if k == "_id" {
			continue
		}
		if _, s := skip[k]; s {
			continue
		}
		if _, r := redacted[k]; r {
			changes[k] = bson.A{nil, redactedVal}
		} else {
			changes[k] = bson.A{nil, v}
		}
	}
	return changes
}

// deleteChanges builds the audited_changes map for a delete.
// Each field is recorded as [value, nil] so reviewers can see what was removed.
// Skipped and redacted fields follow the same rules as other operations.
func deleteChanges(doc bson.M, skip map[string]struct{}, redacted map[string]struct{}, redactedVal string) bson.M {
	changes := make(bson.M, len(doc))
	for k, v := range doc {
		if k == "_id" {
			continue
		}
		if _, s := skip[k]; s {
			continue
		}
		if _, r := redacted[k]; r {
			changes[k] = bson.A{redactedVal, nil}
		} else {
			changes[k] = bson.A{v, nil}
		}
	}
	return changes
}

// toMap serialises any document value (struct, map, bson.M, etc.) to bson.M
// by round-tripping through the BSON codec.
func toMap(v interface{}) (bson.M, error) {
	if m, ok := v.(bson.M); ok {
		return m, nil
	}
	raw, err := bson.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m bson.M
	if err := bson.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}
