package identity

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
)

// errResetLink is the one answer to a reset link that cannot be used: unknown, expired, used, or of a deactivated
// user. It never says which.
var errResetLink = apperr.NotFound("password reset link not found, expired or already used")

// userByUsername resolves a username for an administrator's action (404 when it does not exist).
func (s *Service) userByUsername(ctx context.Context, username string) (User, error) {
	username = normalizeUsername(username)
	var u User
	err := ErrNotFound
	if UsernamePattern.MatchString(username) {
		u, err = s.repo.GetUserByUsername(ctx, username)
	}
	if errors.Is(err, ErrNotFound) {
		return User{}, apperr.NotFound("user %s not found", username)
	}
	return u, err
}

// Deactivate stops a user's access at once: their sessions are refused from the next request and they cannot sign
// in. Project API keys are the project's and keep working. Administrators only; nobody deactivates themselves, and
// the last active administrator stays.
func (s *Service) Deactivate(ctx context.Context, actor User, username string) (User, error) {
	if err := requireAdmin(actor); err != nil {
		return User{}, err
	}
	var out User
	err := s.repo.InTx(ctx, func(r Repository) error {
		u, err := s.userByUsername(ctx, username)
		if err != nil {
			return err
		}
		if u.ID == actor.ID {
			return apperr.Conflict("you cannot deactivate yourself: ask another administrator")
		}
		if u.DeactivatedAt != nil {
			out = u
			return nil
		}
		if u.IsAdmin {
			n, err := r.CountActiveAdmins(ctx)
			if err != nil {
				return err
			}
			if n <= 1 {
				return apperr.Conflict("%s is the last active administrator", u.Username)
			}
		}
		now := s.now()
		if out, err = r.SetUserDeactivated(ctx, u.ID, &now); err != nil {
			return err
		}
		return r.VoidPasswordResets(ctx, u.ID)
	})
	return out, err
}

// Reactivate gives a deactivated user their access back (administrators only).
func (s *Service) Reactivate(ctx context.Context, actor User, username string) (User, error) {
	if err := requireAdmin(actor); err != nil {
		return User{}, err
	}
	u, err := s.userByUsername(ctx, username)
	if err != nil || u.DeactivatedAt == nil {
		return u, err
	}
	return s.repo.SetUserDeactivated(ctx, u.ID, nil)
}

// CreatePasswordReset makes a single-use link to set a new password for an active user (administrators only). Its
// token is shown once; any earlier pending link of the user stops working.
func (s *Service) CreatePasswordReset(ctx context.Context, actor User, username string) (PasswordReset, string, error) {
	if err := requireAdmin(actor); err != nil {
		return PasswordReset{}, "", err
	}
	u, err := s.userByUsername(ctx, username)
	if err != nil {
		return PasswordReset{}, "", err
	}
	if u.DeactivatedAt != nil {
		return PasswordReset{}, "", apperr.Conflict("%s is deactivated: reactivate the account first", u.Username)
	}
	by := actor.ID
	return s.newPasswordReset(ctx, u, &by)
}

// BreakGlassReset makes a reset link for any user without an administrator (the `provenly reset-password` command,
// run on the server): the way back in when the only administrator lost their password. It reactivates the user.
func (s *Service) BreakGlassReset(ctx context.Context, username string) (PasswordReset, string, error) {
	u, err := s.userByUsername(ctx, username)
	if err != nil {
		return PasswordReset{}, "", err
	}
	if u.DeactivatedAt != nil {
		if u, err = s.repo.SetUserDeactivated(ctx, u.ID, nil); err != nil {
			return PasswordReset{}, "", err
		}
	}
	return s.newPasswordReset(ctx, u, nil)
}

func (s *Service) newPasswordReset(ctx context.Context, u User, by *int64) (PasswordReset, string, error) {
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	token := base64.RawURLEncoding.EncodeToString(raw)
	var out PasswordReset
	err := s.repo.InTx(ctx, func(r Repository) error {
		if err := r.VoidPasswordResets(ctx, u.ID); err != nil {
			return err
		}
		var err error
		out, err = r.CreatePasswordReset(ctx, PasswordReset{UserID: u.ID, TokenSHA256: TokenDigest(token), CreatedBy: by,
			ExpiresAt: s.now().Add(s.cfg.PasswordResetTTL)})
		return err
	})
	if err != nil {
		return PasswordReset{}, "", err
	}
	return out, token, nil
}

// ResetPassword sets a new password through a reset link, once, and signs the user in; every older session ends.
func (s *Service) ResetPassword(ctx context.Context, token, password string) (Session, error) {
	var v apperr.Validator
	v.Check(token != "", "token", "is required")
	validatePassword(&v, "password", password)
	if err := v.Err(); err != nil {
		return Session{}, err
	}
	var user User
	err := s.repo.InTx(ctx, func(r Repository) error {
		reset, err := r.LockPasswordResetByToken(ctx, TokenDigest(token))
		if errors.Is(err, ErrNotFound) || (err == nil && (reset.UsedAt != nil || !s.now().Before(reset.ExpiresAt))) {
			return errResetLink
		}
		if err != nil {
			return err
		}
		u, err := r.GetUser(ctx, reset.UserID)
		if err != nil {
			return err
		}
		if u.DeactivatedAt != nil {
			return errResetLink
		}
		// The password is hashed only for a usable link: made-up tokens cost no bcrypt work.
		hash, err := s.hash(password)
		if err != nil {
			return err
		}
		if user, err = r.SetPasswordHash(ctx, u.ID, hash); err != nil {
			return err
		}
		if err := r.MarkPasswordResetUsed(ctx, reset.ID); err != nil {
			return err
		}
		return r.VoidPasswordResets(ctx, u.ID)
	})
	if err != nil {
		return Session{}, err
	}
	return s.issue(user)
}
