// Package identity carries a principal established by verified authentication.
package identity

import "context"

type contextKey struct{}

// WithUser is for trusted authentication adapters, not request parameters.
func WithUser(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

func UserID(ctx context.Context) string {
	id, _ := ctx.Value(contextKey{}).(string)
	return id
}
