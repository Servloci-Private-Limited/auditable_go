package mongoaudit

import (
	"context"
	"fmt"
)

type contextKey string

const defaultUserIDContextKey contextKey = "user_id"
const defaultCommentContextKey contextKey = "comment"

// WithUserID attaches the acting user's identifier to the context so every
// MongoDB mutation routed through AuditableCollection records who made it.
func WithUserID(ctx context.Context, userID any) context.Context {
	return context.WithValue(ctx, defaultUserIDContextKey, userID)
}

// UserIDFromContext retrieves the user ID previously attached with WithUserID.
func UserIDFromContext(ctx context.Context) (string, bool) {
	return valueFromContext(ctx, defaultUserIDContextKey)
}

// WithComment attaches a human-readable reason for the change to the context.
func WithComment(ctx context.Context, comment string) context.Context {
	return context.WithValue(ctx, defaultCommentContextKey, comment)
}

// CommentFromContext retrieves the comment previously attached with WithComment.
func CommentFromContext(ctx context.Context) (string, bool) {
	return valueFromContext(ctx, defaultCommentContextKey)
}

func valueFromContext(ctx context.Context, key any) (string, bool) {
	if ctx == nil {
		return "", false
	}
	value := ctx.Value(key)
	if value == nil {
		return "", false
	}
	switch typed := value.(type) {
	case string:
		return typed, typed != ""
	case fmt.Stringer:
		result := typed.String()
		return result, result != ""
	case []byte:
		result := string(typed)
		return result, result != ""
	default:
		result := fmt.Sprint(typed)
		return result, result != ""
	}
}
