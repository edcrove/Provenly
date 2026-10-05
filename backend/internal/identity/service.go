package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

// Limits and formats of the identity module.
const (
	MinPasswordBytes = 10
	// MaxPasswordBytes is bcrypt's input limit: longer passwords would be silently truncated.
	MaxPasswordBytes = 72
	maxDisplayName   = 100
	maxEmail         = 254
	maxNote          = 200
	issuer           = "provenly"
)

var (
	// UsernamePattern matches 3 to 32 lower-case letters, digits, dots, dashes or underscores, starting with a letter or digit.
	UsernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,31}$`)
	emailPattern    = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	// errBadCredentials is the one answer to a failed sign-in: it never says whether the username exists.
	errBadCredentials = apperr.Unauthorized("invalid username or password")
	errSignIn         = apperr.Unauthorized("sign in to continue")
)

// UsernameMessage is the validation message of a malformed username.
const UsernameMessage = "must be 3 to 32 lower-case letters, digits, '.', '-' or '_', starting with a letter or digit"

// Config holds the secrets and lifetimes of the identity module.
type Config struct {
	// Secret signs session tokens (HS256); at least 32 bytes.
	Secret []byte
	// SessionTTL is how long a session token is valid.
	SessionTTL time.Duration
	// InvitationTTL is how long an invitation link can be used.
	InvitationTTL time.Duration
	// BcryptCost is the password hashing cost.
	BcryptCost int
}

// DefaultConfig returns production lifetimes with the given secret.
func DefaultConfig(secret []byte) Config {
	return Config{Secret: secret, SessionTTL: 12 * time.Hour, InvitationTTL: 7 * 24 * time.Hour, BcryptCost: 12}
}

// RandomSecret returns a fresh 32-byte signing secret (sessions do not survive a restart).
func RandomSecret() []byte {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return b
}

// Session is the result of a successful sign-in.
type Session struct {
	Token     string
	ExpiresAt time.Time
	User      User
}

// CreateInvitationInput is what an admin provides for a new invitation.
type CreateInvitationInput struct {
	Email *string
	Note  string
}

// AcceptInput is what the invited person provides.
type AcceptInput struct {
	Token       string
	Username    string
	DisplayName string
	Email       *string
	Password    string
}

// Service implements the identity use cases.
type Service struct {
	repo  Repository
	now   func() time.Time
	cfg   Config
	dummy []byte
}

// NewService builds the identity service.
func NewService(repo Repository, now func() time.Time, cfg Config) *Service {
	// A hash to compare against when the username does not exist, so a failed sign-in takes the same time.
	dummy, _ := bcrypt.GenerateFromPassword([]byte("provenly-timing-equalizer"), cfg.BcryptCost)
	return &Service{repo: repo, now: now, cfg: cfg, dummy: dummy}
}

// Bootstrap creates the first administrator when there are no users yet; afterwards it does nothing.
func (s *Service) Bootstrap(ctx context.Context, username, password string) error {
	n, err := s.repo.CountUsers(ctx)
	if err != nil || n > 0 {
		return err
	}
	username = normalizeUsername(username)
	var v apperr.Validator
	validateUsername(&v, username)
	validatePassword(&v, "password", password)
	if err := v.Err(); err != nil {
		e, _ := apperr.As(err)
		return fmt.Errorf("bootstrap admin (PROVENLY_ADMIN_USERNAME / PROVENLY_ADMIN_PASSWORD): %s %s", e.Fields[0].Field, e.Fields[0].Message)
	}
	hash, err := s.hash(password)
	if err != nil {
		return err
	}
	_, err = s.repo.CreateUser(ctx, NewUser{Username: username, DisplayName: username, PasswordHash: hash, IsAdmin: true})
	if errors.Is(err, ErrConflict) {
		return nil // another instance bootstrapped it first
	}
	return err
}

// Login checks a username and password and issues a session.
func (s *Service) Login(ctx context.Context, username, password string) (Session, error) {
	username = normalizeUsername(username)
	var u User
	err := ErrNotFound
	// A username no account can have (NUL, invalid UTF-8, too long) is not looked up.
	if UsernamePattern.MatchString(username) {
		u, err = s.repo.GetUserByUsername(ctx, username)
	}
	if errors.Is(err, ErrNotFound) {
		_ = bcrypt.CompareHashAndPassword(s.dummy, []byte(password))
		return Session{}, errBadCredentials
	}
	if err != nil {
		return Session{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return Session{}, errBadCredentials
	}
	return s.issue(u)
}

type claims struct {
	// PasswordVersion changes with the password, so changing it signs every other session out.
	PasswordVersion string `json:"pv"`
	jwt.RegisteredClaims
}

func passwordVersion(hash string) string {
	sum := sha256.Sum256([]byte(hash))
	return hex.EncodeToString(sum[:8])
}

func (s *Service) issue(u User) (Session, error) {
	now := s.now()
	exp := now.Add(s.cfg.SessionTTL)
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		PasswordVersion: passwordVersion(u.PasswordHash),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: issuer, Subject: strconv.FormatInt(u.ID, 10),
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(exp),
		},
	}).SignedString(s.cfg.Secret)
	return Session{Token: token, ExpiresAt: exp, User: u}, err
}

// Authenticate returns the user of a valid, unexpired session token.
func (s *Service) Authenticate(ctx context.Context, token string) (User, error) {
	var c claims
	_, err := jwt.ParseWithClaims(token, &c, func(*jwt.Token) (any, error) { return s.cfg.Secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer(issuer),
		jwt.WithExpirationRequired(), jwt.WithTimeFunc(s.now))
	if err != nil {
		return User{}, errSignIn
	}
	id, err := strconv.ParseInt(c.Subject, 10, 64)
	if err != nil {
		return User{}, errSignIn
	}
	u, err := s.repo.GetUser(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return User{}, errSignIn
	}
	if err != nil {
		return User{}, err
	}
	if c.PasswordVersion != passwordVersion(u.PasswordHash) {
		return User{}, errSignIn
	}
	return u, nil
}

// ChangePassword replaces the user's password after checking the current one; it returns a new session.
func (s *Service) ChangePassword(ctx context.Context, u User, current, next string) (Session, error) {
	var v apperr.Validator
	validatePassword(&v, "newPassword", next)
	if err := v.Err(); err != nil {
		return Session{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(current)) != nil {
		return Session{}, apperr.Validation(apperr.ValidationFailed, apperr.FieldError{Field: "currentPassword", Message: "is not your current password"})
	}
	hash, err := s.hash(next)
	if err != nil {
		return Session{}, err
	}
	updated, err := s.repo.SetPasswordHash(ctx, u.ID, hash)
	if err != nil {
		return Session{}, err
	}
	return s.issue(updated)
}

func requireAdmin(u User) error {
	if !u.IsAdmin {
		return apperr.Forbidden("only administrators can manage users and invitations")
	}
	return nil
}

// ListUsers returns a page of users by username (administrators only).
func (s *Service) ListUsers(ctx context.Context, actor User, page pagination.Page) (pagination.Result[User], error) {
	if err := requireAdmin(actor); err != nil {
		return pagination.Result[User]{}, err
	}
	n, err := s.repo.CountUsers(ctx)
	if err != nil {
		return pagination.Result[User]{}, err
	}
	items, err := s.repo.ListUsers(ctx, page.Limit(), page.Offset())
	if err != nil {
		return pagination.Result[User]{}, err
	}
	return pagination.Result[User]{Items: items, Page: page, Total: n}, nil
}

// TokenDigest is the stored form of an invitation token.
func TokenDigest(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// CreateInvitation creates an invitation and returns its token, which is never stored nor shown again.
func (s *Service) CreateInvitation(ctx context.Context, actor User, in CreateInvitationInput) (Invitation, string, error) {
	if err := requireAdmin(actor); err != nil {
		return Invitation{}, "", err
	}
	in.Note = strings.TrimSpace(in.Note)
	var v apperr.Validator
	email := validateEmail(&v, in.Email)
	v.Check(utf8.RuneCountInString(in.Note) <= maxNote, "note", fmt.Sprintf("must be at most %d characters", maxNote))
	v.CheckText("note", in.Note)
	if err := v.Err(); err != nil {
		return Invitation{}, "", err
	}
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	token := base64.RawURLEncoding.EncodeToString(raw)
	inv, err := s.repo.CreateInvitation(ctx, NewInvitation{
		TokenSHA256: TokenDigest(token), Email: email, Note: in.Note, CreatedBy: actor.ID,
		ExpiresAt: s.now().Add(s.cfg.InvitationTTL),
	})
	if err != nil {
		return Invitation{}, "", err
	}
	return inv, token, nil
}

// ListInvitations returns a page of invitations, newest first (administrators only).
func (s *Service) ListInvitations(ctx context.Context, actor User, page pagination.Page) (pagination.Result[Invitation], error) {
	if err := requireAdmin(actor); err != nil {
		return pagination.Result[Invitation]{}, err
	}
	n, err := s.repo.CountInvitations(ctx)
	if err != nil {
		return pagination.Result[Invitation]{}, err
	}
	items, err := s.repo.ListInvitations(ctx, page.Limit(), page.Offset())
	if err != nil {
		return pagination.Result[Invitation]{}, err
	}
	return pagination.Result[Invitation]{Items: items, Page: page, Total: n}, nil
}

// RevokeInvitation makes a pending invitation unusable (administrators only).
func (s *Service) RevokeInvitation(ctx context.Context, actor User, id int64) (Invitation, error) {
	if err := requireAdmin(actor); err != nil {
		return Invitation{}, err
	}
	inv, err := s.repo.RevokeInvitation(ctx, id)
	if !errors.Is(err, ErrNotFound) {
		return inv, err
	}
	if _, err := s.repo.GetInvitation(ctx, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			return Invitation{}, apperr.NotFound("invitation %d not found", id)
		}
		return Invitation{}, err
	}
	return Invitation{}, apperr.Conflict("invitation %d was already accepted or revoked", id)
}

// AcceptInvitation creates the invited user's account and signs them in.
func (s *Service) AcceptInvitation(ctx context.Context, in AcceptInput) (Session, error) {
	in.Username = normalizeUsername(in.Username)
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	var v apperr.Validator
	v.Check(in.Token != "", "token", "is required")
	validateUsername(&v, in.Username)
	v.Check(in.DisplayName != "", "displayName", "is required")
	v.Check(utf8.RuneCountInString(in.DisplayName) <= maxDisplayName, "displayName", fmt.Sprintf("must be at most %d characters", maxDisplayName))
	v.CheckText("displayName", in.DisplayName)
	email := validateEmail(&v, in.Email)
	validatePassword(&v, "password", in.Password)
	if err := v.Err(); err != nil {
		return Session{}, err
	}
	hash, err := s.hash(in.Password)
	if err != nil {
		return Session{}, err
	}
	var user User
	err = s.repo.InTx(ctx, func(r Repository) error {
		inv, err := r.LockInvitationByToken(ctx, TokenDigest(in.Token))
		if errors.Is(err, ErrNotFound) || (err == nil && inv.Status(s.now()) != InvitationPending) {
			return apperr.NotFound("invitation not found, expired, revoked or already used")
		}
		if err != nil {
			return err
		}
		if inv.Email != nil && email == nil {
			email = inv.Email
		}
		user, err = r.CreateUser(ctx, NewUser{Username: in.Username, DisplayName: in.DisplayName, Email: email, PasswordHash: hash})
		if errors.Is(err, ErrConflict) {
			return apperr.Conflict("username %s is taken", in.Username)
		}
		if err != nil {
			return err
		}
		return r.MarkInvitationAccepted(ctx, inv.ID, user.ID)
	})
	if err != nil {
		return Session{}, err
	}
	return s.issue(user)
}

func (s *Service) hash(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), s.cfg.BcryptCost)
	return string(h), err
}

func normalizeUsername(u string) string { return strings.ToLower(strings.TrimSpace(u)) }

func validateUsername(v *apperr.Validator, username string) {
	v.Check(UsernamePattern.MatchString(username), "username", UsernameMessage)
}

func validatePassword(v *apperr.Validator, field, password string) {
	v.Check(len(password) >= MinPasswordBytes, field, fmt.Sprintf("must be at least %d characters", MinPasswordBytes))
	v.Check(len(password) <= MaxPasswordBytes, field, fmt.Sprintf("must be at most %d bytes", MaxPasswordBytes))
	v.CheckText(field, password)
}

// validateEmail trims an optional email; empty means none.
func validateEmail(v *apperr.Validator, email *string) *string {
	if email == nil {
		return nil
	}
	e := strings.TrimSpace(*email)
	if e == "" {
		return nil
	}
	v.Check(len(e) <= maxEmail && emailPattern.MatchString(e), "email", "must be an email address")
	v.CheckText("email", e)
	return &e
}
