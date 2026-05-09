package mediajobs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	SourceKindUser  = "user"
	SourceKindScout = "scout"

	ImageRoleUserSent   = "user_sent"
	ImageRolePrimary    = "primary"
	ImageRoleSelected   = "selected"
	ImageRoleScreenshot = "screenshot"
	ImageRoleRetained   = "retained_source"
	ImageRoleThumbnail  = "thumbnail"
	ImageRoleAd         = "ad"
	ImageRoleAvatar     = "avatar"
	ImageRoleDecorative = "decorative"
)

type Observation struct {
	SourceEventID string
	SourceKind    string
	ImageRole     string
	ArtifactHash  string
	ArtifactURI   string
	Provider      string
	Model         string
	Mode          string
	OCRText       string
	Description   string
	Language      string
	Confidence    float64
	JobID         string
	CreatedAt     time.Time
}

type MemoryRecord struct {
	Content        string         `json:"content"`
	Acl            string         `json:"acl"`
	Curation       string         `json:"curation"`
	Confidence     float64        `json:"confidence"`
	SourceType     string         `json:"source_type"`
	SourceRef      string         `json:"source_ref"`
	RawRef         string         `json:"raw_ref,omitempty"`
	Environment    string         `json:"environment"`
	IdempotencyKey string         `json:"idempotency_key"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	Tags           []string       `json:"tags,omitempty"`
}

func ShouldEnrichImage(sourceKind, imageRole string) bool {
	sourceKind = normalize(sourceKind)
	imageRole = normalize(imageRole)
	if sourceKind == SourceKindUser {
		return true
	}
	if sourceKind != SourceKindScout {
		return false
	}
	switch imageRole {
	case ImageRolePrimary, ImageRoleSelected, ImageRoleScreenshot, ImageRoleRetained:
		return true
	default:
		return false
	}
}

func NewImageObservationMemoryRecord(obs Observation) (MemoryRecord, error) {
	if strings.TrimSpace(obs.SourceEventID) == "" {
		return MemoryRecord{}, fmt.Errorf("media observation missing source_event_id")
	}
	if strings.TrimSpace(obs.ArtifactHash) == "" {
		return MemoryRecord{}, fmt.Errorf("media observation missing artifact_hash")
	}
	if strings.TrimSpace(obs.Provider) == "" {
		return MemoryRecord{}, fmt.Errorf("media observation missing provider")
	}
	if strings.TrimSpace(obs.Model) == "" {
		return MemoryRecord{}, fmt.Errorf("media observation missing model")
	}
	confidence := obs.Confidence
	if confidence <= 0 || confidence > 1 {
		confidence = 0.75
	}
	sourceKind := normalize(obs.SourceKind)
	if sourceKind == "" {
		sourceKind = SourceKindUser
	}
	imageRole := normalize(obs.ImageRole)
	if imageRole == "" {
		imageRole = ImageRoleUserSent
	}
	mode := normalize(obs.Mode)
	if mode == "" {
		mode = "image_observation"
	}
	content := buildObservationContent(obs)
	key := observationIdempotencyKey(obs.SourceEventID, obs.ArtifactHash, obs.Provider, obs.Model, mode)
	return MemoryRecord{
		Content:        content,
		Acl:            "yellow",
		Curation:       "extracted",
		Confidence:     confidence,
		SourceType:     "media_observation",
		SourceRef:      obs.SourceEventID,
		RawRef:         obs.ArtifactURI,
		Environment:    "prod",
		IdempotencyKey: key,
		Metadata: map[string]any{
			"artifact_hash":   normalize(obs.ArtifactHash),
			"artifact_uri":    strings.TrimSpace(obs.ArtifactURI),
			"provider":        normalize(obs.Provider),
			"model":           strings.TrimSpace(obs.Model),
			"mode":            mode,
			"job_id":          strings.TrimSpace(obs.JobID),
			"source_kind":     sourceKind,
			"image_role":      imageRole,
			"language":        strings.TrimSpace(obs.Language),
			"digest_eligible": false,
			"sensitive":       true,
		},
		Tags: []string{
			"media_observation",
			"media:image",
			"curation:extracted",
			"provider:" + normalize(obs.Provider),
		},
	}, nil
}

func buildObservationContent(obs Observation) string {
	parts := []string{"media_observation"}
	if text := strings.TrimSpace(obs.Description); text != "" {
		parts = append(parts, "description: "+text)
	}
	if text := strings.TrimSpace(obs.OCRText); text != "" {
		parts = append(parts, "ocr: "+text)
	}
	if lang := strings.TrimSpace(obs.Language); lang != "" {
		parts = append(parts, "language: "+lang)
	}
	return strings.Join(parts, "\n")
}

func observationIdempotencyKey(sourceEventID, artifactHash, provider, model, mode string) string {
	parts := []string{
		strings.TrimSpace(sourceEventID),
		normalize(artifactHash),
		normalize(provider),
		strings.TrimSpace(model),
		normalize(mode),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return "media_observation_" + hex.EncodeToString(sum[:16])
}
