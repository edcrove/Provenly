package identity

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/httpx"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
	"github.com/edcrove/provenly/backend/internal/platform/projectkey"
)

// CookieName is the browser session cookie (HttpOnly, SameSite=Strict).
const CookieName = "provenly_session"

// API is what the REST adapter needs from the identity module.
type API interface {
	Login(ctx context.Context, username, password string) (Session, error)
	Authenticate(ctx context.Context, token string) (User, error)
	ChangePassword(ctx context.Context, u User, current, next string) (Session, error)
	ListUsers(ctx context.Context, actor User, page pagination.Page) (pagination.Result[User], error)
	CreateInvitation(ctx context.Context, actor User, in CreateInvitationInput) (Invitation, string, error)
	ListInvitations(ctx context.Context, actor User, page pagination.Page) (pagination.Result[Invitation], error)
	RevokeInvitation(ctx context.Context, actor User, id int64) (Invitation, error)
	AcceptInvitation(ctx context.Context, in AcceptInput) (Session, error)
	ListMembers(ctx context.Context, projectID int64, page pagination.Page) (pagination.Result[Member], error)
	SetMember(ctx context.Context, projectID int64, username, role string) (Member, error)
	RemoveMember(ctx context.Context, projectID int64, username string) error
	CreateAPIKey(ctx context.Context, projectID int64, name string) (APIKey, string, error)
	ListAPIKeys(ctx context.Context, projectID int64, page pagination.Page) (pagination.Result[APIKey], error)
	RevokeAPIKey(ctx context.Context, projectID, id int64) (APIKey, error)
	AuthenticateKey(ctx context.Context, token string) (APIKey, error)
	Deactivate(ctx context.Context, actor User, username string) (User, error)
	Reactivate(ctx context.Context, actor User, username string) (User, error)
	CreatePasswordReset(ctx context.Context, actor User, username string) (PasswordReset, string, error)
	ResetPassword(ctx context.Context, token, password string) (Session, error)
}

// Projects resolves project keys (the catalog module's public interface).
type Projects interface {
	ProjectIDByKey(ctx context.Context, key string) (int64, error)
}

type memberDTO struct {
	User  UserDTO   `json:"user"`
	Role  string    `json:"role"`
	Since time.Time `json:"since"`
}

func toMemberDTO(m Member) memberDTO {
	return memberDTO{User: ToUserDTO(m.User), Role: m.Role.String(), Since: m.Since}
}

type apiKeyDTO struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Status     string     `json:"status"`
	CreatedBy  int64      `json:"createdBy"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	RevokedAt  *time.Time `json:"revokedAt"`
}

func toAPIKeyDTO(k APIKey) apiKeyDTO {
	status := "active"
	if k.RevokedAt != nil {
		status = "revoked"
	}
	return apiKeyDTO{
		ID: k.ID, Name: k.Name, Prefix: k.Prefix, Status: status, CreatedBy: k.CreatedBy, CreatedAt: k.CreatedAt,
		LastUsedAt: k.LastUsedAt, RevokedAt: k.RevokedAt,
	}
}

type createdAPIKeyDTO struct {
	APIKey apiKeyDTO `json:"apiKey"`
	Token  string    `json:"token"`
}

type apiKeyRequest struct {
	Name string `json:"name"`
}

type memberRequest struct {
	Role string `json:"role"`
}

// UserDTO is the wire form of User (never the password hash).
type UserDTO struct {
	ID          int64     `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	Email       *string   `json:"email"`
	IsAdmin     bool      `json:"isAdmin"`
	CreatedAt   time.Time `json:"createdAt"`
	// DeactivatedAt is when an administrator deactivated the user (null: active).
	DeactivatedAt *time.Time `json:"deactivatedAt"`
}

// ToUserDTO converts a User to its wire form.
func ToUserDTO(u User) UserDTO {
	return UserDTO{ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, Email: u.Email, IsAdmin: u.IsAdmin, CreatedAt: u.CreatedAt,
		DeactivatedAt: u.DeactivatedAt}
}

type sessionDTO struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
	User      UserDTO   `json:"user"`
}

type invitationDTO struct {
	ID             int64            `json:"id"`
	Email          *string          `json:"email"`
	Note           string           `json:"note"`
	Status         InvitationStatus `json:"status"`
	CreatedBy      int64            `json:"createdBy"`
	CreatedAt      time.Time        `json:"createdAt"`
	ExpiresAt      time.Time        `json:"expiresAt"`
	AcceptedAt     *time.Time       `json:"acceptedAt"`
	AcceptedUserID *int64           `json:"acceptedUserId"`
	RevokedAt      *time.Time       `json:"revokedAt"`
	ProjectID      *int64           `json:"projectId"`
	ProjectRole    *string          `json:"projectRole"`
}

type createdInvitationDTO struct {
	Invitation invitationDTO `json:"invitation"`
	Token      string        `json:"token"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type passwordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

type invitationRequest struct {
	Email   *string `json:"email"`
	Note    string  `json:"note"`
	Project string  `json:"project"`
	Role    string  `json:"role"`
}

type acceptRequest struct {
	Token       string  `json:"token"`
	Username    string  `json:"username"`
	DisplayName string  `json:"displayName"`
	Email       *string `json:"email"`
	Password    string  `json:"password"`
}

// Handler is the REST adapter of the identity module.
type Handler struct {
	api      API
	projects Projects
	now      func() time.Time
}

// NewHandler builds a Handler.
func NewHandler(api API, projects Projects, now func() time.Time) *Handler {
	return &Handler{api: api, projects: projects, now: now}
}

// RegisterPublic mounts the routes that work without a session.
func (h *Handler) RegisterPublic(mux httpx.Router) {
	mux.HandleFunc("POST /api/v1/auth/login", h.login)
	mux.HandleFunc("POST /api/v1/auth/logout", h.logout)
	mux.HandleFunc("POST /api/v1/invitations/accept", h.accept)
	mux.HandleFunc("POST /api/v1/password-reset", h.resetPassword)
}

// RegisterProtected mounts the routes that need a session (wrap mux with Protect).
func (h *Handler) RegisterProtected(mux httpx.Router) {
	mux.HandleFunc("GET /api/v1/auth/me", h.me)
	mux.HandleFunc("POST /api/v1/auth/password", h.changePassword)
	mux.HandleFunc("GET /api/v1/users", h.listUsers)
	mux.HandleFunc("POST /api/v1/users/{username}/deactivate", h.deactivate)
	mux.HandleFunc("POST /api/v1/users/{username}/reactivate", h.reactivate)
	mux.HandleFunc("POST /api/v1/users/{username}/password-reset", h.createPasswordReset)
	mux.HandleFunc("GET /api/v1/invitations", h.listInvitations)
	mux.HandleFunc("POST /api/v1/invitations", h.createInvitation)
	mux.HandleFunc("POST /api/v1/invitations/{invitationId}/revoke", h.revokeInvitation)
	mux.HandleFunc("GET /api/v1/projects/{projectKey}/members", h.listMembers)
	mux.HandleFunc("PUT /api/v1/projects/{projectKey}/members/{username}", h.setMember)
	mux.HandleFunc("DELETE /api/v1/projects/{projectKey}/members/{username}", h.removeMember)
	mux.HandleFunc("GET /api/v1/projects/{projectKey}/api-keys", h.listAPIKeys)
	mux.HandleFunc("POST /api/v1/projects/{projectKey}/api-keys", h.createAPIKey)
	mux.HandleFunc("POST /api/v1/projects/{projectKey}/api-keys/{apiKeyId}/revoke", h.revokeAPIKey)
}

// protected is a Router whose routes all require a session (or, with keys, an API key).
type protected struct {
	next httpx.Router
	api  API
	keys bool
}

// Protect returns a Router that wraps every route it registers with RequireUser.
func Protect(r httpx.Router, api API) httpx.Router { return protected{next: r, api: api} }

// ProtectWithKeys returns a Router whose routes accept a session or a project API key
// (Authorization: Bearer pvk_...): the routes CI calls.
func ProtectWithKeys(r httpx.Router, api API) httpx.Router {
	return protected{next: r, api: api, keys: true}
}

func (p protected) HandleFunc(pattern string, h func(http.ResponseWriter, *http.Request)) {
	if p.keys {
		p.next.HandleFunc(pattern, RequireUserOrKey(p.api, h))
		return
	}
	p.next.HandleFunc(pattern, RequireUser(p.api, h))
}

// RequireUserOrKey accepts a project API key (put in the context) or, like RequireUser, a session.
// Keys come only in the Authorization header: a cookie is a browser session.
func RequireUserOrKey(api API, next func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	withUser := RequireUser(api, next)
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if !IsAPIKey(token) {
			withUser(w, r)
			return
		}
		k, err := api.AuthenticateKey(r.Context(), token)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		next(w, r.WithContext(WithAPIKey(r.Context(), k)))
	}
}

// bearerToken reads "Authorization: Bearer <token>" ("" without one).
func bearerToken(r *http.Request) string {
	scheme, token, _ := strings.Cut(r.Header.Get("Authorization"), " ")
	if strings.EqualFold(scheme, "Bearer") {
		return strings.TrimSpace(token)
	}
	return ""
}

// sessionToken reads "Authorization: Bearer <token>" or, from a browser, the session cookie.
func sessionToken(r *http.Request) string {
	if r.Header.Get("Authorization") != "" {
		return bearerToken(r)
	}
	if c, err := r.Cookie(CookieName); err == nil {
		return c.Value
	}
	return ""
}

// RequireUser answers 401 unless the request carries a valid session; the user is put in the context.
func RequireUser(api API, next func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := api.Authenticate(r.Context(), sessionToken(r))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		next(w, r.WithContext(WithUser(r.Context(), u)))
	}
}

func secure(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (h *Handler) setSession(w http.ResponseWriter, r *http.Request, s Session) {
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: s.Token, Path: "/", Expires: s.ExpiresAt, MaxAge: int(s.ExpiresAt.Sub(h.now()).Seconds()),
		HttpOnly: true, Secure: secure(r), SameSite: http.SameSiteStrictMode,
	})
}

func toSessionDTO(s Session) sessionDTO {
	return sessionDTO{Token: s.Token, ExpiresAt: s.ExpiresAt, User: ToUserDTO(s.User)}
}

func (h *Handler) toInvitationDTO(i Invitation) invitationDTO {
	dto := invitationDTO{
		ID: i.ID, Email: i.Email, Note: i.Note, Status: i.Status(h.now()), CreatedBy: i.CreatedBy, CreatedAt: i.CreatedAt,
		ExpiresAt: i.ExpiresAt, AcceptedAt: i.AcceptedAt, AcceptedUserID: i.AcceptedUserID, RevokedAt: i.RevokedAt, ProjectID: i.ProjectID,
	}
	if i.ProjectID != nil {
		role := i.ProjectRole.String()
		dto.ProjectRole = &role
	}
	return dto
}

// writeProjectError writes err, naming the requested project key when the project is not visible to the caller.
func writeProjectError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, errProjectHidden) {
		err = ProjectNotFound(r.PathValue("projectKey"))
	}
	httpx.WriteError(w, r, err)
}

// projectID resolves the {projectKey} path segment (400 when malformed, 404 when unknown).
func (h *Handler) projectID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	key, err := projectkey.Path(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return 0, false
	}
	id, err := h.projects.ProjectIDByKey(r.Context(), key)
	if err != nil {
		httpx.WriteError(w, r, err)
		return 0, false
	}
	return id, true
}

func (h *Handler) listMembers(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		writeProjectError(w, r, err)
		return
	}
	id, ok := h.projectID(w, r)
	if !ok {
		return
	}
	res, err := h.api.ListMembers(r.Context(), id, page)
	if err != nil {
		writeProjectError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewPage(res, toMemberDTO))
}

// pathUsername validates the {username} path segment (400 when it cannot be a username, before any lookup).
func pathUsername(w http.ResponseWriter, r *http.Request) (string, bool) {
	username := r.PathValue("username")
	if !UsernamePattern.MatchString(username) {
		httpx.WriteError(w, r, apperr.Validation(apperr.ValidationFailed, apperr.FieldError{Field: "username", Message: UsernameMessage}))
		return "", false
	}
	return username, true
}

func (h *Handler) setMember(w http.ResponseWriter, r *http.Request) {
	var req memberRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		writeProjectError(w, r, err)
		return
	}
	id, ok := h.projectID(w, r)
	if !ok {
		return
	}
	username, ok := pathUsername(w, r)
	if !ok {
		return
	}
	m, err := h.api.SetMember(r.Context(), id, username, req.Role)
	if err != nil {
		writeProjectError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toMemberDTO(m))
}

func (h *Handler) removeMember(w http.ResponseWriter, r *http.Request) {
	id, ok := h.projectID(w, r)
	if !ok {
		return
	}
	username, ok := pathUsername(w, r)
	if !ok {
		return
	}
	if err := h.api.RemoveMember(r.Context(), id, username); err != nil {
		writeProjectError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	s, err := h.api.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	h.setSession(w, r, s)
	httpx.WriteJSON(w, http.StatusOK, toSessionDTO(s))
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: secure(r), SameSite: http.SameSiteStrictMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	httpx.WriteJSON(w, http.StatusOK, ToUserDTO(u))
}

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	var req passwordRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	u, _ := UserFrom(r.Context())
	s, err := h.api.ChangePassword(r.Context(), u, req.CurrentPassword, req.NewPassword)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	h.setSession(w, r, s)
	httpx.WriteJSON(w, http.StatusOK, toSessionDTO(s))
}

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	u, _ := UserFrom(r.Context())
	res, err := h.api.ListUsers(r.Context(), u, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewPage(res, ToUserDTO))
}

func (h *Handler) listInvitations(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	u, _ := UserFrom(r.Context())
	res, err := h.api.ListInvitations(r.Context(), u, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewPage(res, h.toInvitationDTO))
}

func (h *Handler) createInvitation(w http.ResponseWriter, r *http.Request) {
	var req invitationRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	u, _ := UserFrom(r.Context())
	if err := requireAdmin(u); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	in := CreateInvitationInput{Email: req.Email, Note: req.Note, Role: req.Role}
	if req.Project != "" {
		if !projectkey.Valid(req.Project) {
			httpx.WriteError(w, r, projectkey.Invalid("project"))
			return
		}
		id, err := h.projects.ProjectIDByKey(r.Context(), req.Project)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		in.ProjectID = &id
	}
	inv, token, err := h.api.CreateInvitation(r.Context(), u, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, createdInvitationDTO{Invitation: h.toInvitationDTO(inv), Token: token})
}

func (h *Handler) revokeInvitation(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "invitationId")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	u, _ := UserFrom(r.Context())
	inv, err := h.api.RevokeInvitation(r.Context(), u, id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.toInvitationDTO(inv))
}

func (h *Handler) accept(w http.ResponseWriter, r *http.Request) {
	var req acceptRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	s, err := h.api.AcceptInvitation(r.Context(), AcceptInput(req))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	h.setSession(w, r, s)
	httpx.WriteJSON(w, http.StatusCreated, toSessionDTO(s))
}

func (h *Handler) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		writeProjectError(w, r, err)
		return
	}
	id, ok := h.projectID(w, r)
	if !ok {
		return
	}
	res, err := h.api.ListAPIKeys(r.Context(), id, page)
	if err != nil {
		writeProjectError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewPage(res, toAPIKeyDTO))
}

func (h *Handler) createAPIKey(w http.ResponseWriter, r *http.Request) {
	var req apiKeyRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		writeProjectError(w, r, err)
		return
	}
	id, ok := h.projectID(w, r)
	if !ok {
		return
	}
	k, token, err := h.api.CreateAPIKey(r.Context(), id, req.Name)
	if err != nil {
		writeProjectError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, createdAPIKeyDTO{APIKey: toAPIKeyDTO(k), Token: token})
}

func (h *Handler) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
	keyID, err := httpx.PathID(r, "apiKeyId")
	if err != nil {
		writeProjectError(w, r, err)
		return
	}
	id, ok := h.projectID(w, r)
	if !ok {
		return
	}
	k, err := h.api.RevokeAPIKey(r.Context(), id, keyID)
	if err != nil {
		writeProjectError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toAPIKeyDTO(k))
}
