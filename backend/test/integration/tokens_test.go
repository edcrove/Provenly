//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/app"
	"github.com/edcrove/provenly/backend/internal/audit"
	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/identity"
	identitypg "github.com/edcrove/provenly/backend/internal/identity/postgres"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

func TestPersonalAccessTokens(t *testing.T) {
	t.Run("BE-INT-073_personal_access_tokens_read_their_projects_only_and_stop_when_revoked_expired_or_deactivated", func(t *testing.T) {
		s, ctx := fresh(t)
		require.NoError(t, s.Identity.Bootstrap(ctx, "admin", "correct horse"))
		admin, err := s.Identity.Login(ctx, "admin", "correct horse")
		require.NoError(t, err)
		asAdmin := identity.WithUser(ctx, admin.User)
		chk, err := s.Catalog.CreateProject(asAdmin, catalog.CreateProjectInput{Key: "CHK", Name: "Checkout"})
		require.NoError(t, err)
		shop, err := s.Catalog.CreateProject(asAdmin, catalog.CreateProjectInput{Key: "SHOP", Name: "Shop"})
		require.NoError(t, err)
		_, err = s.Catalog.Create(asAdmin, catalog.CreateInput{ProjectID: chk.ID, Title: "pay by card"})
		require.NoError(t, err)
		_, err = s.Catalog.Create(asAdmin, catalog.CreateInput{ProjectID: shop.ID, Title: "browse the shop"})
		require.NoError(t, err)
		_, inv, err := s.Identity.CreateInvitation(ctx, admin.User, identity.CreateInvitationInput{})
		require.NoError(t, err)
		ana, err := s.Identity.AcceptInvitation(ctx, identity.AcceptInput{Token: inv, Username: "ana", DisplayName: "Ana", Password: "ana password"})
		require.NoError(t, err)
		for _, p := range []int64{chk.ID, shop.ID} {
			_, err = s.Identity.SetMember(asAdmin, p, "ana", authz.RoleMaintainer.String())
			require.NoError(t, err)
		}

		srv := httptest.NewServer(app.NewHandler(s, 1<<20))
		defer srv.Close()
		call := func(token, method, path, body string) (int, string) {
			req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+token)
			if body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			res, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer res.Body.Close()
			out, _ := io.ReadAll(res.Body)
			return res.StatusCode, string(out)
		}

		// Ana makes a token over CHK only, with her session.
		code, body := call(ana.Token, "POST", "/api/v1/auth/tokens", `{"name":"MCP","projects":["CHK"]}`)
		require.Equal(t, http.StatusCreated, code, body)
		var created struct {
			PersonalAccessToken struct{ ID int64 }
			Token               string
		}
		require.NoError(t, json.Unmarshal([]byte(body), &created))
		pat := created.Token
		var stored []byte
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT token_sha256 FROM personal_access_tokens WHERE id = $1`, created.PersonalAccessToken.ID).Scan(&stored))
		assert.Equal(t, identity.TokenDigest(pat), stored, "only the digest is stored")

		// It reads CHK, as Ana, and nothing else.
		code, body = call(pat, "GET", "/api/v1/test-cases", "")
		require.Equal(t, http.StatusOK, code, body)
		assert.Contains(t, body, "pay by card")
		assert.NotContains(t, body, "browse the shop", "a list shows only the token's projects")
		code, _ = call(pat, "GET", "/api/v1/projects/SHOP/requirements", "")
		assert.Equal(t, http.StatusForbidden, code, "another of her projects")
		code, _ = call(pat, "GET", "/api/v1/test-cases?project=SHOP", "")
		assert.Equal(t, http.StatusForbidden, code)
		code, body = call(pat, "GET", "/api/v1/projects", "")
		assert.Equal(t, http.StatusOK, code)
		assert.Contains(t, body, `"totalItems":1`)
		code, body = call(pat, "POST", "/api/v1/test-cases", `{"title":"x","project":"CHK"}`)
		assert.Equal(t, http.StatusForbidden, code, "a token never writes, even where she could")
		assert.Contains(t, body, "read-only")
		code, _ = call(pat, "POST", "/api/v1/auth/tokens", `{"name":"x","projects":["CHK"]}`)
		assert.Equal(t, http.StatusForbidden, code, "nor makes tokens")

		// MCP works with the token and sees the same.
		code, body = call(pat, "POST", "/api/v1/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_test_cases","arguments":{}}}`)
		require.Equal(t, http.StatusOK, code, body)
		assert.Contains(t, body, "pay by card")
		assert.NotContains(t, body, "browse the shop")

		// An administrator's token does not administer.
		code, body = call(admin.Token, "POST", "/api/v1/auth/tokens", `{"name":"admin","projects":["SHOP"],"expiresInDays":365}`)
		require.Equal(t, http.StatusCreated, code, body)
		var adminTok struct{ Token string }
		require.NoError(t, json.Unmarshal([]byte(body), &adminTok))
		for _, path := range []string{"/api/v1/users", "/api/v1/invitations", "/api/v1/audit"} {
			code, _ = call(adminTok.Token, "GET", path, "")
			assert.Equal(t, http.StatusForbidden, code, path)
		}

		// Revoked: 401 at once. Expired: 401. A token past 365 days cannot exist.
		code, body = call(ana.Token, "GET", "/api/v1/auth/tokens", "")
		require.Equal(t, http.StatusOK, code)
		assert.Contains(t, body, `"projects":["CHK"]`)
		assert.Contains(t, body, `"totalItems":1`, "only her own tokens")
		assert.Regexp(t, `"lastUsedAt":"`, body, "its use is recorded")
		code, _ = call(ana.Token, "POST", "/api/v1/auth/tokens/1/revoke", "")
		require.Equal(t, http.StatusOK, code)
		code, _ = call(ana.Token, "POST", "/api/v1/auth/tokens/1/revoke", "")
		assert.Equal(t, http.StatusConflict, code, "revoked once")
		code, _ = call(admin.Token, "POST", "/api/v1/auth/tokens/1/revoke", "")
		assert.Equal(t, http.StatusNotFound, code, "nobody revokes another person's token")
		code, _ = call(pat, "GET", "/api/v1/test-cases", "")
		assert.Equal(t, http.StatusUnauthorized, code)
		old := "pvly_pat_0000000a_" + strings.Repeat("A", 43)
		_, err = db.Pool.Exec(ctx, `INSERT INTO personal_access_tokens (user_id, name, prefix, token_sha256, created_at, expires_at)
			VALUES ($1, 'old', 'pvly_pat_0000000a', $2, now() - interval '2 days', now() - interval '1 day')`, ana.User.ID, identity.TokenDigest(old))
		require.NoError(t, err)
		code, _ = call(old, "GET", "/api/v1/auth/me", "")
		assert.Equal(t, http.StatusUnauthorized, code, "expired")

		// Deactivating Ana revokes the tokens she still has.
		code, body = call(ana.Token, "POST", "/api/v1/auth/tokens", `{"name":"again","projects":["CHK","SHOP"]}`)
		require.Equal(t, http.StatusCreated, code, body)
		var again struct{ Token string }
		require.NoError(t, json.Unmarshal([]byte(body), &again))
		code, _ = call(again.Token, "GET", "/api/v1/projects/SHOP/requirements", "")
		require.Equal(t, http.StatusOK, code)
		_, err = s.Identity.Deactivate(ctx, admin.User, "ana")
		require.NoError(t, err)
		var live int
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM personal_access_tokens WHERE user_id = $1 AND revoked_at IS NULL`, ana.User.ID).Scan(&live))
		assert.Zero(t, live)
		code, _ = call(again.Token, "GET", "/api/v1/auth/me", "")
		assert.Equal(t, http.StatusUnauthorized, code)

		// The creation and revocation are audited; reads are not.
		events, err := s.Audit.Events(asAdmin, audit.Filter{Actor: "ana"}, pagination.Page{Number: 1, Size: 100})
		require.NoError(t, err)
		var summaries []string
		for _, e := range events.Items {
			summaries = append(summaries, e.Summary)
		}
		assert.Contains(t, summaries, "created a personal access token")
		assert.Contains(t, summaries, "revoked personal access token #1")

		// A token over a project that does not exist is refused by the database (its projects are real ones).
		store := identitypg.NewStore(db.Pool)
		err = store.InTx(ctx, func(r identity.Repository) error {
			_, err := r.CreateToken(ctx, identity.NewPersonalAccessToken{UserID: ana.User.ID, Name: "ghost", Prefix: "pvly_pat_0000000c",
				TokenSHA256: identity.TokenDigest("ghost"), CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour), ProjectIDs: []int64{999999}})
			return err
		})
		assert.Error(t, err)

		// The database keeps tokens: never deleted, never changed but for their use and one revocation.
		for _, stmt := range []string{
			`DELETE FROM personal_access_tokens`,
			`UPDATE personal_access_tokens SET name = 'renamed'`,
			`UPDATE personal_access_tokens SET expires_at = expires_at + interval '1 day'`,
			`UPDATE personal_access_tokens SET revoked_at = NULL WHERE revoked_at IS NOT NULL`,
			`DELETE FROM personal_access_token_projects`,
			`UPDATE personal_access_token_projects SET project_id = 1`,
			`INSERT INTO personal_access_tokens (user_id, name, prefix, token_sha256, expires_at) VALUES (1, 'x', 'pvly_pat_0000000b', decode(repeat('ab', 32), 'hex'), now() + interval '366 days')`,
			`INSERT INTO personal_access_tokens (user_id, name, prefix, token_sha256, expires_at) VALUES (1, 'x', 'pvk_0000000b', decode(repeat('cd', 32), 'hex'), now() + interval '1 day')`,
		} {
			_, err := db.Pool.Exec(context.Background(), stmt)
			assert.Error(t, err, stmt)
		}
	})
}
