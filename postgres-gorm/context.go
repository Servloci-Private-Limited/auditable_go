package auditablegorm

import (
	"context"
	"fmt"
)

type contextKey string

const defaultUserIDContextKey contextKey = "user_id"
const defaultCommentContextKey contextKey = "comment"

func WithUserID(ctx context.Context, userID any) context.Context {
	return context.WithValue(ctx, defaultUserIDContextKey, userID)
}

func UserIDFromContext(ctx context.Context) (string, bool) {
	return userIDFromContext(ctx, defaultUserIDContextKey)
}

func WithComment(ctx context.Context, comment string) context.Context {
	return context.WithValue(ctx, defaultCommentContextKey, comment)
}

func CommentFromContext(ctx context.Context) (string, bool) {
	return userIDFromContext(ctx, defaultCommentContextKey)
}

func userIDFromContext(ctx context.Context, key any) (string, bool) {
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
