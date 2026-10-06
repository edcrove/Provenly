package identity

import "context"

// Sign-in events the audit log records (card #49): the public routes have no signed-in caller, so identity reports
// them itself, with the username only when that account exists.
const (
	AuthLogin              = "auth.login"
	AuthLoginFailed        = "auth.login_failed"
	AuthLockedOut          = "auth.login_locked"
	AuthLogout             = "auth.logout"
	AuthInvitationAccepted = "auth.invitation_accepted"
	AuthPasswordReset      = "auth.password_reset"
)

// AuthEvent is one sign-in event: its kind, the account (empty when the username matches none) and the HTTP status
// the request answered.
type AuthEvent struct {
	Kind     string
	Username string
	Status   int
}

// AuthLog records sign-in events (the audit log).
type AuthLog interface {
	AuthEvent(ctx context.Context, e AuthEvent)
}

// SetAuthLog makes the service report sign-in events to l.
func (s *Service) SetAuthLog(l AuthLog) { s.authLog = l }

func (s *Service) authEvent(ctx context.Context, kind, username string, status int) {
	if s.authLog != nil {
		s.authLog.AuthEvent(ctx, AuthEvent{Kind: kind, Username: username, Status: status})
	}
}

// existing is username when an account has it, else "" (a locked-out name may be anything).
func (s *Service) existing(ctx context.Context, username string) string {
	if !UsernamePattern.MatchString(username) {
		return ""
	}
	if _, err := s.repo.GetUserByUsername(ctx, username); err != nil {
		return ""
	}
	return username
}

// Logout reports a sign-out by the session token's user; sessions are stateless, so an unknown or expired token
// changes nothing and is not reported.
func (s *Service) Logout(ctx context.Context, token string) {
	if token == "" {
		return
	}
	if u, err := s.Authenticate(ctx, token); err == nil {
		s.authEvent(ctx, AuthLogout, u.Username, 204)
	}
}
