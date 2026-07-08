package auditable

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
	//   db.Use(auditable.New(auditable.Config{
	//       OnAudit: func(a *auditable.Audit) {
	//           log.Printf("[audit] %s %s#%s", a.Action, a.AuditableType, a.AuditableID)
	//       },
	//   }))
	OnAudit func(*Audit)

	// OnError, when set, is called when writing an audit row or calculating the
	// next version fails. The original data mutation has already completed when
	// these callbacks run.
	OnError func(error)

	// UserIDResolver, when set, is called to obtain the current user ID from
	// the request context. The function should return the user-ID string and
	// true when a user is present, or ("", false) when no user is available.
	// When nil the default context key set by WithUserID is used.
	//
	//   db.Use(auditable.New(auditable.Config{
	//       UserIDResolver: func(ctx context.Context) (string, bool) {
	//           u, ok := ctx.Value(myKey{}).(string)
	//           return u, ok && u != ""
	//       },
	//   }))
	UserIDResolver func(ctx context.Context) (string, bool)
}

type plugin struct {
	config     Config
	auditTable string
	skipSet    map[string]struct{}
}

// New returns a GORM plugin that writes an audit row after every create,
// update, and delete. Register it once at DB initialisation time:
//
//	db.Use(auditable.New(auditable.Config{}))
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

func (p *plugin) Name() string { return "auditable" }

func (p *plugin) Initialize(db *gorm.DB) error {
	if err := db.Callback().Create().After("gorm:create").Register("auditable:create", p.afterCreate); err != nil {
		return err
	}
	if err := db.Callback().Update().After("gorm:update").Register("auditable:update", p.afterUpdate); err != nil {
		return err
	}
	if err := db.Callback().Delete().After("gorm:delete").Register("auditable:delete", p.afterDelete); err != nil {
		return err
	}
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

func (p *plugin) resolveUserID(db *gorm.DB) *string {
	var s string
	var ok bool
	if p.config.UserIDResolver != nil {
		s, ok = p.config.UserIDResolver(db.Statement.Context)
	} else {
		s, ok = UserIDFromContext(db.Statement.Context)
	}
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

// persist writes the audit record and fires OnAudit if configured.
func (p *plugin) persist(db *gorm.DB, a *Audit) {
	if err := db.Session(&gorm.Session{NewDB: true}).Table(p.auditTable).Create(a).Error; err != nil {
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

// nextVersion returns the next sequential version number for the given entity.
func (p *plugin) nextVersion(db *gorm.DB, auditableType, auditableID string) uint64 {
	var v uint64
	result := db.Session(&gorm.Session{NewDB: true}).
		Model(&Audit{}).
		Table(p.auditTable).
		Where("auditable_type = ? AND auditable_id = ?", auditableType, auditableID).
		Select("COALESCE(MAX(version), 0)").
		Scan(&v)
	if result.Error != nil {
		p.handleError(result.Error)
		return 1
	}
	return v + 1
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
	changes := make(datatypes.JSONMap, len(destMap))
	for col, newVal := range destMap {
		tag := ""
		if f := schema.LookUpField(col); f != nil {
			tag = f.Tag.Get("auditable")
		}
		if tag == "false" {
			continue
		}
		if tag == "redact" {
			changes[col] = []interface{}{p.config.RedactedValue, p.config.RedactedValue}
		} else {
			changes[col] = []interface{}{nil, newVal}
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
	for _, f := range schema.Fields {
		if !db.Statement.Changed(f.Name) {
			continue
		}
		tag := f.Tag.Get("auditable")
		if tag == "false" {
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
	for _, field := range schema.Fields {
		tag := field.Tag.Get("auditable")
		if tag == "false" {
			continue
		}
		// Timestamps are infrastructure noise — skip them.
		switch field.Name {
		case "CreatedAt", "UpdatedAt", "DeletedAt":
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
