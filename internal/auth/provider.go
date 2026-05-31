package auth

import "context"

// Identity is the result of authenticating a participant.
type Identity struct {
	DisplayName    string
	JellyfinUserID string // empty for guests
}

// Credentials are the inputs to a Provider. Guests supply only a DisplayName.
type Credentials struct {
	DisplayName string
	Username    string
	Password    string
}

// Provider authenticates participants. GuestProvider is the only one in the
// MVP. A JellyfinProvider (Phase 2) will implement the same interface against
// /Users/AuthenticateByName using the modern Jellyfin auth header, after which
// the participant gains a non-empty JellyfinUserID.
type Provider interface {
	Name() string
	Authenticate(ctx context.Context, c Credentials) (Identity, error)
}

// GuestProvider accepts any non-empty display name without a credential check.
type GuestProvider struct{}

// Name returns the provider key.
func (GuestProvider) Name() string { return "guest" }

// Authenticate echoes the supplied display name as a guest identity.
func (GuestProvider) Authenticate(_ context.Context, c Credentials) (Identity, error) {
	return Identity{DisplayName: c.DisplayName}, nil
}

var _ Provider = GuestProvider{}
