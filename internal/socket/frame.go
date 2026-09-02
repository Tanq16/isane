package socket

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"time"
	"uuid"

	"github.com/rs/zerolog/log"
	"github.com/tanq16/isane/internal/store"
)

const (
	TypeHello      = "hello"
	TypeSend       = "send"
	TypeEdit       = "edit"
	TypeDelete     = "delete"
	TypeRead       = "read"
	TypeTyping     = "typing"
	TypeThreadSub  = "thread_sub"
	TypeVisibility = "visibility"
	TypePing       = "ping"

	TypeReady           = "ready"
	TypeMessage         = "message"
	TypeMessageEdited   = "message_edited"
	TypeMessageDeleted  = "message_deleted"
	TypePresence        = "presence"
	TypeCallStarted     = "call_started"
	TypeCallParticipant = "call_participant"
	TypeCallRecording   = "call_recording"
	TypeCallEnded       = "call_ended"
	TypeAgentWorking    = "agent_working"
	TypeAgentDone       = "agent_done"
	TypeSettings        = "settings"
	TypeContainer       = "container"
	TypeUser            = "user"
	TypeAttachment      = "attachment"
	TypePong            = "pong"
	TypeError           = "error"
)

const (
	CodeForbidden = "forbidden"
	CodeConflict  = "conflict"
	CodeNotFound  = "not_found"
	CodeInvalid   = "invalid"
	CodeInternal  = "internal"
)

type Frame struct {
	T string         `json:"t"`
	D jsontext.Value `json:"d,omitempty"`
}

func NewFrame(t string, d any) Frame {
	if d == nil {
		return Frame{T: t}
	}
	raw, err := json.Marshal(d)
	if err != nil {
		log.Error().Err(err).Str("type", t).Msg("encode socket frame payload")
		return Frame{T: t}
	}
	return Frame{T: t, D: raw}
}

type HelloPayload struct {
	Cursors      map[uuid.UUID]int64 `json:"cursors"`
	PushEndpoint string              `json:"push_endpoint,omitempty"`
	Visible      bool                `json:"visible"`
}

type VisibilityPayload struct {
	Visible bool `json:"visible"`
}

type SendPayload struct {
	ContainerID   uuid.UUID   `json:"container_id"`
	ClientID      string      `json:"client_id"`
	Body          string      `json:"body"`
	ReplyToID     *uuid.UUID  `json:"reply_to_id,omitempty"`
	ThreadRootID  *uuid.UUID  `json:"thread_root_id,omitempty"`
	AttachmentIDs []uuid.UUID `json:"attachment_ids,omitempty"`
}

type EditPayload struct {
	MessageID uuid.UUID `json:"message_id"`
	Body      string    `json:"body"`
}

type DeletePayload struct {
	MessageID uuid.UUID `json:"message_id"`
}

type ReadPayload struct {
	ContainerID uuid.UUID `json:"container_id"`
	Seq         int64     `json:"seq"`
}

type TypingPayload struct {
	ContainerID  uuid.UUID  `json:"container_id"`
	ThreadRootID *uuid.UUID `json:"thread_root_id,omitempty"`
}

type ThreadSubPayload struct {
	ThreadRootID uuid.UUID            `json:"thread_root_id"`
	State        store.ThreadSubState `json:"state"`
}

type TypingEventPayload struct {
	ContainerID  uuid.UUID  `json:"container_id"`
	UserID       uuid.UUID  `json:"user_id"`
	ThreadRootID *uuid.UUID `json:"thread_root_id,omitempty"`
}

type MessageEditedPayload struct {
	ID       uuid.UUID  `json:"id"`
	Body     string     `json:"body"`
	EditedAt *time.Time `json:"edited_at,omitempty"`
}

type MessageDeletedPayload struct {
	ID          uuid.UUID `json:"id"`
	ContainerID uuid.UUID `json:"container_id"`
	Seq         int64     `json:"seq"`
}

type PresencePayload struct {
	UserID uuid.UUID `json:"user_id"`
	Online bool      `json:"online"`
}

type CallParticipantPayload struct {
	CallID uuid.UUID `json:"call_id"`
	UserID uuid.UUID `json:"user_id"`
	Joined bool      `json:"joined"`
}

type CallRecordingPayload struct {
	CallID uuid.UUID            `json:"call_id"`
	State  store.RecordingState `json:"recording_state"`
}

type CallEndedPayload struct {
	CallID uuid.UUID `json:"call_id"`
}

type AgentWorkingPayload struct {
	ContainerID uuid.UUID `json:"container_id"`
	AgentID     uuid.UUID `json:"agent_id"`
	JobID       uuid.UUID `json:"job_id"`
}

type AgentDonePayload struct {
	JobID uuid.UUID `json:"job_id"`
}

type ErrorPayload struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	ClientID string `json:"client_id,omitempty"`
}
