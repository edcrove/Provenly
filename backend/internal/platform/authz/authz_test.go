package authz

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoles(t *testing.T) {
	assert.Less(t, RoleNone, RoleViewer)
	assert.Less(t, RoleViewer, RoleMember)
	assert.Less(t, RoleMember, RoleMaintainer)
	assert.Less(t, RoleMaintainer, RoleAdmin)
	for _, name := range MemberRoles {
		r, err := ParseMemberRole(name)
		require.NoError(t, err)
		assert.Equal(t, name, r.String())
	}
	for _, bad := range []string{"", "none", "admin", "owner", "Member"} {
		_, err := ParseMemberRole(bad)
		assert.Error(t, err, bad)
	}
}

func TestScope(t *testing.T) {
	all := Scope{All: true}
	assert.Equal(t, RoleAdmin, all.RoleIn(7))
	assert.Nil(t, all.ProjectIDs(), "administrators: no filter")

	some := Scope{Roles: map[int64]Role{1: RoleViewer, 2: RoleMaintainer}}
	assert.Equal(t, RoleMaintainer, some.RoleIn(2))
	assert.Equal(t, RoleNone, some.RoleIn(3))
	assert.ElementsMatch(t, []int64{1, 2}, some.ProjectIDs())
	none := Scope{}
	assert.NotNil(t, none.ProjectIDs(), "no memberships: an empty filter, never every project")
	assert.Empty(t, none.ProjectIDs())
}
