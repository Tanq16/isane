package handlers

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"uuid"

	"github.com/tanq16/isane/internal/app"
	"github.com/tanq16/isane/internal/auth"
	"github.com/tanq16/isane/internal/store"
)

const defaultInviteTTL = 7 * 24 * time.Hour

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

type Admin struct {
	app *app.App
}

func NewAdmin(a *app.App) *Admin {
	return &Admin{app: a}
}

type adminUserRequest struct {
	Handle      string `json:"handle"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}

type adminPasswordRequest struct {
	Password string `json:"password"`
}

type adminFlagRequest struct {
	IsAdmin bool `json:"is_admin"`
}

type adminInviteRequest struct {
	Note      string `json:"note"`
	ExpiresIn int64  `json:"expires_in"`
}

type adminInvite struct {
	ID        string     `json:"id"`
	URL       string     `json:"url,omitempty"`
	Note      *string    `json:"note,omitempty"`
	CreatedBy uuid.UUID  `json:"created_by"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	UsedBy    *uuid.UUID `json:"used_by,omitempty"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
}

type adminChannelRequest struct {
	Slug  string  `json:"slug"`
	Name  string  `json:"name"`
	Topic *string `json:"topic"`
}

type adminChannelUpdate struct {
	Name  string  `json:"name"`
	Topic *string `json:"topic"`
}

type adminAgentRequest struct {
	Handle      string `json:"handle"`
	DisplayName string `json:"display_name"`
}

type adminAgentReserved struct {
	Agent      store.AgentInfo `json:"agent"`
	ClaimToken string          `json:"claim_token"`
}

type adminAgentDeleted struct {
	HandleFreed bool `json:"handle_freed"`
}

type adminRetention struct {
	MessageDays       int        `json:"message_days"`
	RecordingDays     int        `json:"recording_days"`
	StagedUploadHours int        `json:"staged_upload_hours"`
	LastRunAt         *time.Time `json:"last_run_at,omitempty"`
}

func (h *Admin) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.app.DB.ListUsers(r.Context())
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, nonNil(users))
}

func (h *Admin) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req adminUserRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	handle := strings.ToLower(strings.TrimSpace(req.Handle))
	displayName := strings.TrimSpace(req.DisplayName)
	if !handlePattern.MatchString(handle) {
		WriteError(w, badRequestf("handle must match ^[a-z0-9][a-z0-9_-]{0,31}$"))
		return
	}
	if displayName == "" {
		WriteError(w, badRequestf("display_name is required"))
		return
	}
	created, err := h.app.DB.CreateUser(r.Context(), store.User{
		Kind:        store.UserHuman,
		Handle:      handle,
		DisplayName: displayName,
		Email:       optionalString(strings.TrimSpace(req.Email)),
	})
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, created)
}

func (h *Admin) DeactivateUser(w http.ResponseWriter, r *http.Request) {
	admin, ok := requestUser(w, r)
	if !ok {
		return
	}
	target, ok := h.human(w, r)
	if !ok {
		return
	}
	if target.ID == admin.ID {
		WriteError(w, conflictf("an administrator cannot deactivate their own account"))
		return
	}
	if err := h.app.DB.DeactivateUser(r.Context(), target.ID); err != nil {
		WriteError(w, err)
		return
	}
	if err := h.app.DB.DeleteSessionsForUser(r.Context(), target.ID); err != nil {
		WriteError(w, err)
		return
	}
	writeOK(w)
}

func (h *Admin) ResetPassword(w http.ResponseWriter, r *http.Request) {
	target, ok := h.human(w, r)
	if !ok {
		return
	}
	var req adminPasswordRequest
	if err := ReadJSON(r, &req); err != nil {
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
	if err := h.app.DB.SetPassword(r.Context(), target.ID, hash); err != nil {
		WriteError(w, err)
		return
	}
	writeOK(w)
}

func (h *Admin) SetAdmin(w http.ResponseWriter, r *http.Request) {
	admin, ok := requestUser(w, r)
	if !ok {
		return
	}
	target, ok := h.human(w, r)
	if !ok {
		return
	}
	var req adminFlagRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	if target.ID == admin.ID && !req.IsAdmin {
		WriteError(w, conflictf("an administrator cannot drop their own administrator flag"))
		return
	}
	if err := h.app.DB.SetAdmin(r.Context(), target.ID, req.IsAdmin); err != nil {
		WriteError(w, err)
		return
	}
	writeOK(w)
}

func (h *Admin) ListInvites(w http.ResponseWriter, r *http.Request) {
	invites, err := h.app.DB.ListInvites(r.Context())
	if err != nil {
		WriteError(w, err)
		return
	}
	out := make([]adminInvite, 0, len(invites))
	for _, inv := range invites {
		out = append(out, inviteView(inv))
	}
	WriteJSON(w, http.StatusOK, out)
}

func (h *Admin) CreateInvite(w http.ResponseWriter, r *http.Request) {
	admin, ok := requestUser(w, r)
	if !ok {
		return
	}
	var req adminInviteRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	ttl := time.Duration(req.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = defaultInviteTTL
	}
	raw, hash, err := auth.NewToken()
	if err != nil {
		WriteError(w, fmt.Errorf("mint invite token: %w", err))
		return
	}
	err = h.app.DB.CreateInvite(r.Context(), store.Invite{
		TokenHash: hash,
		CreatedBy: admin.ID,
		Note:      optionalString(strings.TrimSpace(req.Note)),
		ExpiresAt: time.Now().Add(ttl),
	})
	if err != nil {
		WriteError(w, err)
		return
	}
	stored, err := h.app.DB.GetInvite(r.Context(), hash)
	if err != nil {
		WriteError(w, err)
		return
	}
	out := inviteView(stored)
	out.URL = h.publicURL("/invite/" + raw)
	WriteJSON(w, http.StatusCreated, out)
}

func (h *Admin) RevokeInvite(w http.ResponseWriter, r *http.Request) {
	hash, err := hex.DecodeString(r.PathValue("id"))
	if err != nil {
		WriteError(w, badRequestf("id must be an invite id in hex"))
		return
	}
	if err := h.app.DB.RevokeInvite(r.Context(), hash); err != nil {
		WriteError(w, err)
		return
	}
	writeOK(w)
}

func (h *Admin) ListChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := h.app.DB.ListChannels(r.Context(), true)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, nonNil(channels))
}

func (h *Admin) CreateChannel(w http.ResponseWriter, r *http.Request) {
	admin, ok := requestUser(w, r)
	if !ok {
		return
	}
	var req adminChannelRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	slug := strings.ToLower(strings.TrimSpace(req.Slug))
	name := strings.TrimSpace(req.Name)
	if !slugPattern.MatchString(slug) {
		WriteError(w, badRequestf("slug must match ^[a-z0-9][a-z0-9-]{0,63}$"))
		return
	}
	if name == "" {
		WriteError(w, badRequestf("name is required"))
		return
	}
	c, err := h.app.DB.CreateChannel(r.Context(), slug, name, trimTopic(req.Topic), admin.ID)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, c)
}

func (h *Admin) UpdateChannel(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		WriteError(w, err)
		return
	}
	var req adminChannelUpdate
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	c, err := h.app.DB.GetContainer(r.Context(), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		if c.Name == nil {
			WriteError(w, badRequestf("name is required"))
			return
		}
		name = *c.Name
	}
	topic := c.Topic
	if req.Topic != nil {
		topic = trimTopic(req.Topic)
	}
	if err := h.app.DB.UpdateChannel(r.Context(), id, name, topic); err != nil {
		WriteError(w, err)
		return
	}
	c, err = h.app.DB.GetContainer(r.Context(), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, c)
}

func (h *Admin) ArchiveChannel(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		WriteError(w, err)
		return
	}
	if err := h.app.DB.ArchiveChannel(r.Context(), id); err != nil {
		WriteError(w, err)
		return
	}
	writeOK(w)
}

func (h *Admin) UnarchiveChannel(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		WriteError(w, err)
		return
	}
	if err := h.app.DB.UnarchiveChannel(r.Context(), id); err != nil {
		WriteError(w, err)
		return
	}
	writeOK(w)
}

func (h *Admin) ListAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := h.app.DB.ListAgents(r.Context())
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, nonNil(agents))
}

func (h *Admin) ReserveAgent(w http.ResponseWriter, r *http.Request) {
	var req adminAgentRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	handle := strings.ToLower(strings.TrimSpace(req.Handle))
	displayName := strings.TrimSpace(req.DisplayName)
	if !handlePattern.MatchString(handle) {
		WriteError(w, badRequestf("handle must match ^[a-z0-9][a-z0-9_-]{0,31}$"))
		return
	}
	if displayName == "" {
		WriteError(w, badRequestf("display_name is required"))
		return
	}
	raw, hash, err := auth.NewToken()
	if err != nil {
		WriteError(w, fmt.Errorf("mint claim token: %w", err))
		return
	}
	info, err := h.app.DB.ReserveAgent(r.Context(), handle, displayName, hash)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, adminAgentReserved{Agent: info, ClaimToken: raw})
}

func (h *Admin) DeregisterAgent(w http.ResponseWriter, r *http.Request) {
	info, ok := h.agent(w, r)
	if !ok {
		return
	}
	if err := h.app.Agents.Deregister(r.Context(), info); err != nil {
		WriteError(w, err)
		return
	}
	writeOK(w)
}

func (h *Admin) DeleteAgent(w http.ResponseWriter, r *http.Request) {
	info, ok := h.agent(w, r)
	if !ok {
		return
	}
	freed, err := h.app.DB.DeleteAgent(r.Context(), info.User.ID)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, adminAgentDeleted{HandleFreed: freed})
}

func (h *Admin) Stats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.app.DB.Stats(r.Context())
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, stats)
}

func (h *Admin) Retention(w http.ResponseWriter, r *http.Request) {
	last, err := h.app.Retention.LastRun(r.Context())
	if err != nil {
		WriteError(w, err)
		return
	}
	out := adminRetention{
		MessageDays:       h.app.Cfg.Retention.MessageDays,
		RecordingDays:     h.app.Cfg.Retention.RecordingDays,
		StagedUploadHours: h.app.Cfg.Retention.StagedUploadHours,
	}
	if !last.IsZero() {
		out.LastRunAt = &last
	}
	WriteJSON(w, http.StatusOK, out)
}

func (h *Admin) human(w http.ResponseWriter, r *http.Request) (store.User, bool) {
	id, err := pathUUID(r, "id")
	if err != nil {
		WriteError(w, err)
		return store.User{}, false
	}
	u, err := h.app.DB.GetUser(r.Context(), id)
	if err != nil {
		WriteError(w, err)
		return store.User{}, false
	}
	if u.Kind != store.UserHuman {
		WriteError(w, badRequestf("%s is an agent and is managed through the agent endpoints", u.Handle))
		return store.User{}, false
	}
	return u, true
}

func (h *Admin) agent(w http.ResponseWriter, r *http.Request) (store.AgentInfo, bool) {
	info, err := h.app.DB.AgentByHandle(r.Context(), r.PathValue("handle"))
	if err != nil {
		WriteError(w, err)
		return store.AgentInfo{}, false
	}
	return info, true
}

func (h *Admin) publicURL(path string) string {
	return strings.TrimSuffix(h.app.Cfg.Server.PublicURL, "/") + path
}

func inviteView(inv store.Invite) adminInvite {
	return adminInvite{
		ID:        hex.EncodeToString(inv.TokenHash),
		Note:      inv.Note,
		CreatedBy: inv.CreatedBy,
		CreatedAt: inv.CreatedAt,
		ExpiresAt: inv.ExpiresAt,
		UsedBy:    inv.UsedBy,
		UsedAt:    inv.UsedAt,
	}
}

func trimTopic(topic *string) *string {
	if topic == nil {
		return nil
	}
	return optionalString(strings.TrimSpace(*topic))
}
