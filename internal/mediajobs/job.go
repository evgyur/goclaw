package mediajobs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	KindImage    = "image"
	KindAudio    = "audio"
	KindVoice    = "voice"
	KindVideo    = "video"
	KindDocument = "document"

	StatusPending         = "pending"
	StatusRunning         = "running"
	StatusDone            = "done"
	StatusFailedRetryable = "failed_retryable"
	StatusFailedTerminal  = "failed_terminal"
)

// Job is the durable contract for async media enrichment.
// It is intentionally provider-neutral so MiniMax, Groq, and video-grab workers
// can share the same idempotency and retry semantics.
type Job struct {
	JobID         string     `json:"job_id" db:"id"`
	SourceEventID string     `json:"source_event_id" db:"source_event_id"`
	MediaKind     string     `json:"media_kind" db:"media_kind"`
	ArtifactHash  string     `json:"artifact_hash" db:"artifact_hash"`
	Provider      string     `json:"provider" db:"provider"`
	Model         string     `json:"model" db:"model"`
	Mode          string     `json:"mode" db:"mode"`
	Status        string     `json:"status" db:"status"`
	AttemptCount  int        `json:"attempt_count" db:"attempt_count"`
	Error         string     `json:"error,omitempty" db:"error"`
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
	CompletedAt   *time.Time `json:"completed_at,omitempty" db:"completed_at"`
}

type Spec struct {
	SourceEventID string
	MediaKind     string
	ArtifactHash  string
	Provider      string
	Model         string
	Mode          string
	Now           time.Time
}

func New(spec Spec) (Job, error) {
	if err := validateSpec(spec); err != nil {
		return Job{}, err
	}
	now := spec.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	key := IdempotencyKey(spec)
	return Job{
		JobID:         key,
		SourceEventID: strings.TrimSpace(spec.SourceEventID),
		MediaKind:     normalize(spec.MediaKind),
		ArtifactHash:  normalize(spec.ArtifactHash),
		Provider:      normalize(spec.Provider),
		Model:         strings.TrimSpace(spec.Model),
		Mode:          normalize(spec.Mode),
		Status:        StatusPending,
		AttemptCount:  0,
		CreatedAt:     now,
	}, nil
}

func IdempotencyKey(spec Spec) string {
	parts := []string{
		strings.TrimSpace(spec.SourceEventID),
		normalize(spec.ArtifactHash),
		normalize(spec.Provider),
		strings.TrimSpace(spec.Model),
		normalize(spec.Mode),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return "mediajob_" + hex.EncodeToString(sum[:16])
}

func (j Job) Start(now time.Time) Job {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	j.Status = StatusRunning
	j.AttemptCount++
	j.Error = ""
	return j
}

func (j Job) Complete(now time.Time) Job {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	j.Status = StatusDone
	j.Error = ""
	j.CompletedAt = &now
	return j
}

func (j Job) Fail(err error, maxAttempts int, now time.Time) Job {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err != nil {
		j.Error = err.Error()
	}
	if maxAttempts <= 0 || j.AttemptCount < maxAttempts {
		j.Status = StatusFailedRetryable
		j.CompletedAt = nil
		return j
	}
	j.Status = StatusFailedTerminal
	j.CompletedAt = &now
	return j
}

func validateSpec(spec Spec) error {
	missing := make([]string, 0, 6)
	if strings.TrimSpace(spec.SourceEventID) == "" {
		missing = append(missing, "source_event_id")
	}
	if strings.TrimSpace(spec.MediaKind) == "" {
		missing = append(missing, "media_kind")
	}
	if strings.TrimSpace(spec.ArtifactHash) == "" {
		missing = append(missing, "artifact_hash")
	}
	if strings.TrimSpace(spec.Provider) == "" {
		missing = append(missing, "provider")
	}
	if strings.TrimSpace(spec.Model) == "" {
		missing = append(missing, "model")
	}
	if strings.TrimSpace(spec.Mode) == "" {
		missing = append(missing, "mode")
	}
	if len(missing) > 0 {
		return fmt.Errorf("media job spec missing %s", strings.Join(missing, ", "))
	}
	return nil
}

func normalize(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
