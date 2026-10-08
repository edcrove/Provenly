package identity

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

// member creates a non-admin user and returns a context signed in as them.
func member(ctx context.Context, t *testing.T, _ *Service, repo *fakeRepo, username string) (context.Context, User) {
	t.Helper()
	u, err := repo.CreateUser(ctx, NewUser{Username: username, DisplayName: username, PasswordHash: "$2a$x"})
	require.NoError(t, err)
	return WithUser(ctx, u), u
}

func TestGuard(t *testing.T) {
	s, repo, _, ctx := setup(t)
	a := admin(ctx, t, s)
	asAdmin := WithUser(ctx, a)
	asAna, ana := member(ctx, t, s, repo, "ana")
	require.NoError(t, repo.UpsertMember(ctx, 1, ana.ID, authz.RoleMember))
	gone := apperr.NotFound("gone")

	scope, err := s.Scope(asAdmin)
	require.NoError(t, err)
	assert.True(t, scope.All)
	scope, err = s.Scope(asAna)
	require.NoError(t, err)
	assert.Equal(t, map[int64]authz.Role{1: authz.RoleMember}, scope.Roles)

	assert.NoError(t, s.Require(asAdmin, 99, authz.RoleMaintainer, gone), "administrators: every project")
	assert.NoError(t, s.Require(asAna, 1, authz.RoleMember, gone))
	err = s.Require(asAna, 1, authz.RoleMaintainer, gone)
	assert.Equal(t, apperr.KindForbidden, kindOf(t, err))
	assert.Contains(t, err.Error(), "needs the maintainer role in the project (you are member)")
	assert.Equal(t, gone, s.Require(asAna, 2, authz.RoleViewer, gone), "no role: the caller's not-found error")

	actor, err := s.Actor(asAna)
	require.NoError(t, err)
	assert.Equal(t, authz.Actor{ID: ana.ID, Username: "ana"}, actor)
	_, err = s.Actor(WithAPIKey(ctx, APIKey{ProjectID: 1}))
	assert.Equal(t, apperr.KindUnauthorized, kindOf(t, err), "an API key is not a person")

	assert.NoError(t, s.RequireAdmin(asAdmin))
	assert.Equal(t, apperr.KindForbidden, kindOf(t, s.RequireAdmin(asAna)))
	// Other modules (audit, projects) get a message that does not name the users pages.
	forbidden, _ := apperr.As(s.RequireAdmin(asAna))
	assert.Equal(t, "only administrators can do this", forbidden.Message)

	// Without a signed-in user every check is "sign in".
	for _, err := range []error{s.RequireAdmin(ctx), s.Require(ctx, 1, authz.RoleViewer, gone)} {
		assert.Equal(t, apperr.KindUnauthorized, kindOf(t, err))
	}
	_, err = s.Scope(ctx)
	assert.Equal(t, apperr.KindUnauthorized, kindOf(t, err))

	repo.errs["ListUserMemberships"] = errBoom
	_, err = s.Scope(asAna)
	assert.ErrorIs(t, err, errBoom)
	repo.errs["MemberRole"] = errBoom
	assert.ErrorIs(t, s.Require(asAna, 1, authz.RoleViewer, gone), errBoom)
}

func TestMembers(t *testing.T) {
	s, repo, _, ctx := setup(t)
	a := admin(ctx, t, s)
	asAdmin := WithUser(ctx, a)
	asAna, ana := member(ctx, t, s, repo, "ana")
	_, bob := member(ctx, t, s, repo, "bob")

	m, err := s.SetMember(asAdmin, 1, " ANA ", "maintainer")
	require.NoError(t, err)
	assert.Equal(t, authz.RoleMaintainer, m.Role)
	assert.Equal(t, ana.ID, m.User.ID)
	_, err = s.SetMember(asAna, 1, "bob", "viewer")
	require.NoError(t, err, "maintainers manage their project's members")
	page, err := s.ListMembers(asAna, 1, pagination.Page{Number: 1, Size: 1})
	require.NoError(t, err)
	assert.Equal(t, int64(2), page.Total)
	assert.Equal(t, "ana", page.Items[0].User.Username)

	asBob := WithUser(ctx, bob)
	_, err = s.SetMember(asBob, 1, "bob", "maintainer")
	assert.Equal(t, apperr.KindForbidden, kindOf(t, err), "viewers cannot promote themselves")
	_, err = s.ListMembers(asBob, 2, pagination.Default())
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err), "another project is invisible")
	_, err = s.SetMember(asAdmin, 1, "nobody", "member")
	assert.Equal(t, apperr.KindNotFound, kindOf(t, err))
	_, err = s.SetMember(asAdmin, 1, "bob", "owner")
	e, _ := apperr.As(err)
	assert.Equal(t, []apperr.FieldError{{Field: "role", Message: "must be one of maintainer, member, viewer"}}, e.Fields)

	assert.Equal(t, apperr.KindForbidden, kindOf(t, s.RemoveMember(asBob, 1, "ana")))
	require.NoError(t, s.RemoveMember(asAna, 1, "bob"))
	assert.Equal(t, apperr.KindNotFound, kindOf(t, s.RemoveMember(asAna, 1, "bob")), "no longer a member")
	assert.Equal(t, apperr.KindNotFound, kindOf(t, s.RemoveMember(asAna, 1, "nobody")))

	for method, call := range map[string]func() error{
		"CountProjectMembers": func() error { _, err := s.ListMembers(asAdmin, 1, pagination.Default()); return err },
		"ListProjectMembers":  func() error { _, err := s.ListMembers(asAdmin, 1, pagination.Default()); return err },
		"UpsertMember":        func() error { _, err := s.SetMember(asAdmin, 1, "bob", "viewer"); return err },
		"DeleteMember":        func() error { return s.RemoveMember(asAdmin, 1, "ana") },
		"GetUserByUsername":   func() error { return s.RemoveMember(asAdmin, 1, "ana") },
	} {
		repo.errs[method] = errBoom
		assert.ErrorIs(t, call(), errBoom, method)
		delete(repo.errs, method)
	}
	assert.Equal(t, apperr.KindUnauthorized, kindOf(t, s.RemoveMember(ctx, 1, "ana")))
	_, err = s.ListMembers(ctx, 1, pagination.Default())
	assert.Equal(t, apperr.KindUnauthorized, kindOf(t, err))
	_, err = s.SetMember(ctx, 1, "ana", "viewer")
	assert.Equal(t, apperr.KindUnauthorized, kindOf(t, err))
}

func TestInvitationGrantsAProjectRole(t *testing.T) {
	s, repo, _, ctx := setup(t)
	a := admin(ctx, t, s)
	two := int64(2)
	inv, token, err := s.CreateInvitation(ctx, a, CreateInvitationInput{ProjectID: &two, Role: "member"})
	require.NoError(t, err)
	assert.Equal(t, authz.RoleMember, inv.ProjectRole)
	sess, err := s.AcceptInvitation(ctx, AcceptInput{Token: token, Username: "carla", DisplayName: "Carla", Password: "a long password"})
	require.NoError(t, err)
	role, _ := repo.MemberRole(ctx, 2, sess.User.ID)
	assert.Equal(t, authz.RoleMember, role)

	for _, in := range []CreateInvitationInput{{ProjectID: &two}, {ProjectID: &two, Role: "owner"}, {Role: "member"}} {
		_, _, err := s.CreateInvitation(ctx, a, in)
		e, _ := apperr.As(err)
		require.NotNil(t, e, "%+v", in)
		assert.Equal(t, "role", e.Fields[0].Field)
	}

	_, token, _ = s.CreateInvitation(ctx, a, CreateInvitationInput{ProjectID: &two, Role: "viewer"})
	repo.errs["UpsertMember"] = errBoom
	_, err = s.AcceptInvitation(ctx, AcceptInput{Token: token, Username: "dora", DisplayName: "Dora", Password: "a long password"})
	assert.ErrorIs(t, err, errBoom)
	_, err = repo.GetUserByUsername(ctx, "dora")
	assert.ErrorIs(t, err, ErrNotFound, "the account is not created without its role")
}

func TestMemberRoutes(t *testing.T) {
	h := newHarness(t)
	token, _ := h.login("admin", "correct horse")
	auth := bearer(token)
	_, err := h.svc.repo.CreateUser(context.Background(), NewUser{Username: "ana", DisplayName: "Ana", PasswordHash: "$2a$x"})
	require.NoError(t, err)

	rec := h.do("PUT", "/api/v1/projects/CHK/members/ana", `{"role":"member"}`, auth)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"role":"member"`)
	assert.Contains(t, rec.Body.String(), `"username":"ana"`)
	rec = h.do("GET", "/api/v1/projects/CHK/members", "", auth)
	assert.Contains(t, rec.Body.String(), `"totalItems":1`)
	assert.Equal(t, http.StatusNoContent, h.do("DELETE", "/api/v1/projects/CHK/members/ana", "", auth).Code)
	assert.Equal(t, http.StatusNotFound, h.do("DELETE", "/api/v1/projects/CHK/members/ana", "", auth).Code)

	for _, c := range []struct {
		method, target, body string
		want                 int
	}{
		{"GET", "/api/v1/projects/chk/members", "", 400},
		{"GET", "/api/v1/projects/NOPE/members", "", 404},
		{"GET", "/api/v1/projects/CHK/members?page=0", "", 400},
		{"PUT", "/api/v1/projects/NOPE/members/ana", `{"role":"member"}`, 404},
		{"PUT", "/api/v1/projects/CHK/members/ana", `{"role":"owner"}`, 400},
		{"PUT", "/api/v1/projects/CHK/members/ana", "", 415},
		{"DELETE", "/api/v1/projects/NOPE/members/ana", "", 404},
		{"PUT", "/api/v1/projects/CHK/members/%00", `{"role":"member"}`, 400},
		{"PUT", "/api/v1/projects/CHK/members/Ana", `{"role":"member"}`, 400},
		{"DELETE", "/api/v1/projects/CHK/members/%FF", "", 400},
	} {
		assert.Equal(t, c.want, h.do(c.method, c.target, c.body, auth).Code, c.method+" "+c.target)
	}

	// Invitations with a project role: the key is resolved; only administrators invite.
	rec = h.do("POST", "/api/v1/invitations", `{"project":"CHK","role":"viewer"}`, auth)
	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Contains(t, rec.Body.String(), `"projectId":2`)
	assert.Contains(t, rec.Body.String(), `"projectRole":"viewer"`)
	assert.Equal(t, http.StatusNotFound, h.do("POST", "/api/v1/invitations", `{"project":"NOPE","role":"viewer"}`, auth).Code)
	assert.Equal(t, http.StatusBadRequest, h.do("POST", "/api/v1/invitations", `{"project":"bad","role":"viewer"}`, auth).Code)
	h.svc.repo.(*fakeRepo).users[2] = User{ID: 2, Username: "ana", PasswordHash: h.svc.repo.(*fakeRepo).users[1].PasswordHash}
	anaToken, _ := h.login("ana", "correct horse")
	assert.Equal(t, http.StatusForbidden, h.do("POST", "/api/v1/invitations", `{"project":"NOPE","role":"viewer"}`, bearer(anaToken)).Code,
		"a non-admin learns nothing about project keys")
	assert.Equal(t, http.StatusNotFound, h.do("GET", "/api/v1/projects/CHK/members", "", bearer(anaToken)).Code, "not a member: invisible")
}
