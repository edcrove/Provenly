package audit

import (
	"context"
	"log/slog"

	"github.com/edcrove/provenly/backend/internal/identity"
	"github.com/edcrove/provenly/backend/internal/platform/clientinfo"
)

// The audit Service records identity's sign-in events (card #49).
var _ identity.AuthLog = (*Service)(nil)

// signIns says, for each sign-in event, which route it answered and what it means in words.
var signIns = map[string]struct{ route, path, summary string }{
	identity.AuthLogin:              {"POST /api/v1/auth/login", "/api/v1/auth/login", "signed in"},
	identity.AuthLoginFailed:        {"POST /api/v1/auth/login", "/api/v1/auth/login", "failed to sign in"},
	identity.AuthLockedOut:          {"POST /api/v1/auth/login", "/api/v1/auth/login", "was refused: too many failed sign-ins"},
	identity.AuthLogout:             {"POST /api/v1/auth/logout", "/api/v1/auth/logout", "signed out"},
	identity.AuthInvitationAccepted: {"POST /api/v1/invitations/accept", "/api/v1/invitations/accept", "accepted an invitation"},
	identity.AuthPasswordReset:      {"POST /api/v1/password-reset", "/api/v1/password-reset", "set a new password with a reset link"},
}

// AuthEvent records a sign-in event with the client's IP and user agent, and logs it (event=auth.login_failed …).
// The actor is the account, or "unknown" when the username matches none: a mistyped password is never stored.
func (s *Service) AuthEvent(ctx context.Context, e identity.AuthEvent) {
	kind, ok := signIns[e.Kind]
	if !ok {
		slog.ErrorContext(ctx, "unknown sign-in event", "event", e.Kind)
		return
	}
	actor := e.Username
	if actor == "" {
		actor = "unknown"
	}
	client := clientinfo.From(ctx)
	ua := truncate(storable(client.UserAgent), 500)
	slog.InfoContext(ctx, "audit", "event", e.Kind, "actor", actor, "status", e.Status, "ip", client.IP, "user_agent", ua)
	ev := Event{Actor: actor, Action: kind.route, Path: kind.path, Status: int32(e.Status), Summary: kind.summary, IP: client.IP, UserAgent: ua}
	if err := s.repo.Insert(context.WithoutCancel(ctx), ev); err != nil {
		slog.ErrorContext(ctx, "audit event not recorded", "event", e.Kind, "error", err)
	}
}
