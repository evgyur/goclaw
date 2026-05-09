package mediajobs

import (
	"errors"
	"testing"
	"time"
)

func TestIsExternalVideoURLRecognizesVideoPlatforms(t *testing.T) {
	for _, raw := range []string{
		"https://youtu.be/abc",
		"https://www.youtube.com/watch?v=abc",
		"https://www.instagram.com/reel/abc",
		"https://www.tiktok.com/@chip/video/123",
		"https://vimeo.com/123",
	} {
		if !IsExternalVideoURL(raw) {
			t.Fatalf("IsExternalVideoURL(%q) = false", raw)
		}
	}
	for _, raw := range []string{"", "not-a-url", "https://example.com/page"} {
		if IsExternalVideoURL(raw) {
			t.Fatalf("IsExternalVideoURL(%q) = true", raw)
		}
	}
}

func TestNewVideoEnrichmentPlanExternalURLAddsGrabTranscriptAndKeyframes(t *testing.T) {
	now := time.Date(2026, 5, 9, 11, 0, 0, 0, time.UTC)
	plan, err := NewVideoEnrichmentPlan("telegram:-1001:77", "https://youtu.be/abc", VideoArtifactRefs{
		VideoHash:      "sha256:video",
		AudioHash:      "sha256:audio",
		KeyframeHashes: []string{"sha256:kf1", "sha256:kf2"},
	}, now)
	if err != nil {
		t.Fatalf("NewVideoEnrichmentPlan: %v", err)
	}
	if !plan.External {
		t.Fatal("plan.External = false")
	}
	if len(plan.Jobs) != 4 {
		t.Fatalf("len(plan.Jobs) = %d, want 4: %#v", len(plan.Jobs), plan.Jobs)
	}
	assertJob(t, plan.Jobs[0], ProviderChipVideoGrab, ModelChipVideoGrab, KindVideo, ModeVideoGrab)
	assertJob(t, plan.Jobs[1], DefaultTranscriptionProvider, DefaultTranscriptionModel, KindAudio, ModeVideoAudioTranscript)
	assertJob(t, plan.Jobs[2], "minimax", "MiniMax-VL-01", KindImage, ModeVideoKeyframe)
	assertJob(t, plan.Jobs[3], "minimax", "MiniMax-VL-01", KindImage, ModeVideoKeyframe)
}

func TestNewVideoEnrichmentPlanTelegramVideoSkipsGrab(t *testing.T) {
	plan, err := NewVideoEnrichmentPlan("telegram:-1001:78", "", VideoArtifactRefs{
		VideoHash:      "sha256:telegram-video",
		KeyframeHashes: []string{"sha256:kf1"},
	}, time.Time{})
	if err != nil {
		t.Fatalf("NewVideoEnrichmentPlan: %v", err)
	}
	if plan.External {
		t.Fatal("Telegram video plan should not be external")
	}
	if len(plan.Jobs) != 2 {
		t.Fatalf("len(plan.Jobs) = %d, want transcript + keyframe", len(plan.Jobs))
	}
	assertJob(t, plan.Jobs[0], DefaultTranscriptionProvider, DefaultTranscriptionModel, KindAudio, ModeVideoAudioTranscript)
	assertJob(t, plan.Jobs[1], "minimax", "MiniMax-VL-01", KindImage, ModeVideoKeyframe)
}

func TestVideoGrabFailureStaysRetryable(t *testing.T) {
	plan, err := NewVideoEnrichmentPlan("telegram:-1001:79", "https://www.instagram.com/reel/abc", VideoArtifactRefs{
		VideoHash: "sha256:video",
	}, time.Time{})
	if err != nil {
		t.Fatalf("NewVideoEnrichmentPlan: %v", err)
	}
	failed := plan.Jobs[0].Start(time.Time{}).Fail(errors.New("temporary download error"), 3, time.Time{})
	if failed.Status != StatusFailedRetryable {
		t.Fatalf("status = %q, want %q", failed.Status, StatusFailedRetryable)
	}
}

func assertJob(t *testing.T, job Job, provider, model, kind, mode string) {
	t.Helper()
	if job.Provider != provider || job.Model != model || job.MediaKind != kind || job.Mode != mode {
		t.Fatalf("job = provider:%q model:%q kind:%q mode:%q", job.Provider, job.Model, job.MediaKind, job.Mode)
	}
	if job.Status != StatusPending {
		t.Fatalf("job status = %q, want pending", job.Status)
	}
	if job.JobID == "" {
		t.Fatal("job id must be populated")
	}
}
