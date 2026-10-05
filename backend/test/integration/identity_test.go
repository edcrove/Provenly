//go:build integration

package integration

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/identity"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

func kind(t *testing.T, err error) apperr.Kind {
	t.Helper()
	e, ok := apperr.As(err)
	require.True(t, ok, "%v", err)
	return e.Kind
}

func TestIdentity(t *testing.T) {
	t.Run("BE-INT-036_accounts_sign_in_and_are_protected_by_the_database", func(t *testing.T) {
		s, ctx := fresh(t)
		require.NoError(t, s.Identity.Bootstrap(ctx, "admin", "correct horse"))
		require.NoError(t, s.Identity.Bootstrap(ctx, "other", "correct horse"), "only the first start creates an admin")
		sess, err := s.Identity.Login(ctx, "admin", "correct horse")
		require.NoError(t, err)
		assert.True(t, sess.User.IsAdmin)
		u, err := s.Identity.Authenticate(ctx, sess.Token)
		require.NoError(t, err)
		assert.Equal(t, "admin", u.Username)

		next, err := s.Identity.ChangePassword(ctx, u, "correct horse", "a brand new password")
		require.NoError(t, err)
		_, err = s.Identity.Authenticate(ctx, sess.Token)
		assert.Equal(t, apperr.KindUnauthorized, kind(t, err), "a password change signs other sessions out")
		_, err = s.Identity.Authenticate(ctx, next.Token)
		assert.NoError(t, err)

		page, err := s.Identity.ListUsers(ctx, next.User, pagination.Page{Number: 1, Size: 10})
		require.NoError(t, err)
		assert.Equal(t, int64(1), page.Total)
		assert.Equal(t, "admin", page.Items[0].Username)

		for _, stmt := range []string{
			`INSERT INTO users (username, display_name, password_hash) VALUES ('Admin2', 'x', '$2a$x')`,
			`INSERT INTO users (username, display_name, password_hash) VALUES ('ok_user', ' ', '$2a$x')`,
			`INSERT INTO users (username, display_name, password_hash) VALUES ('ok_user', 'x', 'plaintext')`,
			`INSERT INTO users (username, display_name, password_hash) VALUES ('admin', 'x', '$2a$x')`,
			`DELETE FROM users WHERE username = 'admin'`,
			`UPDATE users SET username = 'root' WHERE username = 'admin'`,
		} {
			_, err := db.Pool.Exec(ctx, stmt)
			assert.Error(t, err, stmt)
		}
	})

	t.Run("BE-INT-037_invitations_are_single_use_and_immutable_once_settled", func(t *testing.T) {
		s, ctx := fresh(t)
		require.NoError(t, s.Identity.Bootstrap(ctx, "admin", "correct horse"))
		admin, _ := s.Identity.Login(ctx, "admin", "correct horse")
		email := "ana@example.com"
		inv, token, err := s.Identity.CreateInvitation(ctx, admin.User, identity.CreateInvitationInput{Email: &email, Note: "QA"})
		require.NoError(t, err)
		other, _, err := s.Identity.CreateInvitation(ctx, admin.User, identity.CreateInvitationInput{})
		require.NoError(t, err)
		list, err := s.Identity.ListInvitations(ctx, admin.User, pagination.Page{Number: 1, Size: 1})
		require.NoError(t, err)
		assert.Equal(t, int64(2), list.Total)
		assert.Equal(t, other.ID, list.Items[0].ID, "newest first")

		var stored []byte
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT token_sha256 FROM invitations WHERE id = $1`, inv.ID).Scan(&stored))
		assert.Equal(t, identity.TokenDigest(token), stored, "only the digest of the link is stored")

		ana, err := s.Identity.AcceptInvitation(ctx, identity.AcceptInput{Token: token, Username: "ana", DisplayName: "Ana", Password: "ana's password"})
		require.NoError(t, err)
		assert.Equal(t, email, *ana.User.Email)
		_, err = s.Identity.AcceptInvitation(ctx, identity.AcceptInput{Token: token, Username: "ana2", DisplayName: "Ana", Password: "ana's password"})
		assert.Equal(t, apperr.KindNotFound, kind(t, err))
		_, err = s.Identity.RevokeInvitation(ctx, admin.User, inv.ID)
		assert.Equal(t, apperr.KindConflict, kind(t, err))
		revoked, err := s.Identity.RevokeInvitation(ctx, admin.User, other.ID)
		require.NoError(t, err)
		assert.NotNil(t, revoked.RevokedAt)
		_, err = s.Identity.RevokeInvitation(ctx, admin.User, 987654)
		assert.Equal(t, apperr.KindNotFound, kind(t, err))

		for _, stmt := range []string{
			`DELETE FROM invitations`,
			`UPDATE invitations SET accepted_at = NULL, accepted_user_id = NULL WHERE accepted_at IS NOT NULL`,
			`UPDATE invitations SET revoked_at = NULL WHERE revoked_at IS NOT NULL`,
			`UPDATE invitations SET expires_at = expires_at + interval '1 day'`,
			`UPDATE invitations SET revoked_at = now() WHERE accepted_at IS NOT NULL`,
			`INSERT INTO invitations (token_sha256, created_by, expires_at) VALUES ('short', 1, now() + interval '1 day')`,
			`INSERT INTO invitations (token_sha256, created_by, created_at, expires_at) VALUES (sha256('x'), 1, now(), now() - interval '1 second')`,
		} {
			_, err := db.Pool.Exec(ctx, stmt)
			assert.Error(t, err, stmt)
		}
	})

	t.Run("BE-INT-038_concurrent_acceptances_create_one_account", func(t *testing.T) {
		s, ctx := fresh(t)
		require.NoError(t, s.Identity.Bootstrap(ctx, "admin", "correct horse"))
		admin, _ := s.Identity.Login(ctx, "admin", "correct horse")
		_, token, err := s.Identity.CreateInvitation(ctx, admin.User, identity.CreateInvitationInput{})
		require.NoError(t, err)
		_, token2, err := s.Identity.CreateInvitation(ctx, admin.User, identity.CreateInvitationInput{})
		require.NoError(t, err)

		const n = 8
		var wg sync.WaitGroup
		results := make(chan error, 2*n)
		for i := 0; i < n; i++ {
			wg.Add(2)
			go func(i int) {
				defer wg.Done()
				_, err := s.Identity.AcceptInvitation(context.Background(), identity.AcceptInput{Token: token, Username: "user" + itoa(int64(i)), DisplayName: "U", Password: "a long password"})
				results <- err
			}(i)
			go func() {
				defer wg.Done()
				// Same username through another link: at most one account per name.
				_, err := s.Identity.AcceptInvitation(context.Background(), identity.AcceptInput{Token: token2, Username: "twin", DisplayName: "T", Password: "a long password"})
				results <- err
			}()
		}
		wg.Wait()
		close(results)
		ok := 0
		for err := range results {
			if err == nil {
				ok++
				continue
			}
			assert.Contains(t, []apperr.Kind{apperr.KindNotFound, apperr.KindConflict}, kind(t, err))
		}
		assert.Equal(t, 2, ok, "one account per link")
		var users int
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&users))
		assert.Equal(t, 3, users)
	})
}
