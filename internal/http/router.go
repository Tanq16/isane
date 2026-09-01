package http

import (
	"net/http"

	"github.com/tanq16/isane/internal/http/handlers"
)

func (s *Server) routes(mux *http.ServeMux) {
	sessions := handlers.NewAuth(s.app)
	containers := handlers.NewContainers(s.app)
	messages := handlers.NewMessages(s.app)
	media := handlers.NewMedia(s.app)
	push := handlers.NewPush(s.app)
	calls := handlers.NewCalls(s.app)
	agents := handlers.NewAgents(s.app)
	admin := handlers.NewAdmin(s.app)

	mux.HandleFunc("POST /api/auth/login", sessions.Login)
	mux.HandleFunc("POST /api/auth/logout", sessions.Logout)
	mux.HandleFunc("POST /api/auth/accept-invite", sessions.AcceptInvite)
	mux.Handle("GET /api/auth/me", s.user(sessions.Me))
	mux.Handle("POST /api/auth/password", s.user(sessions.ChangePassword))

	mux.Handle("GET /api/containers", s.user(containers.List))
	mux.Handle("PUT /api/containers/{id}/read", s.user(containers.SetRead))
	mux.Handle("PUT /api/containers/{id}/notification-pref", s.user(containers.SetNotificationPref))
	mux.Handle("POST /api/conversations", s.user(containers.CreateConversation))
	mux.Handle("GET /api/users", s.user(containers.ListUsers))

	mux.Handle("GET /api/containers/{id}/messages", s.user(messages.List))
	mux.Handle("POST /api/containers/{id}/messages", s.user(messages.Post))
	mux.Handle("PATCH /api/messages/{id}", s.user(messages.Edit))
	mux.Handle("DELETE /api/messages/{id}", s.user(messages.Delete))
	mux.Handle("GET /api/threads/{root_id}/messages", s.user(messages.Thread))
	mux.Handle("PUT /api/threads/{root_id}/subscription", s.user(messages.Subscribe))
	mux.Handle("GET /api/search", s.user(messages.Search))

	mux.Handle("POST /api/uploads", s.user(media.Upload))
	mux.Handle("GET /api/attachments/{id}", s.user(media.Get))
	mux.Handle("GET /api/attachments/{id}/thumb", s.user(media.Thumb))

	mux.Handle("GET /api/push/vapid-key", s.user(push.VAPIDKey))
	mux.Handle("POST /api/push/subscribe", s.user(push.Subscribe))
	mux.Handle("DELETE /api/push/subscribe", s.user(push.Unsubscribe))
	mux.Handle("PUT /api/push/subscribe/enabled", s.user(push.SetEnabled))

	mux.Handle("POST /api/containers/{id}/call", s.user(calls.Start))
	mux.Handle("GET /api/calls/{id}/token", s.user(calls.Token))
	mux.Handle("DELETE /api/calls/{id}/me", s.user(calls.Leave))
	mux.Handle("POST /api/calls/{id}/recording", s.user(calls.Recording))
	mux.Handle("GET /api/calls/{id}/recordings", s.user(calls.Recordings))
	mux.HandleFunc("POST /api/livekit/webhook", calls.Webhook)

	mux.Handle("POST /api/agent/register", s.agent(agents.Register))
	mux.Handle("POST /api/agent/deregister", s.agent(agents.Deregister))
	mux.Handle("POST /api/agent/hello", s.agent(agents.Hello))
	mux.Handle("GET /api/agent/jobs", s.agent(agents.Jobs))
	mux.Handle("POST /api/agent/jobs/{id}/result", s.agent(agents.Result))
	mux.Handle("GET /api/agent/jobs/{job_id}/attachments/{attachment_id}", s.agent(agents.Attachment))

	mux.Handle("GET /api/admin/users", s.adminOnly(admin.ListUsers))
	mux.Handle("POST /api/admin/users", s.adminOnly(admin.CreateUser))
	mux.Handle("DELETE /api/admin/users/{id}", s.adminOnly(admin.DeactivateUser))
	mux.Handle("POST /api/admin/users/{id}/password", s.adminOnly(admin.ResetPassword))
	mux.Handle("PUT /api/admin/users/{id}/admin", s.adminOnly(admin.SetAdmin))
	mux.Handle("POST /api/admin/invites", s.adminOnly(admin.CreateInvite))
	mux.Handle("GET /api/admin/invites", s.adminOnly(admin.ListInvites))
	mux.Handle("DELETE /api/admin/invites/{id}", s.adminOnly(admin.RevokeInvite))
	mux.Handle("GET /api/admin/channels", s.adminOnly(admin.ListChannels))
	mux.Handle("POST /api/admin/channels", s.adminOnly(admin.CreateChannel))
	mux.Handle("PATCH /api/admin/channels/{id}", s.adminOnly(admin.UpdateChannel))
	mux.Handle("DELETE /api/admin/channels/{id}", s.adminOnly(admin.ArchiveChannel))
	mux.Handle("POST /api/admin/channels/{id}/unarchive", s.adminOnly(admin.UnarchiveChannel))
	mux.Handle("GET /api/admin/agents", s.adminOnly(admin.ListAgents))
	mux.Handle("POST /api/admin/agents", s.adminOnly(admin.ReserveAgent))
	mux.Handle("POST /api/admin/agents/{handle}/deregister", s.adminOnly(admin.DeregisterAgent))
	mux.Handle("DELETE /api/admin/agents/{handle}", s.adminOnly(admin.DeleteAgent))
	mux.Handle("GET /api/admin/stats", s.adminOnly(admin.Stats))
	mux.Handle("GET /api/admin/retention", s.adminOnly(admin.Retention))
	mux.Handle("GET /api/health", http.HandlerFunc(s.health))

	mux.Handle("GET /ws", s.user(s.handleWS))

	mux.HandleFunc("/", s.serveStatic)
}

func (s *Server) user(h http.HandlerFunc) http.Handler {
	return s.session(handlers.RequireUser(h))
}

func (s *Server) adminOnly(h http.HandlerFunc) http.Handler {
	return s.session(handlers.RequireAdmin(h))
}

func (s *Server) agent(h http.HandlerFunc) http.Handler {
	return s.agentAuth(h)
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	u, ok := handlers.UserFrom(r.Context())
	if !ok {
		handlers.WriteError(w, handlers.ErrUnauthorized)
		return
	}
	s.app.Hub.ServeWS(w, r, u)
}
