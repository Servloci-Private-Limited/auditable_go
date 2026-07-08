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
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
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

	// UserIDResolver, when set, is called to extract the acting user from the
	// request context. Equivalent to the GORM plugin's UserIDResolver:
	//
	//   auditor := mongoaudit.NewAuditor(db, "audits", mongoaudit.Config{
	//       UserIDResolver: func(ctx context.Context) (string, bool) {
	//           return mongoaudit.ExtractUserID(ctx, myMiddleware.UserKey)
	//       },
	//   })
	//
	// When nil the auditor falls back to the value stored by WithUserID.
	UserIDResolver        func(ctx context.Context) (string, bool)
	ChangeComputer        ChangeComputer
	DeferredChanges       DeferredChangeHandler
	DiffLimits            DiffLimits
	FailOnAuditError      bool
	VersionCollectionName string
}

// Auditor owns the audit collection and configuration for a MongoDB database.
// Use it when multiple collections should write to the same audit collection.
type Auditor struct {
	db         *mongo.Database
	auditCol   *mongo.Collection
	versionCol *mongo.Collection
	config     Config
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
	versionCollectionName := cfg.VersionCollectionName
	if versionCollectionName == "" {
		versionCollectionName = auditCollectionName + "_versions"
	}
	return &Auditor{
		db:         db,
		auditCol:   db.Collection(auditCollectionName),
		versionCol: db.Collection(versionCollectionName),
		config:     normalizeConfig(cfg),
	}
}

// Collection wraps db.Collection(name) with auditing enabled.
// All operations (create, update, delete) are audited.
func (a *Auditor) Collection(name string) *AuditableCollection {
	return a.Wrap(a.db.Collection(name))
}

// CollectionFor wraps db.Collection(name) and reads the CRUD ops flag from
// the embedded mongoaudit.Model field of example to decide which operations
// generate audit entries. Pass a zero value of your document type:
//
//	type Voter struct {
//	    mongoaudit.Model `bson:",inline" auditable:"create,update"`
//	    ...
//	}
//	voters := auditor.CollectionFor("voters", &Voter{})
//	// voters.DeleteOne will now skip the audit silently.
//
// When the embedded Model has no auditable tag CollectionFor behaves the same
// as Collection (all ops audited).
func (a *Auditor) CollectionFor(name string, example interface{}) *AuditableCollection {
	c := a.Wrap(a.db.Collection(name))
	info := auditTagsFromStruct(example)
	c.ops = info.ops
	c.auditInfo = info
	return c
}

// Wrap wraps an existing collection with this auditor's audit collection and
// config. It is useful when the caller already has collection handles.
func (a *Auditor) Wrap(coll *mongo.Collection) *AuditableCollection {
	return &AuditableCollection{coll: coll, auditColl: a.auditCol, versionColl: a.versionCol, config: a.config}
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
	coll        *mongo.Collection
	auditColl   *mongo.Collection
	config      Config
	ops         map[string]struct{} // nil = all ops; set by CollectionFor
	versionColl *mongo.Collection
	auditInfo   structAuditInfo
}

// allowsOp reports whether this collection should emit an audit entry for the
// given action. Always returns true when ops is nil (the default from Collection).
func (a *AuditableCollection) allowsOp(action Action) bool {
	if a.ops == nil {
		return true
	}
	_, ok := a.ops[string(action)]
	return ok
}

// Wrap returns an AuditableCollection backed by coll. Audit documents are
// written to auditColl.
func Wrap(coll, auditColl *mongo.Collection, cfg Config) *AuditableCollection {
	versionName := cfg.VersionCollectionName
	if versionName == "" {
		versionName = auditColl.Name() + "_versions"
	}
	return &AuditableCollection{coll: coll, auditColl: auditColl, versionColl: auditColl.Database().Collection(versionName), config: normalizeConfig(cfg)}
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
	if cfg.ChangeComputer == nil {
		cfg.ChangeComputer = defaultChangeComputer{}
	}
	cfg.DiffLimits = defaultDiffLimits(cfg.DiffLimits)
	return cfg
}

// -------------------------------------------------------------------
// Struct-tag audit helpers
// -------------------------------------------------------------------

// structAuditInfo holds per-struct field filtering derived from `auditable` tags.
type structAuditInfo struct {
	redacted map[string]struct{}
	skip     map[string]struct{}
	only     map[string]struct{} // nil = include all (no whitelist)
	ops      map[string]struct{} // nil = all ops; non-nil = only these op names
}

// auditInfoCache caches structAuditInfo by reflect.Type so struct tags are
// parsed only once per type rather than on every write operation.
var auditInfoCache sync.Map // map[reflect.Type]structAuditInfo

// auditTagsFromStruct reads `auditable` struct tags and returns sets for
// redacted, skipped, and whitelist-only fields. Mirrors the GORM plugin's
// includeField / schemaOnlyMode logic.
func auditTagsFromStruct(v interface{}) structAuditInfo {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return structAuditInfo{
			redacted: make(map[string]struct{}),
			skip:     make(map[string]struct{}),
		}
	}
	t := rv.Type()
	if cached, ok := auditInfoCache.Load(t); ok {
		return cached.(structAuditInfo)
	}
	info := structAuditInfo{
		redacted: make(map[string]struct{}),
		skip:     make(map[string]struct{}),
	}
	var hasOnly bool
	collectStructTags(t, &info, &hasOnly)
	if !hasOnly {
		info.only = nil
	}
	auditInfoCache.Store(t, info)
	return info
}

func collectStructTags(t reflect.Type, info *structAuditInfo, hasOnly *bool) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		bsonTag := field.Tag.Get("bson")

		// Recurse into inline / anonymous embedded structs.
		// Also read any CRUD ops flag on the embed field itself:
		//   mongoaudit.Model `bson:",inline" auditable:"create,update"`
		if bsonTag == ",inline" || (field.Anonymous && bsonTag == "") {
			if auditTag := field.Tag.Get("auditable"); auditTag != "" {
				parseOpsTag(auditTag, info)
			}
			ft := field.Type
			if ft.Kind() == reflect.Ptr {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				collectStructTags(ft, info, hasOnly)
			}
			continue
		}

		// Determine BSON field name.
		bsonName := strings.ToLower(field.Name)
		if bsonTag != "" {
			if parts := strings.SplitN(bsonTag, ",", 2); parts[0] != "" && parts[0] != "-" {
				bsonName = parts[0]
			}
		}

		switch field.Tag.Get("auditable") {
		case "-", "false":
			// skip — never record this field
			info.skip[bsonName] = struct{}{}
		case "redact":
			info.redacted[bsonName] = struct{}{}
		case "true", "only":
			// whitelist — only tagged fields appear in audit entries
			if info.only == nil {
				info.only = make(map[string]struct{})
			}
			info.only[bsonName] = struct{}{}
			*hasOnly = true
		}
	}
}

// setDocumentTimestamps sets CreatedAt and UpdatedAt on a pointer-to-struct
// document when those fields are zero. Equivalent to GORM's autoCreateTime /
// autoUpdateTime — callers do not need to set timestamps manually.
func setDocumentTimestamps(v interface{}) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr {
		return
	}
	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return
	}
	now := time.Now()
	timeType := reflect.TypeOf(time.Time{})
	for _, name := range []string{"CreatedAt", "UpdatedAt"} {
		f := rv.FieldByName(name)
		if f.IsValid() && f.CanSet() && f.Type() == timeType {
			if f.Interface().(time.Time).IsZero() {
				f.Set(reflect.ValueOf(now))
			}
		}
	}
}

func mergeStringSets(a, b map[string]struct{}) map[string]struct{} {
	if len(b) == 0 {
		return a
	}
	if len(a) == 0 {
		return b
	}
	merged := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		merged[k] = struct{}{}
	}
	for k := range b {
		merged[k] = struct{}{}
	}
	return merged
}

func (a *AuditableCollection) fieldPolicy(extra structAuditInfo) FieldPolicy {
	only := a.auditInfo.only
	if extra.only != nil {
		only = extra.only
	}
	return FieldPolicy{SkipFields: cloneStringSet(mergeStringSets(mergeStringSets(a.config.SkipFields, a.auditInfo.skip), extra.skip)), RedactedFields: cloneStringSet(mergeStringSets(mergeStringSets(a.config.RedactedFields, a.auditInfo.redacted), extra.redacted)), OnlyFields: cloneStringSet(only), RedactedValue: a.config.RedactedValue}
}

func cloneStringSet(source map[string]struct{}) map[string]struct{} {
	if source == nil {
		return nil
	}
	clone := make(map[string]struct{}, len(source))
	for key := range source {
		clone[key] = struct{}{}
	}
	return clone
}

func (a *AuditableCollection) computeChanges(ctx context.Context, id string, mode ChangeMode, before, after bson.M, update interface{}, extra structAuditInfo) (ChangeResult, error) {
	req := ChangeRequest{Mode: mode, Before: before, After: after, Update: update, Policy: a.fieldPolicy(extra), Collection: a.coll.Name(), DocumentID: id}
	limits := a.config.DiffLimits
	if err := requestComplexity(req, limits); err != nil {
		return a.deferChanges(ctx, req, err)
	}
	computeCtx, cancel := context.WithTimeout(ctx, limits.ComputeTimeout)
	defer cancel()
	result, err := a.config.ChangeComputer.Compute(computeCtx, req)
	if err == nil {
		err = computeCtx.Err()
	}
	if err != nil {
		return a.deferChanges(ctx, req, err)
	}
	if len(result.Changes) > limits.MaxChangedFields {
		return a.deferChanges(ctx, req, fmt.Errorf("%w: %d changed fields", ErrChangeTooComplex, len(result.Changes)))
	}
	if result.Status == "" {
		result.Status = DiffInline
	}
	return result, nil
}

func (a *AuditableCollection) deferChanges(ctx context.Context, req ChangeRequest, reason error) (ChangeResult, error) {
	if a.config.DeferredChanges == nil {
		return ChangeResult{}, reason
	}
	var err error
	req, err = sanitizeDeferredRequest(req)
	if err != nil {
		return ChangeResult{}, err
	}
	result, err := a.config.DeferredChanges.Defer(ctx, req, reason)
	if err != nil {
		return ChangeResult{}, err
	}
	if result.Reference == "" {
		return ChangeResult{}, errors.New("mongoaudit: deferred change handler returned an empty reference")
	}
	result.Status = DiffDeferred
	return result, nil
}

// parseOpsTag parses a comma-separated CRUD ops value (e.g. "create,update")
// and populates info.ops. Only "create", "update", and "delete" are recognised;
// unrecognised tokens are silently ignored so that field-level tags on regular
// fields are unaffected.
func parseOpsTag(tag string, info *structAuditInfo) {
	for _, p := range strings.Split(tag, ",") {
		switch strings.TrimSpace(strings.ToLower(p)) {
		case "create", "update", "delete":
			if info.ops == nil {
				info.ops = make(map[string]struct{})
			}
			info.ops[strings.TrimSpace(strings.ToLower(p))] = struct{}{}
		}
	}
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
			Keys:    bson.D{{Key: "auditable_type", Value: 1}, {Key: "auditable_id", Value: 1}, {Key: "version", Value: 1}},
			Options: options.Index().SetName("uniq_auditable_version").SetUnique(true),
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
	setDocumentTimestamps(document)
	result, err := a.coll.InsertOne(ctx, document, opts...)
	if err != nil {
		return result, err
	}
	if !a.allowsOp(ActionCreate) {
		return result, nil
	}

	docMap, merr := toMap(document)
	if merr == nil {
		id := fmt.Sprint(result.InsertedID)
		tags := auditTagsFromStruct(document)
		if aerr := a.writeAudit(ctx, id, ActionCreate, ChangeCreate, nil, docMap, nil, tags); aerr != nil && a.config.FailOnAuditError {
			return result, aerr
		}
	} else {
		a.handleError(merr)
		if a.config.FailOnAuditError {
			return result, merr
		}
	}
	return result, nil
}

// InsertMany inserts multiple documents and records one create audit entry per
// inserted document.
func (a *AuditableCollection) InsertMany(ctx context.Context, documents []interface{}, opts ...options.Lister[options.InsertManyOptions]) (*mongo.InsertManyResult, error) {
	for _, doc := range documents {
		setDocumentTimestamps(doc)
	}
	result, err := a.coll.InsertMany(ctx, documents, opts...)
	if err != nil {
		return result, err
	}
	if !a.allowsOp(ActionCreate) {
		return result, nil
	}

	for i, doc := range documents {
		docMap, merr := toMap(doc)
		if merr != nil {
			a.handleError(merr)
			if a.config.FailOnAuditError {
				return result, merr
			}
			continue
		}
		id := ""
		if i < len(result.InsertedIDs) {
			id = fmt.Sprint(result.InsertedIDs[i])
		}
		tags := auditTagsFromStruct(doc)
		if aerr := a.writeAudit(ctx, id, ActionCreate, ChangeCreate, nil, docMap, nil, tags); aerr != nil && a.config.FailOnAuditError {
			return result, aerr
		}
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
	idErr := a.coll.FindOne(ctx, filter,
		options.FindOne().SetProjection(bson.M{"_id": 1}),
	).Decode(&idDoc)
	if idErr != nil && idErr != mongo.ErrNoDocuments {
		a.handleError(idErr)
		if a.config.FailOnAuditError {
			return nil, idErr
		}
	}

	result, err := a.coll.UpdateOne(ctx, filter, update, opts...)
	if err != nil {
		return result, err
	}
	if result.MatchedCount == 0 || !a.allowsOp(ActionUpdate) {
		return result, nil
	}

	id := fmt.Sprint(idDoc["_id"])
	if aerr := a.writeAudit(ctx, id, ActionUpdate, ChangeOperators, nil, nil, update, structAuditInfo{}); aerr != nil && a.config.FailOnAuditError {
		return result, aerr
	}
	return result, nil
}

// UpdateMany updates all documents matching filter and records one update audit
// entry per affected document. Changes are extracted from the update operators
// directly — no per-document pre/post reads are performed.
func (a *AuditableCollection) UpdateMany(ctx context.Context, filter interface{}, update interface{}, opts ...options.Lister[options.UpdateManyOptions]) (*mongo.UpdateResult, error) {
	// Collect only _ids before the update (minimal projection, no full snapshots).
	cursor, findErr := a.coll.Find(ctx, filter,
		options.Find().SetProjection(bson.M{"_id": 1}),
	)
	if findErr != nil {
		a.handleError(findErr)
		if a.config.FailOnAuditError {
			return nil, findErr
		}
	}
	var ids []interface{}
	if cursor != nil {
		var idDocs []bson.M
		if err := cursor.All(ctx, &idDocs); err != nil {
			a.handleError(err)
			if a.config.FailOnAuditError {
				return nil, err
			}
		}
		cursor.Close(ctx)
		for _, d := range idDocs {
			ids = append(ids, d["_id"])
		}
	}

	result, err := a.coll.UpdateMany(ctx, filter, update, opts...)
	if err != nil {
		return result, err
	}
	if result.MatchedCount == 0 || !a.allowsOp(ActionUpdate) {
		return result, nil
	}

	for _, rawID := range ids {
		if aerr := a.writeAudit(ctx, fmt.Sprint(rawID), ActionUpdate, ChangeOperators, nil, nil, update, structAuditInfo{}); aerr != nil && a.config.FailOnAuditError {
			return result, aerr
		}
	}
	return result, nil
}

// ReplaceOne replaces the first document matching filter and records an update
// audit entry with the full before/after diff.
func (a *AuditableCollection) ReplaceOne(ctx context.Context, filter interface{}, replacement interface{}, opts ...options.Lister[options.ReplaceOptions]) (*mongo.UpdateResult, error) {
	oldDoc, readErr := a.findOne(ctx, filter)
	if readErr != nil && readErr != mongo.ErrNoDocuments {
		a.handleError(readErr)
		if a.config.FailOnAuditError {
			return nil, readErr
		}
	}

	result, err := a.coll.ReplaceOne(ctx, filter, replacement, opts...)
	if err != nil {
		return result, err
	}
	if result.MatchedCount == 0 || !a.allowsOp(ActionUpdate) {
		return result, nil
	}

	newDoc, _ := toMap(replacement)
	if id, ok := oldDoc["_id"]; ok && newDoc != nil {
		newDoc["_id"] = id
	}

	id := fmt.Sprint(oldDoc["_id"])
	if aerr := a.writeAudit(ctx, id, ActionUpdate, ChangeReplace, oldDoc, newDoc, nil, auditTagsFromStruct(replacement)); aerr != nil && a.config.FailOnAuditError {
		return result, aerr
	}
	return result, nil
}

// DeleteOne deletes the first document matching filter and records a delete
// audit entry with all field values captured before deletion.
func (a *AuditableCollection) DeleteOne(ctx context.Context, filter interface{}, opts ...options.Lister[options.DeleteOneOptions]) (*mongo.DeleteResult, error) {
	oldDoc, readErr := a.findOne(ctx, filter)
	if readErr != nil && readErr != mongo.ErrNoDocuments {
		a.handleError(readErr)
		if a.config.FailOnAuditError {
			return nil, readErr
		}
	}

	result, err := a.coll.DeleteOne(ctx, filter, opts...)
	if err != nil {
		return result, err
	}
	if result.DeletedCount == 0 || !a.allowsOp(ActionDelete) {
		return result, nil
	}

	if len(oldDoc) > 0 {
		id := fmt.Sprint(oldDoc["_id"])
		if aerr := a.writeAudit(ctx, id, ActionDelete, ChangeDelete, oldDoc, nil, nil, structAuditInfo{}); aerr != nil && a.config.FailOnAuditError {
			return result, aerr
		}
	}
	return result, nil
}

// DeleteMany deletes all documents matching filter and records one delete audit
// entry per removed document.
func (a *AuditableCollection) DeleteMany(ctx context.Context, filter interface{}, opts ...options.Lister[options.DeleteManyOptions]) (*mongo.DeleteResult, error) {
	oldDocs, readErr := a.findMany(ctx, filter)
	if readErr != nil {
		a.handleError(readErr)
		if a.config.FailOnAuditError {
			return nil, readErr
		}
	}

	result, err := a.coll.DeleteMany(ctx, filter, opts...)
	if err != nil {
		return result, err
	}
	if result.DeletedCount == 0 || !a.allowsOp(ActionDelete) {
		return result, nil
	}

	for _, doc := range oldDocs {
		id := fmt.Sprint(doc["_id"])
		if aerr := a.writeAudit(ctx, id, ActionDelete, ChangeDelete, doc, nil, nil, structAuditInfo{}); aerr != nil && a.config.FailOnAuditError {
			return result, aerr
		}
	}
	return result, nil
}

// SoftDelete sets the deleted_at field on the document matched by filter to the
// current UTC time. The update is audited as an "update" action. It is a
// convenience wrapper around UpdateOne — no actual MongoDB delete is issued, so
// the document remains in the collection and can be restored with Restore.
//
// Filter can be any valid BSON filter, for example:
//
//	col.SoftDelete(ctx, bson.M{"_id": id})
func (a *AuditableCollection) SoftDelete(ctx context.Context, filter interface{}, opts ...options.Lister[options.UpdateOneOptions]) (*mongo.UpdateResult, error) {
	return a.UpdateOne(ctx, filter, bson.M{"$set": bson.M{"deleted_at": time.Now().UTC()}}, opts...)
}

// SoftDeleteMany sets deleted_at on every document matching filter.
// Each affected document gets its own "update" audit entry (same as UpdateMany).
func (a *AuditableCollection) SoftDeleteMany(ctx context.Context, filter interface{}, opts ...options.Lister[options.UpdateManyOptions]) (*mongo.UpdateResult, error) {
	return a.UpdateMany(ctx, filter, bson.M{"$set": bson.M{"deleted_at": time.Now().UTC()}}, opts...)
}

// Restore clears the deleted_at field on the document matched by filter,
// making it visible to normal queries again. The update is audited as an
// "update" action.
func (a *AuditableCollection) Restore(ctx context.Context, filter interface{}, opts ...options.Lister[options.UpdateOneOptions]) (*mongo.UpdateResult, error) {
	return a.UpdateOne(ctx, filter, bson.M{"$unset": bson.M{"deleted_at": ""}}, opts...)
}

// NotDeleted returns a filter that ANDs the given filter with a condition that
// excludes soft-deleted documents (deleted_at must not exist). Use it to keep
// soft-delete logic out of call sites:
//
//	col.Find(ctx, mongoaudit.NotDeleted(bson.M{"status": "active"}))
func NotDeleted(filter interface{}) bson.M {
	condition := bson.M{"deleted_at": bson.M{"$exists": false}}
	if filter == nil {
		return condition
	}
	return bson.M{"$and": bson.A{filter, condition}}
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

func (a *AuditableCollection) writeAudit(ctx context.Context, id string, action Action, mode ChangeMode, before, after bson.M, update interface{}, extra structAuditInfo) error {
	result, err := a.computeChanges(ctx, id, mode, before, after, update, extra)
	if err != nil {
		a.handleError(err)
		return err
	}
	if action == ActionUpdate && len(result.Changes) == 0 && result.Reference == "" {
		return nil
	}
	if result.Changes == nil {
		result.Changes = bson.M{}
	}
	audit, err := a.buildAudit(ctx, id, action, result)
	if err != nil {
		a.handleError(err)
		return err
	}
	if err := a.persist(ctx, audit); err != nil {
		a.handleError(err)
		return err
	}
	return nil
}

func (a *AuditableCollection) persist(ctx context.Context, audit *Audit) error {
	_, err := a.auditColl.InsertOne(ctx, audit)
	if err != nil {
		return err
	}
	if a.config.OnAudit != nil {
		if err := invokeAuditCallback(a.config.OnAudit, audit); err != nil {
			return err
		}
	}
	return nil
}

func invokeAuditCallback(callback func(*Audit), audit *Audit) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("mongoaudit: OnAudit panic: %v", recovered)
		}
	}()
	callback(audit)
	return nil
}

func (a *AuditableCollection) handleError(err error) {
	if err != nil && a.config.OnError != nil {
		func() { defer func() { _ = recover() }(); a.config.OnError(err) }()
	}
}

func (a *AuditableCollection) buildAudit(ctx context.Context, id string, action Action, result ChangeResult) (*Audit, error) {
	uid := a.resolveUserID(ctx)
	comment := resolveComment(ctx)
	version, err := a.nextVersion(ctx, a.coll.Name(), id)
	if err != nil {
		return nil, err
	}
	var ref *string
	if result.Reference != "" {
		ref = &result.Reference
	}
	return &Audit{
		AuditableID:    id,
		AuditableType:  a.coll.Name(),
		UserID:         uid,
		Action:         action,
		AuditedChanges: result.Changes,
		ChangesRef:     ref,
		DiffStatus:     result.Status,
		Version:        version,
		Comment:        comment,
		CreatedAt:      time.Now(),
	}, nil
}

// nextVersion returns the current max version for the entity plus one.
func (a *AuditableCollection) nextVersion(ctx context.Context, auditableType, auditableID string) (int64, error) {
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(auditableType+"\x00"+auditableID)))
	var result struct {
		Version int64 `bson:"version"`
	}
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		err = a.versionColl.FindOneAndUpdate(ctx, bson.M{"_id": key}, bson.M{"$inc": bson.M{"version": 1}}, options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)).Decode(&result)
		if err == nil {
			return result.Version, nil
		}
		if !mongo.IsDuplicateKeyError(err) {
			break
		}
	}
	return 0, err
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

// resolveUserID returns the acting user from the request context.
// It tries UserIDResolver first, then falls back to WithUserID.
func (a *AuditableCollection) resolveUserID(ctx context.Context) *string {
	if a.config.UserIDResolver != nil {
		if s, ok := a.config.UserIDResolver(ctx); ok {
			return &s
		}
		return nil
	}
	return resolveUserID(ctx)
}

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
func diffDocs(old, new bson.M, skip map[string]struct{}, redacted map[string]struct{}, redactedVal string, only map[string]struct{}) bson.M {
	changes := bson.M{}

	for k, newVal := range new {
		if k == "_id" {
			continue
		}
		if _, s := skip[k]; s {
			continue
		}
		if !includedField(k, only, redacted) {
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
		if !includedField(k, only, redacted) {
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
func extractUpdateChanges(update interface{}, skip map[string]struct{}, redacted map[string]struct{}, redactedVal string, only map[string]struct{}) bson.M {
	updateMap, _ := toMap(update)
	changes := bson.M{}

	record := func(field string, value interface{}) {
		if _, s := skip[field]; s {
			return
		}
		if !includedField(field, only, redacted) {
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
// When only is non-nil (whitelist mode), fields absent from it are dropped.
func createChanges(doc bson.M, skip map[string]struct{}, redacted map[string]struct{}, redactedVal string, only map[string]struct{}) bson.M {
	changes := make(bson.M, len(doc))
	for k, v := range doc {
		if k == "_id" {
			continue
		}
		if _, s := skip[k]; s {
			continue
		}
		if !includedField(k, only, redacted) {
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
func deleteChanges(doc bson.M, skip map[string]struct{}, redacted map[string]struct{}, redactedVal string, only map[string]struct{}) bson.M {
	changes := make(bson.M, len(doc))
	for k, v := range doc {
		if k == "_id" {
			continue
		}
		if _, s := skip[k]; s {
			continue
		}
		if !includedField(k, only, redacted) {
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

func includedField(field string, only, redacted map[string]struct{}) bool {
	if only == nil {
		return true
	}
	if _, ok := only[field]; ok {
		return true
	}
	_, protected := redacted[field]
	return protected
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
