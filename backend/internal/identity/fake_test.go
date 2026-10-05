package identity

import (
	"bytes"
	"context"
	"sort"
	"time"
)

// fakeRepo is an in-memory Repository; errs makes a method fail.
type fakeRepo struct {
	users       map[int64]User
	invitations map[int64]Invitation
	nextUser    int64
	nextInv     int64
	errs        map[string]error
	now         func() time.Time
	// afterLock runs inside AcceptInvitation's transaction (to simulate a concurrent change).
	afterLock func()
}

func newFakeRepo(now func() time.Time) *fakeRepo {
	return &fakeRepo{users: map[int64]User{}, invitations: map[int64]Invitation{}, errs: map[string]error{}, now: now}
}

func (f *fakeRepo) fail(method string) error { return f.errs[method] }

func (f *fakeRepo) InTx(_ context.Context, fn func(Repository) error) error {
	if err := f.fail("InTx"); err != nil {
		return err
	}
	users, invs := map[int64]User{}, map[int64]Invitation{}
	for k, v := range f.users {
		users[k] = v
	}
	for k, v := range f.invitations {
		invs[k] = v
	}
	if err := fn(f); err != nil {
		f.users, f.invitations = users, invs
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
		CreatedAt: f.now(), ExpiresAt: in.ExpiresAt}
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
