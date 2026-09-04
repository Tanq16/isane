package agentproto

import (
	"time"
	"uuid"
)

type JobState string

const (
	JobQueued     JobState = "queued"
	JobDispatched JobState = "dispatched"
	JobDone       JobState = "done"
	JobFailed     JobState = "failed"
	JobTimeout    JobState = "timeout"
)

type Job struct {
	ID           uuid.UUID  `json:"id"`
	AgentID      uuid.UUID  `json:"agent_id"`
	ContainerID  uuid.UUID  `json:"container_id"`
	TriggerMsgID uuid.UUID  `json:"trigger_msg_id"`
	State        JobState   `json:"state"`
	Prompt       string     `json:"prompt"`
	Result       *string    `json:"result,omitempty"`
	Error        *string    `json:"error,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	DispatchedAt *time.Time `json:"dispatched_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
}

type JobList struct {
	Jobs []Job `json:"jobs"`
}

type RegisterRequest struct {
	Handle       string   `json:"handle,omitempty"`
	ClaimToken   string   `json:"claim_token,omitempty"`
	Argv         []string `json:"argv"`
	AllowHistory bool     `json:"allow_history"`
}

type ResultRequest struct {
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}
