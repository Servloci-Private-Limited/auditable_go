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

// ExtractUserID reads a user ID stored under key from ctx and converts it to a
// string. It handles string, []byte, fmt.Stringer, and any numeric type
// (int, int64, uint, uint64, …) — so both integer and string primary keys work
// without an explicit type assertion in your UserIDResolver:
//
//	UserIDResolver: func(ctx context.Context) (string, bool) {
//	    return auditablegorm.ExtractUserID(ctx, myMiddleware.UserKey)
//	}
func ExtractUserID(ctx context.Context, key any) (string, bool) {
	return userIDFromContext(ctx, key)
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
