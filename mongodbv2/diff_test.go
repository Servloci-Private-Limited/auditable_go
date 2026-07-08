package mongoaudit

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestDefaultChangeComputerAppliesOnePolicyToEveryMode(t *testing.T) {
	policy := FieldPolicy{
		SkipFields:     map[string]struct{}{"ignored": {}},
		RedactedFields: map[string]struct{}{"secret": {}},
		OnlyFields:     map[string]struct{}{"visible": {}},
		RedactedValue:  "[HIDDEN]",
	}
	computer := defaultChangeComputer{}
	tests := []struct {
		name string
		req  ChangeRequest
	}{
		{"create", ChangeRequest{Mode: ChangeCreate, After: bson.M{"visible": 2, "secret": "new", "ignored": 1, "other": 3}, Policy: policy}},
		{"operators", ChangeRequest{Mode: ChangeOperators, Update: bson.M{"$set": bson.M{"visible": 2, "secret": "new", "ignored": 1, "other": 3}}, Policy: policy}},
		{"replace", ChangeRequest{Mode: ChangeReplace, Before: bson.M{"visible": 1, "secret": "old", "ignored": 0, "other": 2}, After: bson.M{"visible": 2, "secret": "new", "ignored": 1, "other": 3}, Policy: policy}},
		{"delete", ChangeRequest{Mode: ChangeDelete, Before: bson.M{"visible": 2, "secret": "old", "ignored": 1, "other": 3}, Policy: policy}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := computer.Compute(context.Background(), tc.req)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := result.Changes["visible"]; !ok {
				t.Fatal("visible field was not audited")
			}
			if _, ok := result.Changes["secret"]; !ok {
				t.Fatal("redacted field was dropped in whitelist mode")
			}
			if _, ok := result.Changes["ignored"]; ok {
				t.Fatal("ignored field was audited")
			}
			if _, ok := result.Changes["other"]; ok {
				t.Fatal("untagged field was audited in whitelist mode")
			}
		})
	}
}

func TestNotDeletedPreservesNonMapFilter(t *testing.T) {
	filter := bson.D{{Key: "status", Value: "active"}}
	got := NotDeleted(filter)
	parts, ok := got["$and"].(bson.A)
	if !ok || len(parts) != 2 {
		t.Fatalf("expected an $and filter, got %#v", got)
	}
	if _, ok := parts[0].(bson.D); !ok {
		t.Fatalf("original bson.D filter was not preserved: %#v", parts[0])
	}
}

func TestComplexityLimitsDetectSizeAndDepth(t *testing.T) {
	limits := DiffLimits{MaxDocumentBytes: 32, MaxDepth: 2, MaxChangedFields: 10, ComputeTimeout: time.Second}
	err := requestComplexity(ChangeRequest{After: bson.M{"payload": "a value that is larger than the configured limit"}}, limits)
	if !errors.Is(err, ErrChangeTooComplex) {
		t.Fatalf("expected size error, got %v", err)
	}
	err = requestComplexity(ChangeRequest{After: bson.M{"a": bson.M{"b": bson.M{"c": 1}}}}, DiffLimits{MaxDocumentBytes: 1024, MaxDepth: 2})
	if !errors.Is(err, ErrChangeTooComplex) {
		t.Fatalf("expected depth error, got %v", err)
	}
}

func TestChangeComputerFuncReceivesContextDeadline(t *testing.T) {
	computer := ChangeComputerFunc(func(ctx context.Context, _ ChangeRequest) (ChangeResult, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("custom computer did not receive a deadline")
		}
		return ChangeResult{Changes: bson.M{"custom": bson.A{1, 2}}}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := computer.Compute(ctx, ChangeRequest{})
	if err != nil || len(result.Changes) != 1 {
		t.Fatalf("unexpected custom result: %#v, %v", result, err)
	}
}

func TestDeferredRequestIsSanitized(t *testing.T) {
	req, err := sanitizeDeferredRequest(ChangeRequest{
		Before: bson.M{"visible": 1, "secret": "raw", "ignored": "raw"},
		Policy: FieldPolicy{RedactedFields: map[string]struct{}{"secret": {}}, SkipFields: map[string]struct{}{"ignored": {}}, RedactedValue: "[HIDDEN]"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if req.Before["secret"] != "[HIDDEN]" {
		t.Fatalf("secret was not sanitized: %#v", req.Before)
	}
	if _, ok := req.Before["ignored"]; ok {
		t.Fatalf("ignored field reached deferred handler: %#v", req.Before)
	}
}

func TestAuditCallbackPanicIsContained(t *testing.T) {
	err := invokeAuditCallback(func(*Audit) { panic("boom") }, &Audit{})
	if err == nil {
		t.Fatal("expected callback panic to become an error")
	}
}
