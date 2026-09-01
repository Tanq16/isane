package handlers

import (
	"net/http"

	"github.com/tanq16/isane/internal/app"
	"github.com/tanq16/isane/internal/store"
)

type Push struct{ app *app.App }

func NewPush(a *app.App) *Push { return &Push{app: a} }

type vapidKey struct {
	Enabled   bool   `json:"enabled"`
	PublicKey string `json:"public_key,omitempty"`
}

type subscribeBody struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

type endpointBody struct {
	Endpoint string `json:"endpoint"`
	Enabled  bool   `json:"enabled"`
}

func (h *Push) VAPIDKey(w http.ResponseWriter, r *http.Request) {
	if !h.app.Push.Enabled() {
		WriteJSON(w, http.StatusOK, vapidKey{})
		return
	}
	WriteJSON(w, http.StatusOK, vapidKey{Enabled: true, PublicKey: h.app.Push.VAPIDPublicKey()})
}

func (h *Push) Subscribe(w http.ResponseWriter, r *http.Request) {
	u, ok := requestUser(w, r)
	if !ok || !h.available(w) {
		return
	}
	var body subscribeBody
	if err := ReadJSON(r, &body); err != nil {
		WriteError(w, err)
		return
	}
	if body.Endpoint == "" || body.Keys.P256dh == "" || body.Keys.Auth == "" {
		WriteError(w, badRequestf("endpoint, keys.p256dh, and keys.auth are required"))
		return
	}
	sub := store.PushSubscription{
		UserID:   u.ID,
		Endpoint: body.Endpoint,
		P256dh:   body.Keys.P256dh,
		Auth:     body.Keys.Auth,
	}
	if agent := r.UserAgent(); agent != "" {
		sub.UserAgent = &agent
	}
	out, err := h.app.DB.UpsertPushSubscription(r.Context(), sub)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

func (h *Push) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	u, ok := requestUser(w, r)
	if !ok {
		return
	}
	body, ok := readEndpoint(w, r)
	if !ok {
		return
	}
	if err := h.app.DB.DeletePushSubscription(r.Context(), u.ID, body.Endpoint); err != nil {
		WriteError(w, err)
		return
	}
	writeOK(w)
}

func (h *Push) SetEnabled(w http.ResponseWriter, r *http.Request) {
	u, ok := requestUser(w, r)
	if !ok || !h.available(w) {
		return
	}
	body, ok := readEndpoint(w, r)
	if !ok {
		return
	}
	if err := h.app.DB.SetPushSubscriptionEnabled(r.Context(), u.ID, body.Endpoint, body.Enabled); err != nil {
		WriteError(w, err)
		return
	}
	writeOK(w)
}

func (h *Push) available(w http.ResponseWriter) bool {
	if h.app.Push.Enabled() {
		return true
	}
	WriteJSON(w, http.StatusServiceUnavailable,
		errorBody{Error: "push is unavailable: it needs vapid keys and a trusted certificate"})
	return false
}

func readEndpoint(w http.ResponseWriter, r *http.Request) (endpointBody, bool) {
	var body endpointBody
	if err := ReadJSON(r, &body); err != nil {
		WriteError(w, err)
		return body, false
	}
	if body.Endpoint == "" {
		WriteError(w, badRequestf("endpoint is required"))
		return body, false
	}
	return body, true
}
