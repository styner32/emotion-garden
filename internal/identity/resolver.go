package identity

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

const (
	ModeSingleUser    = "single_user"
	ModeAuthenticated = "authenticated"
)

type Principal struct {
	UserID uuid.UUID
	Mode   string
}

type IdentityResolver interface {
	Resolve(r *http.Request) (Principal, error)
}

type SingleUserResolver struct {
	bootstrapUserID uuid.UUID
}

func NewSingleUserResolver(bootstrapUserID uuid.UUID) *SingleUserResolver {
	return &SingleUserResolver{bootstrapUserID: bootstrapUserID}
}

// Resolve always returns the server-bootstrapped owner ID and ignores any
// client-provided owner headers or query parameters (AUTH01).
func (s *SingleUserResolver) Resolve(_ *http.Request) (Principal, error) {
	return Principal{
		UserID: s.bootstrapUserID,
		Mode:   ModeSingleUser,
	}, nil
}

type principalKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}
