package middleware

import (
	"context"

	"github.com/11DingKing/youth-training-load-ledger/internal/domain"
)

type userKey struct{}
type tokenKey struct{}

func WithUser(ctx context.Context, user domain.User) context.Context {
	return context.WithValue(ctx, userKey{}, user)
}

func User(ctx context.Context) (domain.User, bool) {
	user, ok := ctx.Value(userKey{}).(domain.User)
	return user, ok
}

func WithToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, tokenKey{}, token)
}

func Token(ctx context.Context) string {
	token, _ := ctx.Value(tokenKey{}).(string)
	return token
}
