package mediajobs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	RecordClassMediaTranscript   = "media_transcript"
	DefaultTranscriptionProvider = "groq"
	DefaultTranscriptionModel    = "whisper-large-v3-turbo"
)

type TranscriptSegment struct {
	Index        int     `json:"index"`
	ChunkIndex   int     `json:"chunk_index"`
	StartSeconds float64 `json:"start_seconds"`
	EndSeconds   float64 `json:"end_seconds"`
	Text         string  `json:"text"`
}

type Transcript struct {
	SourceEventID string              `json:"source_event_id"`
	MediaKind     string              `json:"media_kind"`
	ArtifactHash  string              `json:"artifact_hash"`
	ArtifactURI   string              `json:"artifact_uri,omitempty"`
	Provider      string              `json:"provider"`
	Model         string              `json:"model"`
	Mode          string              `json:"mode"`
	Language      string              `json:"language,omitempty"`
	Segments      []TranscriptSegment `json:"segments"`
	JobID         string              `json:"job_id,omitempty"`
	CreatedAt     time.Time           `json:"created_at"`
}

type AudioChunk struct {
	Index        int     `json:"index"`
	StartSeconds float64 `json:"start_seconds"`
	EndSeconds   float64 `json:"end_seconds"`
}

func PlanAudioChunks(durationSeconds, chunkSeconds, overlapSeconds float64) []AudioChunk {
	if durationSeconds <= 0 {
		return nil
	}
	if chunkSeconds <= 0 || durationSeconds <= chunkSeconds {
		return []AudioChunk{{Index: 0, StartSeconds: 0, EndSeconds: durationSeconds}}
	}
	if overlapSeconds < 0 {
		overlapSeconds = 0
	}
	if overlapSeconds >= chunkSeconds {
		overlapSeconds = 0
	}

	var chunks []AudioChunk
	for start, idx := 0.0, 0; start < durationSeconds; idx++ {
		end := start + chunkSeconds
		if end > durationSeconds {
			end = durationSeconds
		}
		chunks = append(chunks, AudioChunk{Index: idx, StartSeconds: start, EndSeconds: end})
		if end >= durationSeconds {
			break
		}
		start = end - overlapSeconds
	}
	return chunks
}

func MergeChunkSegments(chunks []AudioChunk, byChunk map[int][]TranscriptSegment) []TranscriptSegment {
	var out []TranscriptSegment
	for _, chunk := range chunks {
		for _, seg := range byChunk[chunk.Index] {
			text := strings.TrimSpace(seg.Text)
			if text == "" {
				continue
			}
			start := chunk.StartSeconds + seg.StartSeconds
			end := chunk.StartSeconds + seg.EndSeconds
			if end < start {
				end = start
			}
			out = append(out, TranscriptSegment{
				Index:        len(out),
				ChunkIndex:   chunk.Index,
				StartSeconds: start,
				EndSeconds:   end,
				Text:         text,
			})
		}
	}
	return out
}

func NewTranscriptMemoryRecord(tr Transcript) (MemoryRecord, error) {
	if tr.SourceEventID == "" {
		return MemoryRecord{}, fmt.Errorf("media transcript missing source_event_id")
	}
	if tr.ArtifactHash == "" {
		return MemoryRecord{}, fmt.Errorf("media transcript missing artifact_hash")
	}
	if tr.Provider == "" {
		tr.Provider = DefaultTranscriptionProvider
	}
	if tr.Model == "" {
		tr.Model = DefaultTranscriptionModel
	}
	if tr.Mode == "" {
		tr.Mode = "transcription"
	}
	if tr.MediaKind == "" {
		tr.MediaKind = KindAudio
	}

	content := renderTranscriptContent(tr)
	key := transcriptIdempotencyKey(tr)
	return MemoryRecord{
		Content:        content,
		Acl:            "yellow",
		Curation:       "extracted",
		Confidence:     0.85,
		SourceType:     RecordClassMediaTranscript,
		SourceRef:      tr.SourceEventID,
		RawRef:         tr.ArtifactURI,
		Environment:    "prod",
		IdempotencyKey: key,
		Metadata: map[string]any{
			"artifact_hash":   tr.ArtifactHash,
			"artifact_uri":    tr.ArtifactURI,
			"provider":        tr.Provider,
			"model":           tr.Model,
			"mode":            tr.Mode,
			"job_id":          tr.JobID,
			"media_kind":      tr.MediaKind,
			"language":        tr.Language,
			"segment_count":   len(tr.Segments),
			"digest_eligible": false,
			"sensitive":       true,
		},
		Tags: []string{
			RecordClassMediaTranscript,
			"media:" + tr.MediaKind,
			"curation:extracted",
			"provider:" + tr.Provider,
		},
	}, nil
}

func renderTranscriptContent(tr Transcript) string {
	var b strings.Builder
	b.WriteString("media_transcript")
	if tr.Language != "" {
		b.WriteString("\nlanguage: ")
		b.WriteString(tr.Language)
	}
	for _, seg := range tr.Segments {
		text := strings.TrimSpace(seg.Text)
		if text == "" {
			continue
		}
		b.WriteByte('\n')
		b.WriteString("[")
		b.WriteString(formatTranscriptSeconds(seg.StartSeconds))
		b.WriteString(" - ")
		b.WriteString(formatTranscriptSeconds(seg.EndSeconds))
		b.WriteString("] ")
		b.WriteString(text)
	}
	return b.String()
}

func transcriptIdempotencyKey(tr Transcript) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		tr.SourceEventID,
		tr.ArtifactHash,
		tr.Provider,
		tr.Model,
		tr.Mode,
	}, "\x00")))
	return "media_transcript_" + hex.EncodeToString(sum[:16])
}

func formatTranscriptSeconds(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	totalMillis := int(seconds*1000 + 0.5)
	minutes := totalMillis / 60000
	secs := (totalMillis % 60000) / 1000
	millis := totalMillis % 1000
	return fmt.Sprintf("%02d:%02d.%03d", minutes, secs, millis)
}
