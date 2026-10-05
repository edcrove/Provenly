//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/identity"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

func TestAPIKeys(t *testing.T) {
	t.Run("BE-INT-041_api_keys_persist_authenticate_and_report_into_their_project_only", func(t *testing.T) {
		s, ctx := fresh(t)
		require.NoError(t, s.Identity.Bootstrap(ctx, "admin", "correct horse"))
		admin, _ := s.Identity.Login(ctx, "admin", "correct horse")
		asAdmin := identity.WithUser(ctx, admin.User)
		chk, err := s.Catalog.CreateProject(ctx, catalog.CreateProjectInput{Key: "CHK", Name: "Checkout"})
		require.NoError(t, err)
		tc, err := s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: chk.ID, Title: "pay"})
		require.NoError(t, err)

		key, token, err := s.Identity.CreateAPIKey(asAdmin, chk.ID, "GitHub Actions")
		require.NoError(t, err)
		var stored []byte
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT token_sha256 FROM api_keys WHERE id = $1`, key.ID).Scan(&stored))
		assert.Equal(t, identity.TokenDigest(token), stored, "only the digest is stored")

		got, err := s.Identity.AuthenticateKey(ctx, token)
		require.NoError(t, err)
		asKey := identity.WithAPIKey(ctx, got)
		list, err := s.Identity.ListAPIKeys(asAdmin, chk.ID, pagination.Default())
		require.NoError(t, err)
		require.Equal(t, int64(1), list.Total)
		require.NotNil(t, list.Items[0].LastUsedAt, "a use is recorded")
		first := *list.Items[0].LastUsedAt
		_, err = s.Identity.AuthenticateKey(ctx, token)
		require.NoError(t, err)
		again, _ := s.Identity.ListAPIKeys(asAdmin, chk.ID, pagination.Default())
		assert.Equal(t, first, *again.Items[0].LastUsedAt, "at most one write a minute")

		// The key's runs go to its project (no ?project= needed) and correlate there.
		out, err := s.Ingestion.IngestJUnit(asKey, meta("1", 1), strings.NewReader(junitFor(`<testcase name="pay `+tc.Key()+`"/>`)))
		require.NoError(t, err)
		assert.Equal(t, chk.ID, out.Run.ProjectID)
		assert.Equal(t, 1, out.Persisted)
		m := meta("2", 1)
		m.ProjectKey = catalog.DefaultProjectKey
		_, err = s.Ingestion.IngestJUnit(asKey, m, strings.NewReader(junitFor()))
		assert.Equal(t, apperr.KindNotFound, kind(t, err), "another project is invisible to the key")

		// Revocation is immediate and final; keys are never deleted and never change otherwise.
		_, err = s.Identity.RevokeAPIKey(asAdmin, chk.ID, key.ID)
		require.NoError(t, err)
		_, err = s.Identity.AuthenticateKey(ctx, token)
		assert.Equal(t, apperr.KindUnauthorized, kind(t, err))
		_, err = s.Identity.RevokeAPIKey(asAdmin, chk.ID, key.ID)
		assert.Equal(t, apperr.KindConflict, kind(t, err))
		for _, stmt := range []string{
			`DELETE FROM api_keys`,
			`UPDATE api_keys SET name = 'renamed'`,
			`UPDATE api_keys SET project_id = 1`,
			`UPDATE api_keys SET revoked_at = NULL`,
			`INSERT INTO api_keys (project_id, name, prefix, token_sha256, created_by) VALUES (1, 'x', 'bad', '\x00', 1)`,
			`INSERT INTO api_keys (project_id, name, prefix, token_sha256, created_by) VALUES (1, '  ', 'pvk_0000000a', decode(repeat('ab', 32), 'hex'), 1)`,
		} {
			_, err := db.Pool.Exec(ctx, stmt)
			assert.Error(t, err, stmt)
		}
	})
}
