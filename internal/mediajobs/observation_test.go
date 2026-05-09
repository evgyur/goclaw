package mediajobs

import "testing"

func TestShouldEnrichImageCostGate(t *testing.T) {
	tests := []struct {
		name       string
		sourceKind string
		imageRole  string
		want       bool
	}{
		{"user image", SourceKindUser, ImageRoleThumbnail, true},
		{"scout primary", SourceKindScout, ImageRolePrimary, true},
		{"scout selected", SourceKindScout, ImageRoleSelected, true},
		{"scout screenshot", SourceKindScout, ImageRoleScreenshot, true},
		{"scout retained source", SourceKindScout, ImageRoleRetained, true},
		{"scout thumbnail", SourceKindScout, ImageRoleThumbnail, false},
		{"scout ad", SourceKindScout, ImageRoleAd, false},
		{"scout avatar", SourceKindScout, ImageRoleAvatar, false},
		{"scout decorative", SourceKindScout, ImageRoleDecorative, false},
		{"unknown source", "crawler", ImageRolePrimary, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldEnrichImage(tt.sourceKind, tt.imageRole); got != tt.want {
				t.Fatalf("ShouldEnrichImage(%q, %q) = %v, want %v", tt.sourceKind, tt.imageRole, got, tt.want)
			}
		})
	}
}

func TestImageObservationMemoryRecordDefaultsToSensitiveExtracted(t *testing.T) {
	rec, err := NewImageObservationMemoryRecord(Observation{
		SourceEventID: "telegram:-1001:42:photo:0",
		SourceKind:    SourceKindUser,
		ImageRole:     ImageRoleUserSent,
		ArtifactHash:  "ABCDEF",
		ArtifactURI:   "artifact://sha256/abcdef",
		Provider:      "MiniMax",
		Model:         "MiniMax-VL-01",
		Mode:          "image_observation",
		OCRText:       "Invoice 123",
		Description:   "A photographed invoice on a desk.",
		Language:      "en",
		Confidence:    0.91,
		JobID:         "mediajob_123",
	})
	if err != nil {
		t.Fatal(err)
	}

	if rec.SourceType != "media_observation" {
		t.Fatalf("source_type = %q", rec.SourceType)
	}
	if rec.Acl != "yellow" || rec.Curation != "extracted" {
		t.Fatalf("acl/curation = %q/%q, want yellow/extracted", rec.Acl, rec.Curation)
	}
	if rec.Metadata["digest_eligible"] != false {
		t.Fatalf("digest_eligible = %#v, want false", rec.Metadata["digest_eligible"])
	}
	if rec.Metadata["sensitive"] != true {
		t.Fatalf("sensitive = %#v, want true", rec.Metadata["sensitive"])
	}
	if rec.Metadata["provider"] != "minimax" || rec.Metadata["model"] != "MiniMax-VL-01" {
		t.Fatalf("provider/model metadata = %#v/%#v", rec.Metadata["provider"], rec.Metadata["model"])
	}
	if rec.Content == "" || rec.IdempotencyKey == "" {
		t.Fatalf("content/idempotency must be populated: %#v", rec)
	}
}

func TestImageObservationIdempotencyPreventsDuplicateWrites(t *testing.T) {
	obs := Observation{
		SourceEventID: "scout:https://example.test/page:image:hero",
		SourceKind:    SourceKindScout,
		ImageRole:     ImageRolePrimary,
		ArtifactHash:  "hash",
		Provider:      "minimax",
		Model:         "MiniMax-VL-01",
		Mode:          "image_observation",
		Description:   "Hero image with product screenshot.",
		Confidence:    0.8,
	}
	a, err := NewImageObservationMemoryRecord(obs)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewImageObservationMemoryRecord(obs)
	if err != nil {
		t.Fatal(err)
	}
	if a.IdempotencyKey != b.IdempotencyKey {
		t.Fatalf("same observation produced different keys: %q != %q", a.IdempotencyKey, b.IdempotencyKey)
	}

	obs.Model = "MiniMax-VL-02"
	c, err := NewImageObservationMemoryRecord(obs)
	if err != nil {
		t.Fatal(err)
	}
	if c.IdempotencyKey == a.IdempotencyKey {
		t.Fatal("model change should produce a new observation idempotency key")
	}
}
