package auditablegorm

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Config controls the audit plugin behaviour.
type Config struct {
	// RedactedValue is stored for fields tagged `auditable:"redact"`.
	// Defaults to "[REDACTED]".
	RedactedValue string

	// SkipTables lists additional table names that should never be audited.
	// The audit table itself is always skipped automatically.
	SkipTables []string

	// TableName overrides the audit table name. Defaults to "audits".
	TableName string

	// OnAudit, when set, is called synchronously after each audit record is
	// persisted. Use it to stream, log, or forward audit events without
	// registering a separate GORM callback.
	//
	//   db.Use(auditablegorm.New(auditablegorm.Config{
	//       OnAudit: func(a *auditablegorm.Audit) {
	//           log.Printf("[audit] %s %s#%s", a.Action, a.AuditableType, a.AuditableID)
	//       },
	//   }))
	OnAudit func(*Audit)

	// OnError, when set, is called when the configured audit store fails to save
	// an audit record or calculate the next version. The original data mutation
	// has already completed when these callbacks run.
	OnError func(error)

	// UserIDResolver, when set, is called to extract the acting user's ID from
	// the request context. Use this to read whatever key your auth middleware
	// already sets — then db.WithContext(ctx) is the only call you need:
	//
	//   db.Use(auditablegorm.New(auditablegorm.Config{
	//       UserIDResolver: func(ctx context.Context) (string, bool) {
	//           id, ok := ctx.Value(myMiddleware.UserKey).(string)
	//           return id, ok && id != ""
	//       },
	//   }))
	//
	// When nil the plugin falls back to the value stored by WithUserID.
	UserIDResolver func(ctx context.Context) (string, bool)

	// Store is the persistence backend for audit records.
	// When nil the plugin creates a GormStore that writes to the SQL database.
	// Inject a custom implementation to route audit records elsewhere:
	//
	//   db.Use(auditablegorm.New(auditablegorm.Config{
	//       Store: mongoaudit.NewMongoStore(auditCol),
	//   }))
	Store AuditStore
}

type plugin struct {
	config     Config
	auditTable string
	skipSet    map[string]struct{}
	store      AuditStore
}

// New returns a GORM plugin that writes an audit row after every create,
// update, and delete. Register it once at DB initialisation time:
//
//	db.Use(auditablegorm.New(auditablegorm.Config{}))
func New(cfg Config) gorm.Plugin {
	if cfg.RedactedValue == "" {
		cfg.RedactedValue = "[REDACTED]"
	}
	tableName := cfg.TableName
	if tableName == "" {
		tableName = "audits"
	}
	skip := map[string]struct{}{tableName: {}}
	for _, t := range cfg.SkipTables {
		skip[t] = struct{}{}
	}
	return &plugin{config: cfg, auditTable: tableName, skipSet: skip}
}

func (p *plugin) Name() string { return "auditablegorm" }

func (p *plugin) Initialize(db *gorm.DB) error {
	if p.config.Store != nil {
		p.store = p.config.Store
	} else {
		p.store = NewGormStore(db, p.auditTable)
	}
	_ = db.Callback().Create().After("gorm:create").Register("auditablegorm:create", p.afterCreate)
	_ = db.Callback().Update().After("gorm:update").Register("auditablegorm:update", p.afterUpdate)
	_ = db.Callback().Delete().After("gorm:delete").Register("auditablegorm:delete", p.afterDelete)
	return nil
}

func (p *plugin) afterCreate(db *gorm.DB) { p.audit(db, ActionCreate) }
func (p *plugin) afterUpdate(db *gorm.DB) { p.audit(db, ActionUpdate) }
func (p *plugin) afterDelete(db *gorm.DB) { p.audit(db, ActionDelete) }

// shouldSkip returns true when the statement targets the audit table itself or
// a caller-specified table that must be excluded.
func (p *plugin) shouldSkip(db *gorm.DB) bool {
	if db.Statement == nil || db.Statement.Schema == nil {
		return true
	}
	_, skip := p.skipSet[db.Statement.Schema.Table]
	return skip
}

// schemaOnlyMode returns true when at least one field on the schema is tagged
// `auditable:"only"`. When true, only those fields appear in the audit entry.
func schemaOnlyMode(s *gorm.Statement) bool {
	if s.Schema == nil {
		return false
	}
	for _, f := range s.Schema.Fields {
		if f.Tag.Get("auditable") == "only" {
			return true
		}
	}
	return false
}

// includeField decides whether a field belongs in the audit entry.
//
//	`auditable:"only"`   — include (whitelist mode; all untagged fields are dropped)
//	`auditable:"redact"` — include but obscure the value
//	`auditable:"false"`  — always skip
//	(no tag)             — include unless onlyMode is active
func includeField(tag string, onlyMode bool) bool {
	if tag == "false" {
		return false
	}
	if onlyMode && tag != "only" && tag != "redact" {
		return false
	}
	return true
}

func (p *plugin) resolveUserID(db *gorm.DB) *string {
	ctx := db.Statement.Context
	if p.config.UserIDResolver != nil {
		if s, ok := p.config.UserIDResolver(ctx); ok {
			return &s
		}
		return nil
	}
	s, ok := UserIDFromContext(ctx)
	if !ok {
		return nil
	}
	return &s
}

func (p *plugin) resolveComment(db *gorm.DB) *string {
	c, ok := CommentFromContext(db.Statement.Context)
	if !ok {
		return nil
	}
	return &c
}

// audit is the central dispatch: it routes to the right audit strategy based
// on the operation type and the shape of db.Statement.Dest.
func (p *plugin) audit(db *gorm.DB, action Action) {
	if p.shouldSkip(db) {
		return
	}

	uid := p.resolveUserID(db)
	comment := p.resolveComment(db)

	// GORM always sets ReflectValue to the model struct regardless of what
	// Dest is, so we inspect Dest directly to distinguish the two update paths.
	if action == ActionUpdate {
		if _, ok := db.Statement.Dest.(map[string]interface{}); ok {
			p.auditFromMap(db, uid, comment)
		} else {
			p.auditFromChangedCols(db, uid, comment)
		}
		return
	}

	rv := db.Statement.ReflectValue
	if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
		for i := 0; i < rv.Len(); i++ {
			p.auditSingle(db, rv.Index(i), action, uid, comment)
		}
	} else if rv.Kind() == reflect.Struct {
		p.auditSingle(db, rv, action, uid, comment)
	}
}

// persist converts the Audit to an AuditRecord, delegates to the store, and
// fires OnAudit if configured.
func (p *plugin) persist(db *gorm.DB, a *Audit) {
	ctx := context.Background()
	if db.Statement != nil && db.Statement.Context != nil {
		ctx = db.Statement.Context
	}
	// Inject the callback db so GormStore can join the active transaction.
	ctx = withGormTx(ctx, db)
	r := &AuditRecord{
		AuditableID:    a.AuditableID,
		AuditableType:  a.AuditableType,
		UserID:         a.UserID,
		Action:         a.Action,
		AuditedChanges: map[string]interface{}(a.AuditedChanges),
		Version:        a.Version,
		Comment:        a.Comment,
		CreatedAt:      a.CreatedAt,
	}
	if err := p.store.Save(ctx, r); err != nil {
		p.handleError(err)
		return
	}
	if p.config.OnAudit != nil {
		p.config.OnAudit(a)
	}
}

func (p *plugin) handleError(err error) {
	if err != nil && p.config.OnError != nil {
		p.config.OnError(err)
	}
}

// nextVersion delegates version calculation to the configured store.
func (p *plugin) nextVersion(db *gorm.DB, auditableType, auditableID string) uint64 {
	ctx := context.Background()
	if db.Statement != nil && db.Statement.Context != nil {
		ctx = db.Statement.Context
	}
	// Inject the callback db so GormStore can join the active transaction.
	ctx = withGormTx(ctx, db)
	v, err := p.store.NextVersion(ctx, auditableType, auditableID)
	if err != nil {
		p.handleError(err)
		return 1
	}
	return v
}

// pkStr returns the string representation of the model's first primary key.
func pkStr(db *gorm.DB, rv reflect.Value) string {
	for _, f := range db.Statement.Schema.PrimaryFields {
		val, isZero := f.ValueOf(db.Statement.Context, rv)
		if !isZero {
			return fmt.Sprintf("%v", val)
		}
	}
	return ""
}

// auditFromMap handles db.Model(&m).Updates(map[string]interface{}{...}).
func (p *plugin) auditFromMap(db *gorm.DB, userID, comment *string) {
	schema := db.Statement.Schema

	modelVal := reflect.ValueOf(db.Statement.Model)
	if modelVal.Kind() == reflect.Ptr {
		modelVal = modelVal.Elem()
	}
	if modelVal.Kind() != reflect.Struct {
		return
	}

	auditableID := pkStr(db, modelVal)
	if auditableID == "" {
		return
	}

	destMap := db.Statement.Dest.(map[string]interface{})
	onlyMode := schemaOnlyMode(db.Statement)
	changes := make(datatypes.JSONMap, len(destMap))
	for col, newVal := range destMap {
		tag := ""
		dbCol := col
		if f := schema.LookUpField(col); f != nil {
			tag = f.Tag.Get("auditable")
			dbCol = f.DBName
		}
		if !includeField(tag, onlyMode) {
			continue
		}
		if tag == "redact" {
			changes[dbCol] = []interface{}{p.config.RedactedValue, p.config.RedactedValue}
		} else {
			changes[dbCol] = []interface{}{nil, newVal}
		}
	}
	if len(changes) == 0 {
		return
	}

	auditableType := schema.ModelType.Name()
	p.persist(db, &Audit{
		AuditableID:    auditableID,
		AuditableType:  auditableType,
		UserID:         userID,
		Action:         ActionUpdate,
		AuditedChanges: changes,
		Version:        p.nextVersion(db, auditableType, auditableID),
		Comment:        comment,
		CreatedAt:      time.Now(),
	})
}

// auditFromChangedCols handles db.Model(&m).Update("Field", value) and
// db.Model(&m).Updates(struct) where GORM reports changed columns via
// db.Statement.Changed.
func (p *plugin) auditFromChangedCols(db *gorm.DB, userID, comment *string) {
	schema := db.Statement.Schema

	modelVal := reflect.ValueOf(db.Statement.Model)
	if modelVal.Kind() == reflect.Ptr {
		modelVal = modelVal.Elem()
	}
	if modelVal.Kind() != reflect.Struct {
		return
	}

	auditableID := pkStr(db, modelVal)
	if auditableID == "" {
		return
	}

	changes := make(datatypes.JSONMap)
	onlyMode := schemaOnlyMode(db.Statement)
	for _, f := range schema.Fields {
		if !db.Statement.Changed(f.Name) {
			continue
		}
		tag := f.Tag.Get("auditable")
		if !includeField(tag, onlyMode) {
			continue
		}
		newVal, _ := f.ValueOf(db.Statement.Context, modelVal)
		if tag == "redact" {
			changes[f.DBName] = []interface{}{p.config.RedactedValue, p.config.RedactedValue}
		} else {
			changes[f.DBName] = []interface{}{nil, newVal}
		}
	}
	if len(changes) == 0 {
		return
	}

	auditableType := schema.ModelType.Name()
	p.persist(db, &Audit{
		AuditableID:    auditableID,
		AuditableType:  auditableType,
		UserID:         userID,
		Action:         ActionUpdate,
		AuditedChanges: changes,
		Version:        p.nextVersion(db, auditableType, auditableID),
		Comment:        comment,
		CreatedAt:      time.Now(),
	})
}

// auditSingle handles create and delete for a single struct value.
func (p *plugin) auditSingle(db *gorm.DB, rv reflect.Value, action Action, userID, comment *string) {
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return
	}

	schema := db.Statement.Schema
	auditableType := schema.ModelType.Name()
	auditableID := pkStr(db, rv)
	if auditableID == "" && action != ActionCreate {
		return
	}

	changes := make(datatypes.JSONMap)
	onlyMode := schemaOnlyMode(db.Statement)
	for _, field := range schema.Fields {
		tag := field.Tag.Get("auditable")
		// Timestamps are infrastructure noise — skip them.
		switch field.Name {
		case "CreatedAt", "UpdatedAt", "DeletedAt":
			continue
		}
		if !includeField(tag, onlyMode) {
			continue
		}

		val, _ := field.ValueOf(db.Statement.Context, rv)
		switch action {
		case ActionCreate:
			if tag == "redact" {
				changes[field.DBName] = []interface{}{nil, p.config.RedactedValue}
			} else {
				changes[field.DBName] = []interface{}{nil, val}
			}
		case ActionDelete:
			if tag == "redact" {
				changes[field.DBName] = []interface{}{p.config.RedactedValue, nil}
			} else {
				changes[field.DBName] = []interface{}{val, nil}
			}
		}
	}
	if len(changes) == 0 {
		return
	}

	p.persist(db, &Audit{
		AuditableID:    auditableID,
		AuditableType:  auditableType,
		UserID:         userID,
		Action:         action,
		AuditedChanges: changes,
		Version:        p.nextVersion(db, auditableType, auditableID),
		Comment:        comment,
		CreatedAt:      time.Now(),
	})
}
