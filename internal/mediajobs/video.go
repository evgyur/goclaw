package mediajobs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	ProviderChipVideoGrab = "chip-video-grab"
	ModelChipVideoGrab    = "chip-video-grab"

	ModeVideoGrab            = "video_grab"
	ModeVideoAudioTranscript = "video_audio_transcript"
	ModeVideoKeyframe        = "video_keyframe_observation"
)

type VideoArtifactRefs struct {
	VideoHash      string   `json:"video_hash"`
	AudioHash      string   `json:"audio_hash,omitempty"`
	KeyframeHashes []string `json:"keyframe_hashes,omitempty"`
}

type VideoEnrichmentPlan struct {
	SourceEventID string `json:"source_event_id"`
	URL           string `json:"url,omitempty"`
	External      bool   `json:"external"`
	Jobs          []Job  `json:"jobs"`
}

func IsExternalVideoURL(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	switch {
	case host == "youtu.be" || strings.HasSuffix(host, ".youtube.com") || host == "youtube.com":
		return true
	case host == "instagram.com" || strings.HasSuffix(host, ".instagram.com"):
		return true
	case host == "tiktok.com" || strings.HasSuffix(host, ".tiktok.com"):
		return true
	case host == "vimeo.com" || strings.HasSuffix(host, ".vimeo.com"):
		return true
	default:
		return false
	}
}

func NewVideoEnrichmentPlan(sourceEventID, rawURL string, refs VideoArtifactRefs, now time.Time) (VideoEnrichmentPlan, error) {
	if strings.TrimSpace(sourceEventID) == "" {
		return VideoEnrichmentPlan{}, fmt.Errorf("video enrichment missing source_event_id")
	}
	if refs.VideoHash == "" {
		refs.VideoHash = hashExternalVideoRef(rawURL)
	}
	if refs.VideoHash == "" {
		return VideoEnrichmentPlan{}, fmt.Errorf("video enrichment missing video artifact hash")
	}

	plan := VideoEnrichmentPlan{
		SourceEventID: sourceEventID,
		URL:           strings.TrimSpace(rawURL),
		External:      IsExternalVideoURL(rawURL),
	}
	if plan.External {
		job, err := New(Spec{
			SourceEventID: sourceEventID,
			MediaKind:     KindVideo,
			ArtifactHash:  refs.VideoHash,
			Provider:      ProviderChipVideoGrab,
			Model:         ModelChipVideoGrab,
			Mode:          ModeVideoGrab,
			Now:           now,
		})
		if err != nil {
			return VideoEnrichmentPlan{}, err
		}
		plan.Jobs = append(plan.Jobs, job)
	}

	audioHash := refs.AudioHash
	if audioHash == "" {
		audioHash = refs.VideoHash
	}
	transcriptJob, err := New(Spec{
		SourceEventID: sourceEventID,
		MediaKind:     KindAudio,
		ArtifactHash:  audioHash,
		Provider:      DefaultTranscriptionProvider,
		Model:         DefaultTranscriptionModel,
		Mode:          ModeVideoAudioTranscript,
		Now:           now,
	})
	if err != nil {
		return VideoEnrichmentPlan{}, err
	}
	plan.Jobs = append(plan.Jobs, transcriptJob)

	for _, keyframeHash := range refs.KeyframeHashes {
		if strings.TrimSpace(keyframeHash) == "" {
			continue
		}
		job, err := New(Spec{
			SourceEventID: sourceEventID,
			MediaKind:     KindImage,
			ArtifactHash:  keyframeHash,
			Provider:      "minimax",
			Model:         "MiniMax-VL-01",
			Mode:          ModeVideoKeyframe,
			Now:           now,
		})
		if err != nil {
			return VideoEnrichmentPlan{}, err
		}
		plan.Jobs = append(plan.Jobs, job)
	}
	return plan, nil
}

func hashExternalVideoRef(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(rawURL))
	return "sha256:" + hex.EncodeToString(sum[:])
}
