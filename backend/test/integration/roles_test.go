//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/identity"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

func TestRoles(t *testing.T) {
	t.Run("BE-INT-039_project_roles_persist_and_authorize", func(t *testing.T) {
		s, ctx := fresh(t)
		require.NoError(t, s.Identity.Bootstrap(ctx, "admin", "correct horse"))
		admin, _ := s.Identity.Login(ctx, "admin", "correct horse")
		asAdmin := identity.WithUser(ctx, admin.User)
		chk, err := s.Catalog.CreateProject(ctx, catalog.CreateProjectInput{Key: "CHK", Name: "Checkout"})
		require.NoError(t, err)

		// An invitation grants a project role on accept, in the same transaction.
		inv, token, err := s.Identity.CreateInvitation(ctx, admin.User, identity.CreateInvitationInput{ProjectID: &chk.ID, Role: "member"})
		require.NoError(t, err)
		assert.Equal(t, authz.RoleMember, inv.ProjectRole)
		list, err := s.Identity.ListInvitations(ctx, admin.User, pagination.Default())
		require.NoError(t, err)
		assert.Equal(t, chk.ID, *list.Items[0].ProjectID)
		ana, err := s.Identity.AcceptInvitation(ctx, identity.AcceptInput{Token: token, Username: "ana", DisplayName: "Ana", Password: "ana's password"})
		require.NoError(t, err)
		asAna := identity.WithUser(ctx, ana.User)

		scope, err := s.Identity.Scope(asAna)
		require.NoError(t, err)
		assert.Equal(t, map[int64]authz.Role{chk.ID: authz.RoleMember}, scope.Roles)
		assert.NoError(t, s.Identity.Require(asAna, chk.ID, authz.RoleMember, nil))
		assert.Equal(t, apperr.KindForbidden, kind(t, s.Identity.Require(asAna, chk.ID, authz.RoleMaintainer, nil)))
		gone := apperr.NotFound("gone")
		assert.Equal(t, gone, s.Identity.Require(asAna, catalog.DefaultProjectID, authz.RoleViewer, gone))

		// Maintainers manage members; roles change in place; removal is final.
		_, err = s.Identity.SetMember(asAdmin, chk.ID, "ana", "maintainer")
		require.NoError(t, err)
		_, err = s.Identity.SetMember(asAna, chk.ID, "admin", "viewer")
		require.NoError(t, err)
		members, err := s.Identity.ListMembers(asAna, chk.ID, pagination.Page{Number: 1, Size: 10})
		require.NoError(t, err)
		require.Equal(t, int64(2), members.Total)
		assert.Equal(t, []string{"admin", "ana"}, []string{members.Items[0].User.Username, members.Items[1].User.Username})
		assert.Equal(t, authz.RoleMaintainer, members.Items[1].Role)
		require.NoError(t, s.Identity.RemoveMember(asAna, chk.ID, "admin"))
		assert.Equal(t, apperr.KindNotFound, kind(t, s.Identity.RemoveMember(asAna, chk.ID, "admin")))

		for _, stmt := range []string{
			`INSERT INTO project_members (project_id, user_id, role) VALUES (1, 1, 'owner')`,
			`INSERT INTO project_members (project_id, user_id, role) VALUES (999, 1, 'viewer')`,
			`UPDATE invitations SET project_role = NULL`,
		} {
			_, err := db.Pool.Exec(ctx, stmt)
			assert.Error(t, err, stmt)
		}
	})

	t.Run("BE-INT-040_lists_are_narrowed_to_the_visible_projects", func(t *testing.T) {
		s, ctx := fresh(t)
		chk, err := s.Catalog.CreateProject(ctx, catalog.CreateProjectInput{Key: "CHK", Name: "Checkout"})
		require.NoError(t, err)
		_, _ = s.Catalog.Create(ctx, catalog.CreateInput{Title: "default"})
		_, _ = s.Catalog.Create(ctx, catalog.CreateInput{ProjectID: chk.ID, Title: "checkout"})
		_, err = s.Ingestion.IngestJUnit(ctx, meta("1", 1), strings.NewReader(junitFor()))
		require.NoError(t, err)
		m := meta("2", 1)
		m.ProjectKey = "CHK"
		_, err = s.Ingestion.IngestJUnit(ctx, m, strings.NewReader(junitFor()))
		require.NoError(t, err)

		for _, c := range []struct {
			ids  []int64
			want int64
		}{{nil, 2}, {[]int64{chk.ID}, 1}, {[]int64{}, 0}} {
			cases, err := s.Catalog.List(ctx, catalog.ListFilter{ProjectIDs: c.ids}, pagination.Default())
			require.NoError(t, err)
			assert.Equal(t, c.want, cases.Total, "test cases %v", c.ids)
			runs, err := s.Execution.ListRuns(ctx, execution.RunFilter{ProjectIDs: c.ids}, pagination.Default())
			require.NoError(t, err)
			assert.Equal(t, c.want, runs.Total, "runs %v", c.ids)
			projects, err := s.Catalog.ListProjects(ctx, c.ids, nil, pagination.Default())
			require.NoError(t, err)
			assert.Equal(t, c.want, projects.Total, "projects %v", c.ids)
			assert.Len(t, projects.Items, int(c.want))
		}
	})
}
