package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestOpenAITranscriptionCallRequestsVerboseSegmentTimestamps(t *testing.T) {
	var fields url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/transcriptions" {
			t.Fatalf("path = %q, want /audio/transcriptions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization = %q", got)
		}
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		fields = r.MultipartForm.Value
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("file missing: %v", err)
		}
		defer file.Close()
		body, _ := io.ReadAll(file)
		if string(body) != "audio-bytes" {
			t.Fatalf("uploaded file = %q", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"text": "hello world",
			"segments": []map[string]any{
				{"start": 0.0, "end": 1.25, "text": "hello"},
				{"start": 1.25, "end": 2.5, "text": "world"},
			},
		})
	}))
	defer srv.Close()

	resp, err := openaiTranscriptionCall(context.Background(), "test-key", srv.URL, "whisper-large-v3-turbo", []byte("audio-bytes"), "audio/mpeg")
	if err != nil {
		t.Fatalf("openaiTranscriptionCall: %v", err)
	}
	if fields.Get("model") != "whisper-large-v3-turbo" {
		t.Fatalf("model = %q", fields.Get("model"))
	}
	if fields.Get("response_format") != "verbose_json" {
		t.Fatalf("response_format = %q", fields.Get("response_format"))
	}
	if fields.Get("timestamp_granularities[]") != "segment" {
		t.Fatalf("timestamp_granularities[] = %q", fields.Get("timestamp_granularities[]"))
	}
	if !strings.Contains(resp.Content, "[00:00.000 - 00:01.250] hello") {
		t.Fatalf("content missing first timestamped segment: %q", resp.Content)
	}
	if !strings.Contains(resp.Content, "[00:01.250 - 00:02.500] world") {
		t.Fatalf("content missing second timestamped segment: %q", resp.Content)
	}
}
