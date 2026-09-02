package handlers

import (
	"cmp"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
	"time"
	"uuid"

	"github.com/tanq16/isane/internal/app"
	"github.com/tanq16/isane/internal/auth"
	"github.com/tanq16/isane/internal/store"
)

const minPasswordLength = 8

var handlePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

type Auth struct {
	app *app.App
}

func NewAuth(a *app.App) *Auth {
	return &Auth{app: a}
}

type loginRequest struct {
	Handle   string `json:"handle"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type acceptInviteRequest struct {
	Token       string `json:"token"`
	Handle      string `json:"handle"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
}

type passwordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (h *Auth) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	ident := strings.TrimSpace(cmp.Or(req.Handle, req.Email))
	if ident == "" || req.Password == "" {
		WriteError(w, badRequestf("handle or email and password are required"))
		return
	}
	u, err := h.app.DB.GetUserByLogin(r.Context(), ident)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, errInvalidLogin())
			return
		}
		WriteError(w, fmt.Errorf("login lookup: %w", err))
		return
	}
	if !u.Active() || u.PasswordHash == nil || !auth.VerifyPassword(*u.PasswordHash, req.Password) {
		WriteError(w, errInvalidLogin())
		return
	}
	if err := h.startSession(w, r, u); err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, u)
}

func (h *Auth) Logout(w http.ResponseWriter, r *http.Request) {
	if raw, ok := auth.ReadSession(r); ok {
		sess, _, err := h.app.DB.SessionByTokenHash(r.Context(), auth.HashToken(raw))
		if err == nil {
			if err := h.app.DB.DeleteSession(r.Context(), sess.ID); err != nil {
				h.app.Log.Error().Err(err).Msg("delete session")
			}
		}
	}
	auth.ClearSession(w, h.secure())
	writeOK(w)
}

func (h *Auth) Me(w http.ResponseWriter, r *http.Request) {
	u, ok := requestUser(w, r)
	if !ok {
		return
	}
	WriteJSON(w, http.StatusOK, u)
}

func (h *Auth) AcceptInvite(w http.ResponseWriter, r *http.Request) {
	var req acceptInviteRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	handle := strings.ToLower(strings.TrimSpace(req.Handle))
	displayName := strings.TrimSpace(req.DisplayName)
	if req.Token == "" {
		WriteError(w, badRequestf("token is required"))
		return
	}
	if !handlePattern.MatchString(handle) {
		WriteError(w, badRequestf("handle must match ^[a-z0-9][a-z0-9_-]{0,31}$"))
		return
	}
	if displayName == "" {
		WriteError(w, badRequestf("display_name is required"))
		return
	}
	if len(req.Password) < minPasswordLength {
		WriteError(w, badRequestf("password must be at least %d characters", minPasswordLength))
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		WriteError(w, fmt.Errorf("hash password: %w", err))
		return
	}
	count, err := h.app.DB.CountUsers(r.Context())
	if err != nil {
		WriteError(w, fmt.Errorf("count users: %w", err))
		return
	}
	u, err := h.app.DB.AcceptInvite(r.Context(), auth.HashToken(req.Token), store.User{
		Kind:         store.UserHuman,
		Handle:       handle,
		DisplayName:  displayName,
		PasswordHash: &hash,
		IsAdmin:      count == 0,
	})
	if err != nil {
		WriteError(w, err)
		return
	}
	if err := h.startSession(w, r, u); err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, u)
}

func (h *Auth) ChangePassword(w http.ResponseWriter, r *http.Request) {
	u, ok := requestUser(w, r)
	if !ok {
		return
	}
	var req passwordRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	if u.PasswordHash == nil || !auth.VerifyPassword(*u.PasswordHash, req.CurrentPassword) {
		WriteError(w, fmt.Errorf("%w: current password is incorrect", ErrUnauthorized))
		return
	}
	if len(req.NewPassword) < minPasswordLength {
		WriteError(w, badRequestf("password must be at least %d characters", minPasswordLength))
		return
	}
	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		WriteError(w, fmt.Errorf("hash password: %w", err))
		return
	}
	if err := h.app.DB.SetPassword(r.Context(), u.ID, hash); err != nil {
		WriteError(w, err)
		return
	}
	writeOK(w)
}

func (h *Auth) startSession(w http.ResponseWriter, r *http.Request, u store.User) error {
	raw, hash, err := auth.NewToken()
	if err != nil {
		return fmt.Errorf("mint session token: %w", err)
	}
	expires := time.Now().Add(auth.SessionTTL)
	sess := store.Session{
		ID:        uuid.New(),
		TokenHash: hash,
		UserID:    u.ID,
		ExpiresAt: expires,
		UserAgent: optionalString(r.UserAgent()),
		IP:        clientIP(r),
	}
	if err := h.app.DB.CreateSession(r.Context(), sess); err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	auth.SetSession(w, raw, expires, h.secure())
	return nil
}

func (h *Auth) secure() bool {
	return !h.app.Cfg().Server.Insecure
}

func errInvalidLogin() error {
	return fmt.Errorf("%w: invalid handle or password", ErrUnauthorized)
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func clientIP(r *http.Request) *netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return nil
	}
	return &addr
}
