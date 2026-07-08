package auditablegorm

import (
	"context"
	"testing"
)

func TestIncludeField(t *testing.T) {
	tests := []struct {
		tag        string
		only, want bool
	}{
		{"", false, true}, {"", true, false}, {"true", true, true}, {"only", true, true},
		{"redact", true, true}, {"-", false, false}, {"false", false, false},
	}
	for _, tc := range tests {
		if got := includeField(tc.tag, tc.only); got != tc.want {
			t.Errorf("includeField(%q, %v) = %v, want %v", tc.tag, tc.only, got, tc.want)
		}
	}
}

func TestContextMetadata(t *testing.T) {
	ctx := WithUserID(context.Background(), 42)
	ctx = WithComment(ctx, "approved")
	if got, ok := UserIDFromContext(ctx); !ok || got != "42" {
		t.Fatalf("unexpected user: %q, %v", got, ok)
	}
	if got, ok := CommentFromContext(ctx); !ok || got != "approved" {
		t.Fatalf("unexpected comment: %q, %v", got, ok)
	}
}

func TestAuditCallbackPanicIsContained(t *testing.T) {
	err := invokeAuditCallback(func(*Audit) { panic("boom") }, &Audit{})
	if err == nil {
		t.Fatal("expected callback panic to become an error")
	}
}
