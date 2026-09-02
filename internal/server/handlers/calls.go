package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"time"
	"uuid"

	"github.com/livekit/protocol/livekit"
	"github.com/tanq16/isane/internal/app"
	"github.com/tanq16/isane/internal/socket"
	"github.com/tanq16/isane/internal/store"
)

const (
	egressOutRoot          = "/out"
	recordingMime          = "audio/mp4"
	eventParticipantJoined = "participant_joined"
	eventParticipantLeft   = "participant_left"
	eventRoomFinished      = "room_finished"
	eventEgressEnded       = "egress_ended"
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
	call, created, err := h.app.DB.StartCall(r.Context(), containerID, u.ID, "call_"+uuid.New().String())
	if err != nil {
		WriteError(w, err)
		return
	}
	if !created {
		WriteJSON(w, http.StatusOK, call)
		return
	}
	h.app.Broadcast(r.Context(), containerID, socket.NewFrame(socket.TypeCallStarted, call))
	if _, err := h.app.PostCallNotice(r.Context(), containerID, u.ID, call.ID, "@"+u.Handle+" started a call"); err != nil {
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
	h.app.Broadcast(r.Context(), call.ContainerID, participantFrame(call.ID, u.ID, false))
	h.endIfEmpty(r.Context(), call)
	writeOK(w)
}

func (h *Calls) Recording(w http.ResponseWriter, r *http.Request) {
	call, u, ok := h.memberCall(w, r)
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
		if call.RecordingState == store.RecordingActive {
			WriteError(w, conflictf("call %s is already recording", call.ID))
			return
		}
		if err := h.startRecording(r.Context(), call, u.ID); err != nil {
			h.app.Log.Error().Err(err).Str("call_id", call.ID.String()).Msg("start recording")
			WriteJSON(w, http.StatusServiceUnavailable,
				errorBody{Error: "the recording could not be started; this host records one call at a time"})
			return
		}
		state = store.RecordingActive
	} else if h.stopEgress(r.Context(), call.RoomName) > 0 {
		state = store.RecordingProcessing
	}
	if err := h.setRecording(r.Context(), call, state); err != nil {
		WriteError(w, err)
		return
	}
	call.RecordingState = state
	WriteJSON(w, http.StatusOK, call)
}

func (h *Calls) Audio(w http.ResponseWriter, r *http.Request) {
	u, ok := requestUser(w, r)
	if !ok {
		return
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		WriteError(w, err)
		return
	}
	rec, err := h.app.DB.GetCallRecording(r.Context(), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	call, err := h.app.DB.GetCall(r.Context(), rec.CallID)
	if err != nil {
		WriteError(w, err)
		return
	}
	if _, err := containerFor(r.Context(), h.app, call.ContainerID, u.ID); err != nil {
		WriteError(w, err)
		return
	}
	f, err := h.app.Media.OpenRecording(rec)
	if err != nil {
		WriteError(w, err)
		return
	}
	w.Header().Set("Cache-Control", attachmentCache)
	serveFile(w, r, f, filepath.Base(rec.StoragePath), rec.Mime, queryBool(r, "download"))
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
		h.app.Broadcast(ctx, call.ContainerID, participantFrame(call.ID, userID, true))
	case eventParticipantLeft:
		call, userID, err := h.eventCall(ctx, e)
		if err != nil {
			return err
		}
		if err := h.app.DB.LeaveCall(ctx, call.ID, userID); err != nil {
			return err
		}
		h.app.Broadcast(ctx, call.ContainerID, participantFrame(call.ID, userID, false))
		h.endIfEmpty(ctx, call)
	case eventRoomFinished:
		call, err := h.app.DB.CallByRoom(ctx, e.GetRoom().GetName())
		if err != nil {
			return err
		}
		h.stopEgress(ctx, call.RoomName)
		if call.RecordingState == store.RecordingActive {
			if err := h.setRecording(ctx, call, store.RecordingProcessing); err != nil {
				return err
			}
		}
		if err := h.app.DB.EndCall(ctx, call.ID); err != nil {
			return err
		}
		h.app.Broadcast(ctx, call.ContainerID,
			socket.NewFrame(socket.TypeCallEnded, socket.CallEndedPayload{CallID: call.ID}))
	case eventEgressEnded:
		return h.finishEgress(ctx, e.GetEgressInfo())
	}
	return nil
}

func (h *Calls) finishEgress(ctx context.Context, info *livekit.EgressInfo) error {
	if info == nil || info.GetEgressId() == "" {
		return nil
	}
	rec, err := h.app.DB.RecordingByEgressID(ctx, info.GetEgressId())
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	call, err := h.app.DB.CallByRoom(ctx, info.GetRoomName())
	if err != nil {
		return err
	}
	files := info.GetFileResults()
	if info.GetStatus() != livekit.EgressStatus_EGRESS_COMPLETE || len(files) != 1 || files[0].GetSize() == 0 {
		h.app.Log.Error().Str("egress_id", info.GetEgressId()).Str("status", info.GetStatus().String()).
			Msg("recording did not complete")
		return h.setRecording(ctx, call, store.RecordingFailed)
	}
	stat, err := os.Stat(rec.StoragePath)
	if err != nil || stat.Size() == 0 {
		h.app.Log.Error().Err(err).Str("recording", rec.ID.String()).Msg("recording file is missing or empty")
		return h.setRecording(ctx, call, store.RecordingFailed)
	}
	var duration *int
	if ms := int(files[0].GetDuration() / int64(time.Millisecond)); ms > 0 {
		duration = &ms
	}
	if err := h.app.DB.FinishCallRecording(ctx, rec.ID, duration, files[0].GetSize()); err != nil {
		return err
	}
	if err := h.postRecordingNotice(ctx, call, rec, duration); err != nil {
		return err
	}
	return h.setRecording(ctx, call, store.RecordingReady)
}

func (h *Calls) postRecordingNotice(ctx context.Context, call store.Call, rec store.CallRecording, duration *int) error {
	authorID := call.StartedBy
	if rec.UserID != nil {
		authorID = *rec.UserID
	}
	author, err := h.app.DB.GetUser(ctx, authorID)
	if err != nil {
		return err
	}
	body := "@" + author.Handle + " recorded the call"
	if duration != nil {
		body = "@" + author.Handle + " recorded " + recordingLength(*duration) + " of the call"
	}
	_, err = h.app.PostRecordingNotice(ctx, call.ContainerID, author.ID, rec.ID, body)
	if errors.Is(err, store.ErrConflict) {
		return nil
	}
	return err
}

func recordingLength(ms int) string {
	d := (time.Duration(ms) * time.Millisecond).Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
}

func (h *Calls) eventCall(ctx context.Context, e *livekit.WebhookEvent) (store.Call, uuid.UUID, error) {
	userID, err := uuid.Parse(e.GetParticipant().GetIdentity())
	if err != nil {
		return store.Call{}, uuid.Nil(), fmt.Errorf("parse participant identity: %w", err)
	}
	call, err := h.app.DB.CallByRoom(ctx, e.GetRoom().GetName())
	if err != nil {
		return store.Call{}, uuid.Nil(), err
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

func (h *Calls) startRecording(ctx context.Context, call store.Call, startedBy uuid.UUID) error {
	recID := uuid.New()
	// Egress writes from its own container, which mounts the recordings directory at /out rather than at media.root.
	egressID, err := h.app.Calls.StartRoomEgress(ctx, call.RoomName,
		path.Join(egressOutRoot, h.app.Media.RecordingName(call.ID, recID)))
	if err != nil {
		return err
	}
	_, err = h.app.DB.CreateCallRecording(ctx, store.CallRecording{
		ID:          recID,
		CallID:      call.ID,
		UserID:      &startedBy,
		EgressID:    &egressID,
		StoragePath: h.app.Media.RecordingPath(call.ID, recID),
		Mime:        recordingMime,
	})
	if err != nil {
		if stopErr := h.app.Calls.StopEgress(ctx, egressID); stopErr != nil {
			h.app.Log.Error().Err(stopErr).Str("egress_id", egressID).Msg("stop orphaned egress")
		}
		return err
	}
	return nil
}

func (h *Calls) setRecording(ctx context.Context, call store.Call, state store.RecordingState) error {
	if err := h.app.DB.SetRecordingState(ctx, call.ID, state); err != nil {
		return err
	}
	h.app.Broadcast(ctx, call.ContainerID, socket.NewFrame(socket.TypeCallRecording,
		socket.CallRecordingPayload{CallID: call.ID, State: state}))
	return nil
}

func (h *Calls) endIfEmpty(ctx context.Context, call store.Call) {
	ended, err := h.app.DB.EndCallIfEmpty(ctx, call.ID)
	if err != nil {
		h.app.Log.Error().Err(err).Str("call_id", call.ID.String()).Msg("end empty call")
		return
	}
	if !ended {
		return
	}
	h.stopEgress(ctx, call.RoomName)
	if call.RecordingState == store.RecordingActive {
		if err := h.setRecording(ctx, call, store.RecordingProcessing); err != nil {
			h.app.Log.Error().Err(err).Str("call_id", call.ID.String()).Msg("mark recording processing")
		}
	}
	if err := h.app.Calls.DeleteRoom(ctx, call.RoomName); err != nil {
		h.app.Log.Error().Err(err).Str("room", call.RoomName).Msg("delete livekit room")
	}
	h.app.Broadcast(ctx, call.ContainerID,
		socket.NewFrame(socket.TypeCallEnded, socket.CallEndedPayload{CallID: call.ID}))
}

func (h *Calls) stopEgress(ctx context.Context, roomName string) int {
	ids, err := h.app.Calls.ListEgress(ctx, roomName)
	if err != nil {
		h.app.Log.Error().Err(err).Str("room", roomName).Msg("list egress")
		return 0
	}
	stopped := 0
	for _, id := range ids {
		if err := h.app.Calls.StopEgress(ctx, id); err != nil {
			h.app.Log.Error().Err(err).Str("egress_id", id).Msg("stop egress")
			continue
		}
		stopped++
	}
	return stopped
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
