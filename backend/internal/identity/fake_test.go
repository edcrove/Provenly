package identity

import (
	"bytes"
	"context"
	"sort"
	"time"

	"github.com/edcrove/provenly/backend/internal/platform/authz"
)

// fakeRepo is an in-memory Repository; errs makes a method fail.
type fakeRepo struct {
	members     map[[2]int64]Member
	keys        map[int64]APIKey
	nextKey     int64
	users       map[int64]User
	invitations map[int64]Invitation
	nextUser    int64
	nextInv     int64
	errs        map[string]error
	now         func() time.Time
	// afterLock runs inside AcceptInvitation's transaction (to simulate a concurrent change).
	afterLock func()
	resets    map[int64]PasswordReset
	nextReset int64
}

func newFakeRepo(now func() time.Time) *fakeRepo {
	return &fakeRepo{keys: map[int64]APIKey{}, members: map[[2]int64]Member{}, users: map[int64]User{}, invitations: map[int64]Invitation{}, errs: map[string]error{}, now: now}
}

func (f *fakeRepo) fail(method string) error { return f.errs[method] }

func (f *fakeRepo) InTx(_ context.Context, fn func(Repository) error) error {
	if err := f.fail("InTx"); err != nil {
		return err
	}
	users, invs, members, resets := map[int64]User{}, map[int64]Invitation{}, map[[2]int64]Member{}, map[int64]PasswordReset{}
	for k, v := range f.resets {
		resets[k] = v
	}
	for k, v := range f.members {
		members[k] = v
	}
	for k, v := range f.users {
		users[k] = v
	}
	for k, v := range f.invitations {
		invs[k] = v
	}
	if err := fn(f); err != nil {
		f.users, f.invitations, f.members, f.resets = users, invs, members, resets
		return err
	}
	return nil
}

func (f *fakeRepo) CountUsers(context.Context) (int64, error) {
	if err := f.fail("CountUsers"); err != nil {
		return 0, err
	}
	return int64(len(f.users)), nil
}

func (f *fakeRepo) CreateUser(_ context.Context, u NewUser) (User, error) {
	if err := f.fail("CreateUser"); err != nil {
		return User{}, err
	}
	for _, x := range f.users {
		if x.Username == u.Username {
			return User{}, ErrConflict
		}
	}
	f.nextUser++
	user := User{ID: f.nextUser, Username: u.Username, DisplayName: u.DisplayName, Email: u.Email, PasswordHash: u.PasswordHash,
		IsAdmin: u.IsAdmin, CreatedAt: f.now(), UpdatedAt: f.now()}
	f.users[user.ID] = user
	return user, nil
}

func (f *fakeRepo) GetUser(_ context.Context, id int64) (User, error) {
	if err := f.fail("GetUser"); err != nil {
		return User{}, err
	}
	u, ok := f.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	return u, nil
}

func (f *fakeRepo) GetUserByUsername(_ context.Context, username string) (User, error) {
	if err := f.fail("GetUserByUsername"); err != nil {
		return User{}, err
	}
	for _, u := range f.users {
		if u.Username == username {
			return u, nil
		}
	}
	return User{}, ErrNotFound
}

func (f *fakeRepo) ListUsers(_ context.Context, limit, offset int32) ([]User, error) {
	if err := f.fail("ListUsers"); err != nil {
		return nil, err
	}
	var out []User
	for _, u := range f.users {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return window(out, limit, offset), nil
}

func window[T any](items []T, limit, offset int32) []T {
	if int(offset) >= len(items) {
		return []T{}
	}
	end := min(int(offset+limit), len(items))
	return items[offset:end]
}

func (f *fakeRepo) SetPasswordHash(_ context.Context, id int64, hash string) (User, error) {
	if err := f.fail("SetPasswordHash"); err != nil {
		return User{}, err
	}
	u := f.users[id]
	u.PasswordHash = hash
	f.users[id] = u
	return u, nil
}

func (f *fakeRepo) CreateInvitation(_ context.Context, in NewInvitation) (Invitation, error) {
	if err := f.fail("CreateInvitation"); err != nil {
		return Invitation{}, err
	}
	f.nextInv++
	inv := Invitation{ID: f.nextInv, TokenSHA256: in.TokenSHA256, Email: in.Email, Note: in.Note, CreatedBy: in.CreatedBy,
		CreatedAt: f.now(), ExpiresAt: in.ExpiresAt, ProjectID: in.ProjectID, ProjectRole: in.ProjectRole}
	f.invitations[inv.ID] = inv
	return inv, nil
}

func (f *fakeRepo) ListInvitations(_ context.Context, limit, offset int32) ([]Invitation, error) {
	if err := f.fail("ListInvitations"); err != nil {
		return nil, err
	}
	var out []Invitation
	for _, i := range f.invitations {
		out = append(out, i)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID > out[b].ID })
	return window(out, limit, offset), nil
}

func (f *fakeRepo) CountInvitations(context.Context) (int64, error) {
	if err := f.fail("CountInvitations"); err != nil {
		return 0, err
	}
	return int64(len(f.invitations)), nil
}

func (f *fakeRepo) GetInvitation(_ context.Context, id int64) (Invitation, error) {
	if err := f.fail("GetInvitation"); err != nil {
		return Invitation{}, err
	}
	i, ok := f.invitations[id]
	if !ok {
		return Invitation{}, ErrNotFound
	}
	return i, nil
}

func (f *fakeRepo) LockInvitationByToken(_ context.Context, digest []byte) (Invitation, error) {
	if err := f.fail("LockInvitationByToken"); err != nil {
		return Invitation{}, err
	}
	for _, i := range f.invitations {
		if bytes.Equal(i.TokenSHA256, digest) {
			if f.afterLock != nil {
				f.afterLock()
			}
			return i, nil
		}
	}
	return Invitation{}, ErrNotFound
}

func (f *fakeRepo) MarkInvitationAccepted(_ context.Context, id, userID int64) error {
	if err := f.fail("MarkInvitationAccepted"); err != nil {
		return err
	}
	i := f.invitations[id]
	at := f.now()
	i.AcceptedAt, i.AcceptedUserID = &at, &userID
	f.invitations[id] = i
	return nil
}

func (f *fakeRepo) RevokeInvitation(_ context.Context, id int64) (Invitation, error) {
	if err := f.fail("RevokeInvitation"); err != nil {
		return Invitation{}, err
	}
	i, ok := f.invitations[id]
	if !ok || i.AcceptedAt != nil || i.RevokedAt != nil {
		return Invitation{}, ErrNotFound
	}
	at := f.now()
	i.RevokedAt = &at
	f.invitations[id] = i
	return i, nil
}

func (f *fakeRepo) MemberRole(_ context.Context, projectID, userID int64) (authz.Role, error) {
	if err := f.fail("MemberRole"); err != nil {
		return authz.RoleNone, err
	}
	return f.members[[2]int64{projectID, userID}].Role, nil
}

func (f *fakeRepo) ListUserMemberships(_ context.Context, userID int64) (map[int64]authz.Role, error) {
	if err := f.fail("ListUserMemberships"); err != nil {
		return nil, err
	}
	out := map[int64]authz.Role{}
	for k, m := range f.members {
		if k[1] == userID {
			out[k[0]] = m.Role
		}
	}
	return out, nil
}

func (f *fakeRepo) ListProjectMembers(_ context.Context, projectID int64, limit, offset int32) ([]Member, error) {
	if err := f.fail("ListProjectMembers"); err != nil {
		return nil, err
	}
	var out []Member
	for k, m := range f.members {
		if k[0] == projectID {
			m.User = f.users[k[1]]
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].User.Username < out[j].User.Username })
	return window(out, limit, offset), nil
}

func (f *fakeRepo) CountProjectMembers(ctx context.Context, projectID int64) (int64, error) {
	if err := f.fail("CountProjectMembers"); err != nil {
		return 0, err
	}
	all, _ := f.ListProjectMembers(ctx, projectID, 1<<30, 0)
	return int64(len(all)), nil
}

func (f *fakeRepo) UpsertMember(_ context.Context, projectID, userID int64, role authz.Role) error {
	if err := f.fail("UpsertMember"); err != nil {
		return err
	}
	f.members[[2]int64{projectID, userID}] = Member{Role: role, Since: f.now()}
	return nil
}

func (f *fakeRepo) DeleteMember(_ context.Context, projectID, userID int64) (bool, error) {
	if err := f.fail("DeleteMember"); err != nil {
		return false, err
	}
	_, ok := f.members[[2]int64{projectID, userID}]
	delete(f.members, [2]int64{projectID, userID})
	return ok, nil
}

func (f *fakeRepo) CreateAPIKey(_ context.Context, k NewAPIKey) (APIKey, error) {
	if err := f.fail("CreateAPIKey"); err != nil {
		return APIKey{}, err
	}
	f.nextKey++
	key := APIKey{ID: f.nextKey, ProjectID: k.ProjectID, Name: k.Name, Prefix: k.Prefix, TokenSHA256: k.TokenSHA256, CreatedBy: k.CreatedBy, CreatedAt: f.now()}
	f.keys[key.ID] = key
	return key, nil
}

func (f *fakeRepo) ListAPIKeys(_ context.Context, projectID int64, limit, offset int32) ([]APIKey, error) {
	if err := f.fail("ListAPIKeys"); err != nil {
		return nil, err
	}
	var out []APIKey
	for _, k := range f.keys {
		if k.ProjectID == projectID {
			out = append(out, k)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return window(out, limit, offset), nil
}

func (f *fakeRepo) CountAPIKeys(ctx context.Context, projectID int64) (int64, error) {
	if err := f.fail("CountAPIKeys"); err != nil {
		return 0, err
	}
	all, _ := f.ListAPIKeys(ctx, projectID, 1<<30, 0)
	return int64(len(all)), nil
}

func (f *fakeRepo) GetAPIKey(_ context.Context, projectID, id int64) (APIKey, error) {
	if err := f.fail("GetAPIKey"); err != nil {
		return APIKey{}, err
	}
	k, ok := f.keys[id]
	if !ok || k.ProjectID != projectID {
		return APIKey{}, ErrNotFound
	}
	return k, nil
}

func (f *fakeRepo) GetAPIKeyByToken(_ context.Context, digest []byte) (APIKey, error) {
	if err := f.fail("GetAPIKeyByToken"); err != nil {
		return APIKey{}, err
	}
	for _, k := range f.keys {
		if bytes.Equal(k.TokenSHA256, digest) {
			return k, nil
		}
	}
	return APIKey{}, ErrNotFound
}

func (f *fakeRepo) RevokeAPIKey(ctx context.Context, projectID, id int64) (APIKey, error) {
	if err := f.fail("RevokeAPIKey"); err != nil {
		return APIKey{}, err
	}
	k, err := f.GetAPIKey(ctx, projectID, id)
	if err != nil || k.RevokedAt != nil {
		return APIKey{}, ErrNotFound
	}
	now := f.now()
	k.RevokedAt = &now
	f.keys[id] = k
	return k, nil
}

func (f *fakeRepo) TouchAPIKey(_ context.Context, id int64) error {
	if err := f.fail("TouchAPIKey"); err != nil {
		return err
	}
	k := f.keys[id]
	now := f.now()
	k.LastUsedAt = &now
	f.keys[id] = k
	return nil
}

func (f *fakeRepo) SetUserDeactivated(_ context.Context, id int64, at *time.Time) (User, error) {
	if err := f.fail("SetUserDeactivated"); err != nil {
		return User{}, err
	}
	u, ok := f.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	u.DeactivatedAt = at
	f.users[id] = u
	return u, nil
}

func (f *fakeRepo) CountActiveAdmins(context.Context) (int64, error) {
	if err := f.fail("CountActiveAdmins"); err != nil {
		return 0, err
	}
	n := int64(0)
	for _, u := range f.users {
		if u.IsAdmin && u.DeactivatedAt == nil {
			n++
		}
	}
	return n, nil
}

func (f *fakeRepo) VoidPasswordResets(_ context.Context, userID int64) error {
	if err := f.fail("VoidPasswordResets"); err != nil {
		return err
	}
	now := f.now()
	for id, r := range f.resets {
		if r.UserID == userID && r.UsedAt == nil {
			r.UsedAt = &now
			f.resets[id] = r
		}
	}
	return nil
}

func (f *fakeRepo) CreatePasswordReset(_ context.Context, r PasswordReset) (PasswordReset, error) {
	if err := f.fail("CreatePasswordReset"); err != nil {
		return PasswordReset{}, err
	}
	if f.resets == nil {
		f.resets = map[int64]PasswordReset{}
	}
	f.nextReset++
	r.ID, r.CreatedAt = f.nextReset, f.now()
	f.resets[r.ID] = r
	return r, nil
}

func (f *fakeRepo) LockPasswordResetByToken(_ context.Context, digest []byte) (PasswordReset, error) {
	if err := f.fail("LockPasswordResetByToken"); err != nil {
		return PasswordReset{}, err
	}
	for _, r := range f.resets {
		if bytes.Equal(r.TokenSHA256, digest) {
			return r, nil
		}
	}
	return PasswordReset{}, ErrNotFound
}

func (f *fakeRepo) MarkPasswordResetUsed(_ context.Context, id int64) error {
	if err := f.fail("MarkPasswordResetUsed"); err != nil {
		return err
	}
	r := f.resets[id]
	now := f.now()
	r.UsedAt = &now
	f.resets[id] = r
	return nil
}
