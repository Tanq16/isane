package handlers

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/livekit/protocol/livekit"

	"github.com/tanq16/isane/internal/app"
	"github.com/tanq16/isane/internal/socket"
	"github.com/tanq16/isane/internal/store"
)

const (
	egressOutRoot          = "/out"
	eventParticipantJoined = "participant_joined"
	eventParticipantLeft   = "participant_left"
	eventRoomFinished      = "room_finished"
)

type Calls struct{ app *app.App }

func NewCalls(a *app.App) *Calls { return &Calls{app: a} }

type joinToken struct {
	Token    string `json:"token"`
	URL      string `json:"url"`
	RoomName string `json:"room_name"`
}

type recordingBody struct {
	On bool `json:"on"`
}

func (h *Calls) Start(w http.ResponseWriter, r *http.Request) {
	u, ok := requestUser(w, r)
	if !ok || !h.enabled(w) {
		return
	}
	containerID, err := pathUUID(r, "id")
	if err != nil {
		WriteError(w, err)
		return
	}
	member, err := h.app.DB.IsMember(r.Context(), containerID, u.ID)
	if err != nil {
		WriteError(w, err)
		return
	}
	if !member {
		WriteError(w, forbiddenf("you are not a member of this container"))
		return
	}
	c, err := h.app.DB.GetContainer(r.Context(), containerID)
	if err != nil {
		WriteError(w, err)
		return
	}
	if c.ArchivedAt != nil {
		WriteError(w, conflictf("container %s is archived", containerID))
		return
	}
	call, created, err := h.app.DB.StartCall(r.Context(), containerID, u.ID, "call_"+uuid.NewString())
	if err != nil {
		WriteError(w, err)
		return
	}
	if !created {
		WriteJSON(w, http.StatusOK, call)
		return
	}
	broadcastTo(r.Context(), h.app, containerID, socket.NewFrame(socket.TypeCallStarted, call))
	if _, err := h.app.PostSystem(r.Context(), containerID, u.ID, nil, "@"+u.Handle+" started a call"); err != nil {
		h.app.Log.Error().Err(err).Str("call_id", call.ID.String()).Msg("post call notice")
	}
	WriteJSON(w, http.StatusCreated, call)
}

func (h *Calls) Token(w http.ResponseWriter, r *http.Request) {
	call, u, ok := h.memberCall(w, r)
	if !ok || !h.enabled(w) {
		return
	}
	if call.EndedAt != nil {
		WriteError(w, conflictf("call %s has ended", call.ID))
		return
	}
	token, err := h.app.Calls.Token(call.RoomName, u)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, joinToken{
		Token:    token,
		URL:      h.app.Calls.PublicURL(),
		RoomName: call.RoomName,
	})
}

func (h *Calls) Leave(w http.ResponseWriter, r *http.Request) {
	call, u, ok := h.memberCall(w, r)
	if !ok {
		return
	}
	if err := h.app.DB.LeaveCall(r.Context(), call.ID, u.ID); err != nil {
		WriteError(w, err)
		return
	}
	broadcastTo(r.Context(), h.app, call.ContainerID, participantFrame(call.ID, u.ID, false))
	writeOK(w)
}

func (h *Calls) Recording(w http.ResponseWriter, r *http.Request) {
	call, _, ok := h.memberCall(w, r)
	if !ok || !h.enabled(w) {
		return
	}
	if call.EndedAt != nil {
		WriteError(w, conflictf("call %s has ended", call.ID))
		return
	}
	var body recordingBody
	if err := ReadJSON(r, &body); err != nil {
		WriteError(w, err)
		return
	}
	state := store.RecordingOff
	if body.On {
		state = store.RecordingActive
		participants, err := h.app.DB.LiveCallParticipants(r.Context(), call.ID)
		if err != nil {
			WriteError(w, err)
			return
		}
		for _, userID := range participants {
			h.startEgress(r.Context(), call, userID)
		}
	} else {
		h.stopEgress(r.Context(), call.RoomName)
	}
	if err := h.app.DB.SetRecordingState(r.Context(), call.ID, state); err != nil {
		WriteError(w, err)
		return
	}
	call.RecordingState = state
	WriteJSON(w, http.StatusOK, call)
}

func (h *Calls) Recordings(w http.ResponseWriter, r *http.Request) {
	call, _, ok := h.memberCall(w, r)
	if !ok {
		return
	}
	list, err := h.app.DB.ListCallRecordings(r.Context(), call.ID)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, nonNil(list))
}

func (h *Calls) Webhook(w http.ResponseWriter, r *http.Request) {
	event, err := h.app.Calls.VerifyWebhook(r)
	if err != nil {
		WriteError(w, fmt.Errorf("%w: %v", ErrUnauthorized, err))
		return
	}
	if err := h.handleEvent(r.Context(), event); err != nil {
		h.app.Log.Error().Err(err).Str("event", event.Event).Msg("handle livekit webhook")
	}
	writeOK(w)
}

func (h *Calls) handleEvent(ctx context.Context, e *livekit.WebhookEvent) error {
	switch e.Event {
	case eventParticipantJoined:
		call, userID, err := h.eventCall(ctx, e)
		if err != nil {
			return err
		}
		if err := h.app.DB.JoinCall(ctx, call.ID, userID); err != nil {
			return err
		}
		broadcastTo(ctx, h.app, call.ContainerID, participantFrame(call.ID, userID, true))
		if call.RecordingState == store.RecordingActive {
			h.startEgress(ctx, call, userID)
		}
	case eventParticipantLeft:
		call, userID, err := h.eventCall(ctx, e)
		if err != nil {
			return err
		}
		if err := h.app.DB.LeaveCall(ctx, call.ID, userID); err != nil {
			return err
		}
		broadcastTo(ctx, h.app, call.ContainerID, participantFrame(call.ID, userID, false))
	case eventRoomFinished:
		call, err := h.app.DB.CallByRoom(ctx, e.GetRoom().GetName())
		if err != nil {
			return err
		}
		h.stopEgress(ctx, call.RoomName)
		if call.RecordingState == store.RecordingActive {
			if err := h.app.DB.SetRecordingState(ctx, call.ID, store.RecordingProcessing); err != nil {
				return err
			}
		}
		if err := h.app.DB.EndCall(ctx, call.ID); err != nil {
			return err
		}
		broadcastTo(ctx, h.app, call.ContainerID,
			socket.NewFrame(socket.TypeCallEnded, socket.CallEndedPayload{CallID: call.ID}))
	}
	return nil
}

func (h *Calls) eventCall(ctx context.Context, e *livekit.WebhookEvent) (store.Call, uuid.UUID, error) {
	userID, err := uuid.Parse(e.GetParticipant().GetIdentity())
	if err != nil {
		return store.Call{}, uuid.Nil, fmt.Errorf("parse participant identity: %w", err)
	}
	call, err := h.app.DB.CallByRoom(ctx, e.GetRoom().GetName())
	if err != nil {
		return store.Call{}, uuid.Nil, err
	}
	return call, userID, nil
}

func (h *Calls) memberCall(w http.ResponseWriter, r *http.Request) (store.Call, store.User, bool) {
	u, ok := requestUser(w, r)
	if !ok {
		return store.Call{}, store.User{}, false
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		WriteError(w, err)
		return store.Call{}, store.User{}, false
	}
	call, err := h.app.DB.GetCall(r.Context(), id)
	if err != nil {
		WriteError(w, err)
		return store.Call{}, store.User{}, false
	}
	member, err := h.app.DB.IsMember(r.Context(), call.ContainerID, u.ID)
	if err != nil {
		WriteError(w, err)
		return store.Call{}, store.User{}, false
	}
	if !member {
		WriteError(w, forbiddenf("you are not a member of this container"))
		return store.Call{}, store.User{}, false
	}
	return call, u, true
}

func (h *Calls) startEgress(ctx context.Context, call store.Call, userID uuid.UUID) {
	name := userID.String() + ".ogg"
	// Egress writes from its own container, which mounts the recordings directory at /out rather than at media.root.
	egressID, err := h.app.Calls.StartTrackEgress(ctx, call.RoomName, userID.String(),
		path.Join(egressOutRoot, call.ID.String(), name))
	if err != nil {
		h.app.Log.Error().Err(err).Str("call_id", call.ID.String()).
			Str("user_id", userID.String()).Msg("start track egress")
		return
	}
	_, err = h.app.DB.CreateCallRecording(ctx, store.CallRecording{
		CallID:      call.ID,
		UserID:      &userID,
		EgressID:    &egressID,
		StoragePath: filepath.Join(h.app.Media.RecordingsDir(), call.ID.String(), name),
	})
	if err != nil {
		h.app.Log.Error().Err(err).Str("egress_id", egressID).Msg("record track egress")
	}
}

func (h *Calls) stopEgress(ctx context.Context, roomName string) {
	ids, err := h.app.Calls.ListEgress(ctx, roomName)
	if err != nil {
		h.app.Log.Error().Err(err).Str("room", roomName).Msg("list egress")
		return
	}
	for _, id := range ids {
		if err := h.app.Calls.StopEgress(ctx, id); err != nil {
			h.app.Log.Error().Err(err).Str("egress_id", id).Msg("stop egress")
		}
	}
}

func broadcastTo(ctx context.Context, a *app.App, containerID uuid.UUID, f socket.Frame) {
	members, err := a.DB.MemberIDs(ctx, containerID)
	if err != nil {
		a.Log.Error().Err(err).Str("container_id", containerID.String()).Msg("resolve broadcast members")
		return
	}
	a.Hub.ToUsers(members, f)
}

func (h *Calls) enabled(w http.ResponseWriter) bool {
	if h.app.Calls.Enabled() {
		return true
	}
	WriteJSON(w, http.StatusServiceUnavailable,
		errorBody{Error: "calls are unavailable: livekit is not configured"})
	return false
}

func participantFrame(callID, userID uuid.UUID, joined bool) socket.Frame {
	return socket.NewFrame(socket.TypeCallParticipant,
		socket.CallParticipantPayload{CallID: callID, UserID: userID, Joined: joined})
}
