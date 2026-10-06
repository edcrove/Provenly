package identity

import (
	"net/http"
	"time"

	"github.com/edcrove/provenly/backend/internal/platform/httpx"
)

type passwordResetDTO struct {
	Username  string    `json:"username"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type resetPasswordRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

// userAction runs an administrator's action on {username} and answers the user.
func (h *Handler) userAction(w http.ResponseWriter, r *http.Request, act func(User, string) (User, error)) {
	actor, _ := UserFrom(r.Context())
	u, err := act(actor, r.PathValue("username"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ToUserDTO(u))
}

func (h *Handler) deactivate(w http.ResponseWriter, r *http.Request) {
	h.userAction(w, r, func(actor User, username string) (User, error) { return h.api.Deactivate(r.Context(), actor, username) })
}

func (h *Handler) reactivate(w http.ResponseWriter, r *http.Request) {
	h.userAction(w, r, func(actor User, username string) (User, error) { return h.api.Reactivate(r.Context(), actor, username) })
}

func (h *Handler) createPasswordReset(w http.ResponseWriter, r *http.Request) {
	actor, _ := UserFrom(r.Context())
	reset, token, err := h.api.CreatePasswordReset(r.Context(), actor, r.PathValue("username"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, passwordResetDTO{Username: normalizeUsername(r.PathValue("username")), Token: token,
		ExpiresAt: reset.ExpiresAt})
}

func (h *Handler) resetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	s, err := h.api.ResetPassword(r.Context(), req.Token, req.Password)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	h.setSession(w, r, s)
	httpx.WriteJSON(w, http.StatusOK, toSessionDTO(s))
}
