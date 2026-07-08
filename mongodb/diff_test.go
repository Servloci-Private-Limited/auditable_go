package mongoaudit

import (
	"context"
	"errors"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

func TestFieldPolicyIsConsistentAcrossMongoV1Modes(t *testing.T) {
	policy := FieldPolicy{SkipFields: map[string]struct{}{"ignored": {}}, RedactedFields: map[string]struct{}{"secret": {}}, OnlyFields: map[string]struct{}{"visible": {}}, RedactedValue: "[HIDDEN]"}
	computer := defaultChangeComputer{}
	for _, req := range []ChangeRequest{
		{Mode: ChangeCreate, After: bson.M{"visible": 2, "secret": "new", "ignored": 1, "other": 3}, Policy: policy},
		{Mode: ChangeOperators, Update: bson.M{"$set": bson.M{"visible": 2, "secret": "new", "ignored": 1, "other": 3}}, Policy: policy},
		{Mode: ChangeReplace, Before: bson.M{"visible": 1, "secret": "old"}, After: bson.M{"visible": 2, "secret": "new"}, Policy: policy},
		{Mode: ChangeDelete, Before: bson.M{"visible": 2, "secret": "old", "ignored": 1, "other": 3}, Policy: policy},
	} {
		result, err := computer.Compute(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := result.Changes["visible"]; !ok {
			t.Fatal("visible field was not audited")
		}
		if _, ok := result.Changes["secret"]; !ok {
			t.Fatal("redacted field was not audited")
		}
		if _, ok := result.Changes["ignored"]; ok {
			t.Fatal("ignored field was audited")
		}
		if _, ok := result.Changes["other"]; ok {
			t.Fatal("untagged field was audited")
		}
	}
}

func TestMongoV1ComplexityLimit(t *testing.T) {
	err := requestComplexity(ChangeRequest{After: bson.M{"a": bson.M{"b": bson.M{"c": 1}}}}, DiffLimits{MaxDocumentBytes: 1024, MaxDepth: 2})
	if !errors.Is(err, ErrChangeTooComplex) {
		t.Fatalf("expected complexity error, got %v", err)
	}
}
