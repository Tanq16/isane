package handlers

import (
	"cmp"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/rs/zerolog/log"
	"github.com/tanq16/isane/internal/app"
	"github.com/tanq16/isane/internal/auth"
	"github.com/tanq16/isane/internal/socket"
	"github.com/tanq16/isane/internal/store"
)

const (
	minPasswordLength  = 8
	forwardedForHeader = "X-Forwarded-For"
)

var handlePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

type Auth struct {
	app      *app.App
	accounts *limiter
	sources  *limiter
}

func NewAuth(a *app.App) *Auth {
	return &Auth{
		app:      a,
		accounts: newLimiter(freeAccountAttempts),
		sources:  newLimiter(freeSourceAttempts),
	}
}

var decoyHash = sync.OnceValue(func() string {
	hash, err := auth.HashPassword("isane-has-no-account-with-this-handle")
	if err != nil {
		log.Error().Err(err).Msg("build login decoy hash")
	}
	return hash
})

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

type profileRequest struct {
	DisplayName    *string    `json:"display_name"`
	AvatarID       *uuid.UUID `json:"avatar_id"`
	MarkReadOnOpen *bool      `json:"mark_read_on_open"`
}

type passwordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type sessionView struct {
	ID         uuid.UUID   `json:"id"`
	Current    bool        `json:"current"`
	CreatedAt  time.Time   `json:"created_at"`
	LastSeenAt time.Time   `json:"last_seen_at"`
	ExpiresAt  time.Time   `json:"expires_at"`
	IP         *netip.Addr `json:"ip"`
	UserAgent  *string     `json:"user_agent"`
}

type revokedBody struct {
	Revoked int64 `json:"revoked"`
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
	source, account := requestKey(r, h.trusted()), strings.ToLower(ident)
	if wait := max(h.sources.retryAfter(source), h.accounts.retryAfter(account)); wait > 0 {
		writeRetryAfter(w, wait, "too many sign-in attempts, try again shortly")
		return
	}
	u, err := h.app.DB.GetUserByLogin(r.Context(), ident)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		WriteError(w, fmt.Errorf("login lookup: %w", err))
		return
	}
	stored := decoyHash()
	if err == nil && u.Active() && u.PasswordHash != nil {
		stored = *u.PasswordHash
	}
	if !auth.VerifyPassword(stored, req.Password) || err != nil || !u.Active() || u.PasswordHash == nil {
		h.sources.fail(source)
		h.accounts.fail(account)
		WriteError(w, errInvalidLogin())
		return
	}
	h.sources.succeed(source)
	h.accounts.succeed(account)
	if err := h.startSession(w, r, u); err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, u)
}

func (h *Auth) Logout(w http.ResponseWriter, r *http.Request) {
	if raw, ok := auth.ReadSession(r); ok {
		sess, _, err := h.app.DB.SessionByTokenHash(r.Context(), auth.HashToken(raw), time.Now().Add(-auth.SessionMaxAge))
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

func (h *Auth) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	u, ok := requestUser(w, r)
	if !ok {
		return
	}
	var req profileRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	displayName := u.DisplayName
	if req.DisplayName != nil {
		name, err := boundedField("display_name", *req.DisplayName, maxNameLength)
		if err != nil {
			WriteError(w, err)
			return
		}
		displayName = name
	}
	avatarID := req.AvatarID
	if avatarID != nil {
		if err := h.ownedAvatar(r, *avatarID, u.ID); err != nil {
			WriteError(w, err)
			return
		}
	}
	if err := h.app.DB.UpdateProfile(r.Context(), u.ID, displayName, avatarID); err != nil {
		WriteError(w, err)
		return
	}
	if req.MarkReadOnOpen != nil {
		if err := h.app.DB.SetMarkReadOnOpen(r.Context(), u.ID, *req.MarkReadOnOpen); err != nil {
			WriteError(w, err)
			return
		}
	}
	updated, err := h.app.DB.GetUser(r.Context(), u.ID)
	if err != nil {
		WriteError(w, err)
		return
	}
	h.app.Hub.ToAll(socket.NewFrame(socket.TypeUser, updated.Directory()))
	audit(r, h.app, store.AuditEvent{
		Action:      "profile.update",
		TargetType:  new(targetUser),
		TargetID:    &u.ID,
		TargetLabel: &u.Handle,
		Detail:      auditDetail(map[string]any{"fields": changedProfileFields(u, updated)}),
	})
	WriteJSON(w, http.StatusOK, updated)
}

func (h *Auth) ownedAvatar(r *http.Request, id, userID uuid.UUID) error {
	a, err := h.app.DB.GetAttachment(r.Context(), id)
	if err != nil {
		return err
	}
	if a.UploaderID != userID {
		return forbiddenf("not the uploader of this attachment")
	}
	if a.Kind != store.AttachmentImage || a.State != store.AttachmentReady {
		return badRequestf("avatar_id must name a processed image")
	}
	return nil
}

func (h *Auth) AcceptInvite(w http.ResponseWriter, r *http.Request) {
	var req acceptInviteRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	source := requestKey(r, h.trusted())
	if wait := h.sources.retryAfter(source); wait > 0 {
		writeRetryAfter(w, wait, "too many invite attempts, try again shortly")
		return
	}
	handle := strings.ToLower(strings.TrimSpace(req.Handle))
	if req.Token == "" {
		WriteError(w, badRequestf("token is required"))
		return
	}
	if !handlePattern.MatchString(handle) {
		WriteError(w, badRequestf("handle must match ^[a-z0-9][a-z0-9_-]{0,31}$"))
		return
	}
	displayName, err := boundedField("display_name", req.DisplayName, maxNameLength)
	if err != nil {
		WriteError(w, err)
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
	u, err := h.app.DB.AcceptInvite(r.Context(), auth.HashToken(req.Token), store.User{
		Kind:         store.UserHuman,
		Handle:       handle,
		DisplayName:  displayName,
		PasswordHash: &hash,
	})
	if err != nil {
		h.sources.fail(source)
		WriteError(w, err)
		return
	}
	h.sources.succeed(source)
	if err := h.startSession(w, r, u); err != nil {
		WriteError(w, err)
		return
	}
	h.app.Hub.ToAll(socket.NewFrame(socket.TypeUser, u.Directory()))
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
	if err := h.app.DB.DeleteSessionsForUser(r.Context(), u.ID); err != nil {
		WriteError(w, err)
		return
	}
	if current, ok := SessionFrom(r.Context()); ok {
		h.app.Hub.CloseSessionsExcept(u.ID, current)
	}
	if err := h.startSession(w, r, u); err != nil {
		WriteError(w, err)
		return
	}
	audit(r, h.app, store.AuditEvent{
		Action:      "password.change",
		TargetType:  new(targetUser),
		TargetID:    &u.ID,
		TargetLabel: &u.Handle,
	})
	writeOK(w)
}

func (h *Auth) ListSessions(w http.ResponseWriter, r *http.Request) {
	u, ok := requestUser(w, r)
	if !ok {
		return
	}
	current, _ := SessionFrom(r.Context())
	sessions, err := h.app.DB.ListSessionsForUser(r.Context(), u.ID, time.Now().Add(-auth.SessionMaxAge))
	if err != nil {
		WriteError(w, err)
		return
	}
	out := make([]sessionView, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, sessionView{
			ID:         s.ID,
			Current:    s.ID == current,
			CreatedAt:  s.CreatedAt,
			LastSeenAt: s.LastSeenAt,
			ExpiresAt:  s.ExpiresAt,
			IP:         s.IP,
			UserAgent:  s.UserAgent,
		})
	}
	WriteJSON(w, http.StatusOK, out)
}

func (h *Auth) RevokeSession(w http.ResponseWriter, r *http.Request) {
	u, ok := requestUser(w, r)
	if !ok {
		return
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		WriteError(w, err)
		return
	}
	if err := h.app.DB.DeleteUserSession(r.Context(), u.ID, id); err != nil {
		WriteError(w, err)
		return
	}
	h.app.Hub.CloseSession(u.ID, id)
	audit(r, h.app, store.AuditEvent{
		Action:     "session.revoke",
		TargetType: new(targetSession),
		TargetID:   &id,
	})
	writeOK(w)
}

func (h *Auth) RevokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	u, ok := requestUser(w, r)
	if !ok {
		return
	}
	current, ok := SessionFrom(r.Context())
	if !ok {
		WriteError(w, errSignIn())
		return
	}
	revoked, err := h.app.DB.DeleteSessionsForUserExcept(r.Context(), u.ID, current)
	if err != nil {
		WriteError(w, err)
		return
	}
	h.app.Hub.CloseSessionsExcept(u.ID, current)
	audit(r, h.app, store.AuditEvent{
		Action:     "session.revoke_others",
		TargetType: new(targetSession),
		Detail:     auditDetail(map[string]any{"revoked": revoked}),
	})
	WriteJSON(w, http.StatusOK, revokedBody{Revoked: revoked})
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
		IP:        clientIP(r, h.trusted()),
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

func (h *Auth) trusted() []netip.Prefix {
	return h.app.Cfg().Server.TrustedPrefixes()
}

func errInvalidLogin() error {
	return fmt.Errorf("%w: invalid handle or password", ErrUnauthorized)
}

func changedProfileFields(before, after store.User) []string {
	fields := make([]string, 0, 3)
	if before.DisplayName != after.DisplayName {
		fields = append(fields, "display_name")
	}
	if !sameUUID(before.AvatarID, after.AvatarID) {
		fields = append(fields, "avatar_id")
	}
	if before.MarkReadOnOpen != after.MarkReadOnOpen {
		fields = append(fields, "mark_read_on_open")
	}
	return fields
}

func sameUUID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func requestKey(r *http.Request, trusted []netip.Prefix) string {
	if addr := clientIP(r, trusted); addr != nil {
		return "ip:" + addr.String()
	}
	return "ip:" + r.RemoteAddr
}

func clientIP(r *http.Request, trusted []netip.Prefix) *netip.Addr {
	peer := parseAddr(r.RemoteAddr)
	if peer == nil || !trustedAddr(trusted, *peer) {
		return peer
	}
	var chain []string
	for _, value := range r.Header.Values(forwardedForHeader) {
		chain = append(chain, strings.Split(value, ",")...)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		addr := parseAddr(chain[i])
		if addr != nil && !trustedAddr(trusted, *addr) {
			return addr
		}
	}
	return peer
}

func parseAddr(raw string) *netip.Addr {
	host := strings.TrimSpace(raw)
	if host == "" {
		return nil
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		unmapped := addr.Unmap()
		return &unmapped
	}
	if split, _, err := net.SplitHostPort(host); err == nil {
		host = split
	} else if bracketed, ok := strings.CutPrefix(host, "["); ok {
		host, _, _ = strings.Cut(bracketed, "]")
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return nil
	}
	unmapped := addr.Unmap()
	return &unmapped
}

func trustedAddr(trusted []netip.Prefix, addr netip.Addr) bool {
	return slices.ContainsFunc(trusted, func(p netip.Prefix) bool { return p.Contains(addr) })
}
