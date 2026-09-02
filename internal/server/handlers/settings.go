package handlers

import (
	"net/http"

	"github.com/tanq16/isane/internal/app"
)

type Settings struct {
	app *app.App
}

func NewSettings(a *app.App) *Settings {
	return &Settings{app: a}
}

type settingsRequest struct {
	AllowMemberChannels bool `json:"allow_member_channels"`
}

func (h *Settings) Get(w http.ResponseWriter, r *http.Request) {
	s, err := h.app.DB.ServerSettings(r.Context())
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, s)
}

func (h *Settings) Update(w http.ResponseWriter, r *http.Request) {
	u, ok := requestUser(w, r)
	if !ok {
		return
	}
	var req settingsRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	s, err := h.app.DB.UpdateServerSettings(r.Context(), req.AllowMemberChannels, u.ID)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, s)
}
