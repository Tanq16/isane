package store

import (
	"net/netip"
	"time"
	"uuid"
)

type UserKind string

const (
	UserHuman UserKind = "human"
	UserAgent UserKind = "agent"
)

type ContainerKind string

const (
	ContainerChannel      ContainerKind = "channel"
	ContainerConversation ContainerKind = "conversation"
)

type NotificationLevel string

const (
	LevelAll      NotificationLevel = "all"
	LevelMentions NotificationLevel = "mentions"
	LevelNone     NotificationLevel = "none"
)

type ThreadSubState string

const (
	ThreadSubscribed ThreadSubState = "subscribed"
	ThreadMuted      ThreadSubState = "muted"
)

type AttachmentKind string

const (
	AttachmentImage AttachmentKind = "image"
	AttachmentVideo AttachmentKind = "video"
	AttachmentAudio AttachmentKind = "audio"
	AttachmentFile  AttachmentKind = "file"
)

type AttachmentState string

const (
	AttachmentStaged     AttachmentState = "staged"
	AttachmentProcessing AttachmentState = "processing"
	AttachmentReady      AttachmentState = "ready"
	AttachmentFailed     AttachmentState = "failed"
)

type AgentState string

const (
	AgentReserved AgentState = "reserved"
	AgentServing  AgentState = "serving"
)

type AgentJobState string

const (
	JobQueued     AgentJobState = "queued"
	JobDispatched AgentJobState = "dispatched"
	JobDone       AgentJobState = "done"
	JobFailed     AgentJobState = "failed"
	JobTimeout    AgentJobState = "timeout"
)

type RecordingState string

const (
	RecordingOff        RecordingState = "off"
	RecordingActive     RecordingState = "recording"
	RecordingProcessing RecordingState = "processing"
	RecordingReady      RecordingState = "ready"
	RecordingFailed     RecordingState = "failed"
)

type User struct {
	ID            uuid.UUID  `json:"id"`
	Kind          UserKind   `json:"kind"`
	Handle        string     `json:"handle"`
	DisplayName   string     `json:"display_name"`
	AvatarID      *uuid.UUID `json:"avatar_id,omitempty"`
	Email         *string    `json:"email,omitempty"`
	PasswordHash  *string    `json:"-"`
	IsAdmin       bool       `json:"is_admin"`
	CreatedAt     time.Time  `json:"created_at"`
	DeactivatedAt *time.Time `json:"deactivated_at,omitempty"`
}

func (u User) Active() bool { return u.DeactivatedAt == nil }

type Session struct {
	ID         uuid.UUID
	TokenHash  []byte
	UserID     uuid.UUID
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
	UserAgent  *string
	IP         *netip.Addr
}

type Invite struct {
	TokenHash []byte     `json:"-"`
	CreatedBy uuid.UUID  `json:"created_by"`
	Note      *string    `json:"note,omitempty"`
	ExpiresAt time.Time  `json:"expires_at"`
	UsedBy    *uuid.UUID `json:"used_by,omitempty"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type ServerSettings struct {
	AllowMemberChannels bool       `json:"allow_member_channels"`
	UpdatedAt           time.Time  `json:"updated_at"`
	UpdatedBy           *uuid.UUID `json:"updated_by,omitempty"`
}

type Container struct {
	ID         uuid.UUID     `json:"id"`
	Kind       ContainerKind `json:"kind"`
	Slug       *string       `json:"slug,omitempty"`
	Name       *string       `json:"name,omitempty"`
	Topic      *string       `json:"topic,omitempty"`
	LastSeq    int64         `json:"last_seq"`
	CreatedBy  uuid.UUID     `json:"created_by"`
	CreatedAt  time.Time     `json:"created_at"`
	ArchivedAt *time.Time    `json:"archived_at,omitempty"`
}

type ContainerView struct {
	Container
	Unread       int64             `json:"unread"`
	Mentions     int64             `json:"mentions"`
	Level        NotificationLevel `json:"level"`
	Participants []uuid.UUID       `json:"participants,omitempty"`
}

type Message struct {
	ID                uuid.UUID    `json:"id"`
	ContainerID       uuid.UUID    `json:"container_id"`
	Seq               int64        `json:"seq"`
	AuthorID          uuid.UUID    `json:"author_id"`
	Body              string       `json:"body"`
	ReplyToID         *uuid.UUID   `json:"reply_to_id,omitempty"`
	ThreadRootID      *uuid.UUID   `json:"thread_root_id,omitempty"`
	ClientID          string       `json:"client_id"`
	IsSystem          bool         `json:"is_system"`
	ThreadReplyCount  int          `json:"thread_reply_count"`
	ThreadLastReplyAt *time.Time   `json:"thread_last_reply_at,omitempty"`
	CreatedAt         time.Time    `json:"created_at"`
	EditedAt          *time.Time   `json:"edited_at,omitempty"`
	DeletedAt         *time.Time   `json:"deleted_at,omitempty"`
	Attachments       []Attachment `json:"attachments,omitempty"`
	Mentions          []uuid.UUID  `json:"mentions,omitempty"`
}

type NewMessage struct {
	ContainerID   uuid.UUID
	AuthorID      uuid.UUID
	Body          string
	ClientID      string
	ReplyToID     *uuid.UUID
	ThreadRootID  *uuid.UUID
	IsSystem      bool
	AttachmentIDs []uuid.UUID
	MentionIDs    []uuid.UUID
}

type Attachment struct {
	ID           uuid.UUID       `json:"id"`
	MessageID    *uuid.UUID      `json:"message_id,omitempty"`
	UploaderID   uuid.UUID       `json:"uploader_id"`
	Kind         AttachmentKind  `json:"kind"`
	OriginalName string          `json:"original_name"`
	Mime         string          `json:"mime"`
	SizeBytes    int64           `json:"size_bytes"`
	Width        *int            `json:"width,omitempty"`
	Height       *int            `json:"height,omitempty"`
	DurationMs   *int            `json:"duration_ms,omitempty"`
	StoragePath  string          `json:"-"`
	ThumbPath    *string         `json:"-"`
	State        AttachmentState `json:"state"`
	Error        *string         `json:"error,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
}

func (a Attachment) HasThumb() bool { return a.ThumbPath != nil && *a.ThumbPath != "" }

type Agent struct {
	UserID         uuid.UUID  `json:"user_id"`
	State          AgentState `json:"state"`
	ClaimTokenHash []byte     `json:"-"`
	AllowHistory   bool       `json:"allow_history"`
	Argv           []string   `json:"argv,omitempty"`
	RegisteredAt   *time.Time `json:"registered_at,omitempty"`
	LastSeenAt     *time.Time `json:"last_seen_at,omitempty"`
}

type AgentJob struct {
	ID           uuid.UUID     `json:"id"`
	AgentID      uuid.UUID     `json:"agent_id"`
	ContainerID  uuid.UUID     `json:"container_id"`
	TriggerMsgID uuid.UUID     `json:"trigger_msg_id"`
	State        AgentJobState `json:"state"`
	Prompt       string        `json:"prompt"`
	Result       *string       `json:"result,omitempty"`
	Error        *string       `json:"error,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
	DispatchedAt *time.Time    `json:"dispatched_at,omitempty"`
	FinishedAt   *time.Time    `json:"finished_at,omitempty"`
}

type Call struct {
	ID             uuid.UUID      `json:"id"`
	ContainerID    uuid.UUID      `json:"container_id"`
	RoomName       string         `json:"room_name"`
	StartedBy      uuid.UUID      `json:"started_by"`
	StartedAt      time.Time      `json:"started_at"`
	EndedAt        *time.Time     `json:"ended_at,omitempty"`
	RecordingState RecordingState `json:"recording_state"`
	Participants   []uuid.UUID    `json:"participants,omitempty"`
}

type CallRecording struct {
	ID          uuid.UUID  `json:"id"`
	CallID      uuid.UUID  `json:"call_id"`
	UserID      *uuid.UUID `json:"user_id,omitempty"`
	EgressID    *string    `json:"-"`
	StoragePath string     `json:"-"`
	SizeBytes   int64      `json:"size_bytes"`
	DurationMs  *int       `json:"duration_ms,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type PushSubscription struct {
	ID            uuid.UUID  `json:"id"`
	UserID        uuid.UUID  `json:"user_id"`
	Endpoint      string     `json:"endpoint"`
	P256dh        string     `json:"-"`
	Auth          string     `json:"-"`
	UserAgent     *string    `json:"user_agent,omitempty"`
	Enabled       bool       `json:"enabled"`
	CreatedAt     time.Time  `json:"created_at"`
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
	LastFailureAt *time.Time `json:"last_failure_at,omitempty"`
}

type ReadMarker struct {
	UserID      uuid.UUID
	ContainerID uuid.UUID
	LastReadSeq int64
	UpdatedAt   time.Time
}

type SearchResult struct {
	Message Message `json:"message"`
	Rank    float32 `json:"rank"`
}

type Stats struct {
	Messages       int64 `json:"messages"`
	Users          int64 `json:"users"`
	Agents         int64 `json:"agents"`
	Channels       int64 `json:"channels"`
	Conversations  int64 `json:"conversations"`
	ActiveSessions int64 `json:"active_sessions"`
	MediaBytes     int64 `json:"media_bytes"`
	RecordingBytes int64 `json:"recording_bytes"`
	DatabaseBytes  int64 `json:"database_bytes"`
}

type AgentInfo struct {
	User  User  `json:"user"`
	Agent Agent `json:"agent"`
}
