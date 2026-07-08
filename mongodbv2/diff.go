package mongoaudit

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type ChangeMode string

const (
	ChangeCreate    ChangeMode = "create"
	ChangeOperators ChangeMode = "operators"
	ChangeReplace   ChangeMode = "replace"
	ChangeDelete    ChangeMode = "delete"
)

type DiffStatus string

const (
	DiffInline   DiffStatus = "inline"
	DiffDeferred DiffStatus = "deferred"
)

var ErrChangeTooComplex = errors.New("mongoaudit: change exceeds configured inline diff limits")

type FieldPolicy struct {
	SkipFields, RedactedFields, OnlyFields map[string]struct{}
	RedactedValue                          string
}
type ChangeRequest struct {
	Mode                   ChangeMode
	Before, After          bson.M
	Update                 interface{}
	Policy                 FieldPolicy
	Collection, DocumentID string
}
type ChangeResult struct {
	Changes   bson.M
	Reference string
	Status    DiffStatus
}

type ChangeComputer interface {
	Compute(context.Context, ChangeRequest) (ChangeResult, error)
}
type ChangeComputerFunc func(context.Context, ChangeRequest) (ChangeResult, error)

func (f ChangeComputerFunc) Compute(ctx context.Context, req ChangeRequest) (ChangeResult, error) {
	return f(ctx, req)
}

type DeferredChangeHandler interface {
	Defer(context.Context, ChangeRequest, error) (ChangeResult, error)
}
type DeferredChangeHandlerFunc func(context.Context, ChangeRequest, error) (ChangeResult, error)

func (f DeferredChangeHandlerFunc) Defer(ctx context.Context, req ChangeRequest, reason error) (ChangeResult, error) {
	return f(ctx, req, reason)
}

type DiffLimits struct {
	MaxDocumentBytes, MaxDepth, MaxChangedFields int
	ComputeTimeout                               time.Duration
}

func defaultDiffLimits(v DiffLimits) DiffLimits {
	if v.MaxDocumentBytes == 0 {
		v.MaxDocumentBytes = 1 << 20
	}
	if v.MaxDepth == 0 {
		v.MaxDepth = 64
	}
	if v.MaxChangedFields == 0 {
		v.MaxChangedFields = 1000
	}
	if v.ComputeTimeout == 0 {
		v.ComputeTimeout = 250 * time.Millisecond
	}
	return v
}

type defaultChangeComputer struct{}

func (defaultChangeComputer) Compute(ctx context.Context, req ChangeRequest) (ChangeResult, error) {
	if err := ctx.Err(); err != nil {
		return ChangeResult{}, err
	}
	p := req.Policy
	var changes bson.M
	switch req.Mode {
	case ChangeCreate:
		changes = createChanges(req.After, p.SkipFields, p.RedactedFields, p.RedactedValue, p.OnlyFields)
	case ChangeOperators:
		changes = extractUpdateChanges(req.Update, p.SkipFields, p.RedactedFields, p.RedactedValue, p.OnlyFields)
	case ChangeReplace:
		changes = diffDocs(req.Before, req.After, p.SkipFields, p.RedactedFields, p.RedactedValue, p.OnlyFields)
	case ChangeDelete:
		changes = deleteChanges(req.Before, p.SkipFields, p.RedactedFields, p.RedactedValue, p.OnlyFields)
	default:
		return ChangeResult{}, fmt.Errorf("mongoaudit: unsupported change mode %q", req.Mode)
	}
	return ChangeResult{Changes: changes, Status: DiffInline}, nil
}

func requestComplexity(req ChangeRequest, limits DiffLimits) error {
	for _, doc := range []bson.M{req.Before, req.After} {
		if doc == nil {
			continue
		}
		raw, err := bson.Marshal(doc)
		if err != nil {
			return err
		}
		if len(raw) > limits.MaxDocumentBytes {
			return fmt.Errorf("%w: document is %d bytes", ErrChangeTooComplex, len(raw))
		}
		if depthOf(doc, limits.MaxDepth) > limits.MaxDepth {
			return fmt.Errorf("%w: depth exceeds %d", ErrChangeTooComplex, limits.MaxDepth)
		}
	}
	return nil
}

func sanitizeDeferredRequest(req ChangeRequest) (ChangeRequest, error) {
	req.Before = sanitizeDocument(req.Before, req.Policy)
	req.After = sanitizeDocument(req.After, req.Policy)
	if req.Update != nil {
		update, err := toMap(req.Update)
		if err != nil {
			return ChangeRequest{}, fmt.Errorf("mongoaudit: cannot safely serialize deferred update: %w", err)
		}
		safe := bson.M{}
		for op, raw := range update {
			fields, err := toMap(raw)
			if err != nil {
				return ChangeRequest{}, err
			}
			safe[op] = sanitizeDocument(fields, req.Policy)
		}
		req.Update = safe
	}
	return req, nil
}

func sanitizeDocument(doc bson.M, policy FieldPolicy) bson.M {
	if doc == nil {
		return nil
	}
	safe := bson.M{}
	for key, value := range doc {
		if _, skip := policy.SkipFields[key]; skip || !includedField(key, policy.OnlyFields, policy.RedactedFields) {
			continue
		}
		if _, redact := policy.RedactedFields[key]; redact {
			safe[key] = policy.RedactedValue
		} else {
			safe[key] = value
		}
	}
	return safe
}

func depthOf(root interface{}, stopAfter int) int {
	type item struct {
		value reflect.Value
		depth int
	}
	stack := []item{{reflect.ValueOf(root), 1}}
	maxDepth := 0
	for len(stack) > 0 {
		last := len(stack) - 1
		current := stack[last]
		stack = stack[:last]
		v := current.value
		for v.IsValid() && (v.Kind() == reflect.Interface || v.Kind() == reflect.Ptr) {
			if v.IsNil() {
				v = reflect.Value{}
				break
			}
			v = v.Elem()
		}
		if !v.IsValid() {
			continue
		}
		if current.depth > maxDepth {
			maxDepth = current.depth
		}
		if maxDepth > stopAfter {
			return maxDepth
		}
		switch v.Kind() {
		case reflect.Map:
			iter := v.MapRange()
			for iter.Next() {
				stack = append(stack, item{iter.Value(), current.depth + 1})
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				stack = append(stack, item{v.Index(i), current.depth + 1})
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if v.Field(i).CanInterface() {
					stack = append(stack, item{v.Field(i), current.depth + 1})
				}
			}
		}
	}
	return maxDepth
}
