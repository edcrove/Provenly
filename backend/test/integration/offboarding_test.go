//go:build integration

package integration

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/identity"
)

func TestOffboarding(t *testing.T) {
	t.Run("BE-INT-067_deactivation_and_single_use_reset_links_hold_on_a_real_database", func(t *testing.T) {
		s, ctx := fresh(t)
		require.NoError(t, s.Identity.Bootstrap(ctx, "admin", "correct horse"))
		admin, err := s.Identity.Login(ctx, "admin", "correct horse")
		require.NoError(t, err)
		_, tok, err := s.Identity.CreateInvitation(ctx, admin.User, identity.CreateInvitationInput{})
		require.NoError(t, err)
		ana, err := s.Identity.AcceptInvitation(ctx, identity.AcceptInput{Token: tok, Username: "ana", DisplayName: "Ana", Password: "ana password"})
		require.NoError(t, err)

		u, err := s.Identity.Deactivate(ctx, admin.User, "ana")
		require.NoError(t, err)
		require.NotNil(t, u.DeactivatedAt)
		_, err = s.Identity.Authenticate(ctx, ana.Token)
		assert.Error(t, err, "the session stops at once")
		_, err = s.Identity.Deactivate(ctx, admin.User, "admin")
		assert.Error(t, err, "the last administrator stays")
		_, err = s.Identity.Reactivate(ctx, admin.User, "ana")
		require.NoError(t, err)

		// Ten concurrent uses of one link: exactly one sets the password.
		_, link, err := s.Identity.CreatePasswordReset(ctx, admin.User, "ana")
		require.NoError(t, err)
		var wg sync.WaitGroup
		var mu sync.Mutex
		ok := 0
		for range 10 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := s.Identity.ResetPassword(ctx, link, "ana's new password"); err == nil {
					mu.Lock()
					ok++
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		assert.Equal(t, 1, ok)
		_, err = s.Identity.Login(ctx, "ana", "ana's new password")
		require.NoError(t, err)

		// A newer link voids the older one.
		_, older, err := s.Identity.CreatePasswordReset(ctx, admin.User, "ana")
		require.NoError(t, err)
		_, _, err = s.Identity.CreatePasswordReset(ctx, admin.User, "ana")
		require.NoError(t, err)
		_, err = s.Identity.ResetPassword(ctx, older, "a third password")
		assert.Error(t, err)

		// The database keeps links: never deleted, only marked used, once.
		_, err = db.Pool.Exec(ctx, `DELETE FROM password_resets`)
		assert.ErrorContains(t, err, "cannot be deleted")
		_, err = db.Pool.Exec(ctx, `UPDATE password_resets SET used_at = now() WHERE used_at IS NOT NULL`)
		assert.ErrorContains(t, err, "only ever marked used, once")
		_, err = db.Pool.Exec(ctx, `UPDATE password_resets SET expires_at = expires_at + interval '1 day'`)
		assert.ErrorContains(t, err, "only ever marked used, once")
		_, err = db.Pool.Exec(ctx, `INSERT INTO password_resets (user_id, token_sha256, expires_at) VALUES (1, '\x00', now() + interval '1 day')`)
		assert.Error(t, err, "a token digest is 32 bytes")
		_, err = db.Pool.Exec(ctx, `INSERT INTO password_resets (user_id, token_sha256, expires_at) VALUES (1, decode(repeat('ab', 32), 'hex'), now() - interval '1 day')`)
		assert.ErrorContains(t, err, "password_resets_expiry")
	})
}
