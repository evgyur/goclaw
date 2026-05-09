package mediajobs

import (
	"errors"
	"testing"
	"time"
)

func TestIdempotencyKeyIsStableAndModelAware(t *testing.T) {
	base := Spec{
		SourceEventID: "telegram:-1001:42:photo:0",
		MediaKind:     KindImage,
		ArtifactHash:  "ABCDEF",
		Provider:      "MiniMax",
		Model:         "MiniMax-VL-01",
		Mode:          "image_observation",
	}

	got := IdempotencyKey(base)
	again := IdempotencyKey(base)
	if got != again {
		t.Fatalf("key not stable: %q != %q", got, again)
	}

	changed := base
	changed.Model = "MiniMax-VL-02"
	if got == IdempotencyKey(changed) {
		t.Fatal("model change must produce a distinct key for reprocessing")
	}
}

func TestNewJobNormalizesRequiredFields(t *testing.T) {
	now := time.Date(2026, 5, 9, 11, 0, 0, 0, time.UTC)
	job, err := New(Spec{
		SourceEventID: "telegram:-1001:42:voice:0",
		MediaKind:     " Voice ",
		ArtifactHash:  "ABCDEF",
		Provider:      " GROQ ",
		Model:         "whisper-large-v3-turbo",
		Mode:          " transcription ",
		Now:           now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != StatusPending {
		t.Fatalf("status = %q, want %q", job.Status, StatusPending)
	}
	if job.AttemptCount != 0 {
		t.Fatalf("attempt_count = %d, want 0", job.AttemptCount)
	}
	if job.MediaKind != KindVoice || job.Provider != "groq" || job.Mode != "transcription" {
		t.Fatalf("normalization failed: %#v", job)
	}
	if job.CreatedAt != now {
		t.Fatalf("created_at = %s, want %s", job.CreatedAt, now)
	}
}

func TestRetryableFailureBecomesTerminalAtMaxAttempts(t *testing.T) {
	now := time.Date(2026, 5, 9, 11, 0, 0, 0, time.UTC)
	job, err := New(Spec{
		SourceEventID: "telegram:-1001:99:audio:0",
		MediaKind:     KindAudio,
		ArtifactHash:  "hash",
		Provider:      "groq",
		Model:         "whisper-large-v3-turbo",
		Mode:          "transcription",
		Now:           now,
	})
	if err != nil {
		t.Fatal(err)
	}

	job = job.Start(now)
	job = job.Fail(errors.New("provider timeout"), 3, now)
	if job.Status != StatusFailedRetryable {
		t.Fatalf("status after attempt 1 = %q, want %q", job.Status, StatusFailedRetryable)
	}
	if job.CompletedAt != nil {
		t.Fatal("retryable failure must not set completed_at")
	}

	job = job.Start(now)
	job = job.Fail(errors.New("provider timeout"), 3, now)
	if job.Status != StatusFailedRetryable {
		t.Fatalf("status after attempt 2 = %q, want %q", job.Status, StatusFailedRetryable)
	}

	job = job.Start(now)
	job = job.Fail(errors.New("provider timeout"), 3, now)
	if job.Status != StatusFailedTerminal {
		t.Fatalf("status after attempt 3 = %q, want %q", job.Status, StatusFailedTerminal)
	}
	if job.CompletedAt == nil {
		t.Fatal("terminal failure must set completed_at")
	}
}

func TestSpecValidationRequiresAllIdempotencyInputs(t *testing.T) {
	_, err := New(Spec{SourceEventID: "telegram:-1001:1"})
	if err == nil {
		t.Fatal("expected validation error")
	}
}
