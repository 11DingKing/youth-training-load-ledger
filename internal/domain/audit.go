package domain

import "time"

type AuditEvent struct {
	ID         int64     `json:"id"`
	ActorID    int64     `json:"actor_id"`
	Action     string    `json:"action"`
	ObjectType string    `json:"object_type"`
	ObjectID   int64     `json:"object_id"`
	Outcome    string    `json:"outcome"`
	Basis      string    `json:"basis,omitempty"`
	RequestID  string    `json:"request_id"`
	Metadata   string    `json:"metadata_json"`
	CreatedAt  time.Time `json:"created_at"`
}

func (e AuditEvent) Validate() error {
	if e.ActorID <= 0 || e.Action == "" || e.ObjectType == "" || e.ObjectID <= 0 {
		return FieldError{Field: "audit", Problem: "actor, action, object type and object id are required"}
	}
	if e.Outcome != "success" && e.Outcome != "rejected" && e.Outcome != "failed" {
		return FieldError{Field: "outcome", Problem: "unsupported audit outcome"}
	}
	if e.RequestID == "" {
		return FieldError{Field: "request_id", Problem: "is required"}
	}
	return nil
}

type StatusTransition struct {
	ID         int64     `json:"id"`
	AthleteID  int64     `json:"athlete_id"`
	ActorID    int64     `json:"actor_id"`
	FromStatus string    `json:"from_status"`
	ToStatus   string    `json:"to_status"`
	Reason     string    `json:"reason"`
	RequestID  string    `json:"request_id"`
	OccurredAt time.Time `json:"occurred_at"`
}

type JobStatus string

const (
	JobPending         JobStatus = "pending"
	JobRunning         JobStatus = "running"
	JobRetry           JobStatus = "retry"
	JobSucceeded       JobStatus = "succeeded"
	JobPermanentFailed JobStatus = "permanent_failed"
)

type WorkerJob struct {
	ID          int64      `json:"id"`
	Kind        string     `json:"kind"`
	Payload     string     `json:"payload_json"`
	Status      JobStatus  `json:"status"`
	Attempts    int        `json:"attempts"`
	MaxAttempts int        `json:"max_attempts"`
	AvailableAt time.Time  `json:"available_at"`
	LeaseOwner  string     `json:"lease_owner,omitempty"`
	LeaseUntil  *time.Time `json:"lease_until,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (j WorkerJob) CanRun(at time.Time) bool {
	if j.Status != JobPending && j.Status != JobRetry && j.Status != JobRunning {
		return false
	}
	if j.AvailableAt.After(at) {
		return false
	}
	return j.LeaseUntil == nil || !j.LeaseUntil.After(at)
}
