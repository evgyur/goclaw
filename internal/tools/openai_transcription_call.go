package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// isTranscriptionModel returns true for OpenAI models that require the
// /v1/audio/transcriptions endpoint instead of /chat/completions.
// Covers whisper and the gpt-4o-(mini-)transcribe family.
func isTranscriptionModel(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return false
	}
	if strings.HasPrefix(m, "whisper") {
		return true
	}
	// gpt-4o-transcribe, gpt-4o-mini-transcribe, and future variants.
	return strings.Contains(m, "transcribe")
}

// extFromMime maps an audio MIME type to a file extension accepted by
// OpenAI's transcription endpoint. Falls back to .mp3 for unknown types.
func extFromMime(mime string) string {
	m := strings.ToLower(mime)
	switch {
	case strings.Contains(m, "wav"):
		return ".wav"
	case strings.Contains(m, "mp3"), strings.Contains(m, "mpeg"):
		return ".mp3"
	case strings.Contains(m, "m4a"), strings.Contains(m, "mp4"):
		return ".m4a"
	case strings.Contains(m, "ogg"), strings.Contains(m, "opus"):
		return ".ogg"
	case strings.Contains(m, "flac"):
		return ".flac"
	case strings.Contains(m, "webm"):
		return ".webm"
	default:
		return ".mp3"
	}
}

// openaiTranscriptionCall sends audio to an OpenAI-compatible
// /v1/audio/transcriptions endpoint using multipart/form-data. It requests
// verbose segment timestamps and falls back to plain text when segments are
// absent. Transcription endpoints do not provide token usage counters.
func openaiTranscriptionCall(ctx context.Context, apiKey, baseURL, model string, data []byte, mime string) (*providers.ChatResponse, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	filePart, err := w.CreateFormFile("file", "audio"+extFromMime(mime))
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := filePart.Write(data); err != nil {
		return nil, fmt.Errorf("write audio payload: %w", err)
	}
	if err := w.WriteField("model", model); err != nil {
		return nil, fmt.Errorf("write model field: %w", err)
	}
	if err := w.WriteField("response_format", "verbose_json"); err != nil {
		return nil, fmt.Errorf("write response_format field: %w", err)
	}
	if err := w.WriteField("timestamp_granularities[]", "segment"); err != nil {
		return nil, fmt.Errorf("write timestamp_granularities field: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("close multipart: %w", err)
	}

	url := strings.TrimRight(baseURL, "/") + "/audio/transcriptions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, &buf)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateStr(string(respBody), 500))
	}

	var out struct {
		Text     string `json:"text"`
		Segments []struct {
			Start float64 `json:"start"`
			End   float64 `json:"end"`
			Text  string  `json:"text"`
		} `json:"segments"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if out.Text == "" {
		return nil, fmt.Errorf("empty transcription")
	}
	content := out.Text
	if len(out.Segments) > 0 {
		var b strings.Builder
		for _, seg := range out.Segments {
			text := strings.TrimSpace(seg.Text)
			if text == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString("[")
			b.WriteString(formatSeconds(seg.Start))
			b.WriteString(" - ")
			b.WriteString(formatSeconds(seg.End))
			b.WriteString("] ")
			b.WriteString(text)
		}
		if b.Len() > 0 {
			content = b.String()
		}
	}
	return &providers.ChatResponse{
		Content:      content,
		FinishReason: "stop",
	}, nil
}

func formatSeconds(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	totalMillis := int(seconds*1000 + 0.5)
	minutes := totalMillis / 60000
	secs := (totalMillis % 60000) / 1000
	millis := totalMillis % 1000
	return fmt.Sprintf("%02d:%02d.%03d", minutes, secs, millis)
}
