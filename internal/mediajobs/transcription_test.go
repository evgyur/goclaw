package mediajobs

import (
	"strings"
	"testing"
)

func TestPlanAudioChunksPreservesOrderWithOverlap(t *testing.T) {
	chunks := PlanAudioChunks(125, 60, 5)
	if len(chunks) != 3 {
		t.Fatalf("len(chunks) = %d, want 3: %#v", len(chunks), chunks)
	}
	if chunks[0].StartSeconds != 0 || chunks[0].EndSeconds != 60 {
		t.Fatalf("chunk 0 = %#v", chunks[0])
	}
	if chunks[1].StartSeconds != 55 || chunks[1].EndSeconds != 115 {
		t.Fatalf("chunk 1 = %#v", chunks[1])
	}
	if chunks[2].StartSeconds != 110 || chunks[2].EndSeconds != 125 {
		t.Fatalf("chunk 2 = %#v", chunks[2])
	}
}

func TestMergeChunkSegmentsAppliesChunkOffsets(t *testing.T) {
	chunks := []AudioChunk{
		{Index: 0, StartSeconds: 0, EndSeconds: 60},
		{Index: 1, StartSeconds: 60, EndSeconds: 120},
	}
	segments := MergeChunkSegments(chunks, map[int][]TranscriptSegment{
		0: {{StartSeconds: 1, EndSeconds: 2, Text: "first"}},
		1: {{StartSeconds: 3, EndSeconds: 4, Text: "second"}},
	})
	if len(segments) != 2 {
		t.Fatalf("len(segments) = %d", len(segments))
	}
	if segments[0].Index != 0 || segments[0].StartSeconds != 1 || segments[0].EndSeconds != 2 {
		t.Fatalf("segment 0 = %#v", segments[0])
	}
	if segments[1].Index != 1 || segments[1].StartSeconds != 63 || segments[1].EndSeconds != 64 {
		t.Fatalf("segment 1 = %#v", segments[1])
	}
}

func TestTranscriptMemoryRecordDefaultsToRawExtractedSensitive(t *testing.T) {
	rec, err := NewTranscriptMemoryRecord(Transcript{
		SourceEventID: "telegram:-1001:42",
		MediaKind:     KindVoice,
		ArtifactHash:  "sha256:voice",
		ArtifactURI:   "artifact://voice",
		Language:      "en",
		Segments: []TranscriptSegment{
			{StartSeconds: 0, EndSeconds: 1.5, Text: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("NewTranscriptMemoryRecord: %v", err)
	}
	if rec.SourceType != RecordClassMediaTranscript {
		t.Fatalf("source_type = %q", rec.SourceType)
	}
	if rec.Acl != "yellow" || rec.Curation != "extracted" {
		t.Fatalf("acl/curation = %q/%q, want yellow/extracted", rec.Acl, rec.Curation)
	}
	if rec.Metadata["digest_eligible"] != false || rec.Metadata["sensitive"] != true {
		t.Fatalf("sensitivity metadata = %#v", rec.Metadata)
	}
	if rec.Metadata["provider"] != DefaultTranscriptionProvider || rec.Metadata["model"] != DefaultTranscriptionModel {
		t.Fatalf("provider/model metadata = %#v", rec.Metadata)
	}
	if !strings.Contains(rec.Content, "[00:00.000 - 00:01.500] hello") {
		t.Fatalf("content missing timestamped transcript: %q", rec.Content)
	}
}

func TestTranscriptIdempotencyPreventsDuplicateWrites(t *testing.T) {
	tr := Transcript{
		SourceEventID: "telegram:-1001:42",
		ArtifactHash:  "sha256:audio",
		Provider:      "groq",
		Model:         "whisper-large-v3-turbo",
		Mode:          "transcription",
		Segments:      []TranscriptSegment{{Text: "first"}},
	}
	a, err := NewTranscriptMemoryRecord(tr)
	if err != nil {
		t.Fatalf("NewTranscriptMemoryRecord(a): %v", err)
	}
	b, err := NewTranscriptMemoryRecord(tr)
	if err != nil {
		t.Fatalf("NewTranscriptMemoryRecord(b): %v", err)
	}
	if a.IdempotencyKey != b.IdempotencyKey {
		t.Fatalf("same transcript produced different keys: %q vs %q", a.IdempotencyKey, b.IdempotencyKey)
	}
	tr.Model = "whisper-large-v3"
	c, err := NewTranscriptMemoryRecord(tr)
	if err != nil {
		t.Fatalf("NewTranscriptMemoryRecord(c): %v", err)
	}
	if c.IdempotencyKey == a.IdempotencyKey {
		t.Fatal("model change should produce a new transcript idempotency key")
	}
}
