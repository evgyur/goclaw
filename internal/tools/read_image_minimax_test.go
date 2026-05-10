package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

type minimaxTestCreds struct {
	key  string
	base string
}

func (m minimaxTestCreds) APIKey() string  { return m.key }
func (m minimaxTestCreds) APIBase() string { return m.base }

func TestCallMiniMaxCodingPlanVLM(t *testing.T) {
	var gotAuth string
	var gotSource string
	var gotPath string
	var gotPayload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotSource = r.Header.Get("MM-API-Source")
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":"image says hello","base_resp":{"status_code":0,"status_msg":""}}`))
	}))
	defer srv.Close()

	out, usage, err := callMiniMaxCodingPlanVLM(context.Background(), minimaxTestCreds{key: "k", base: srv.URL + "/v1"}, "describe", []providers.ImageContent{{MimeType: "image/png", Data: "abc"}}, "MiniMax-M2.7-highspeed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != "image says hello" {
		t.Fatalf("unexpected output: %s", out)
	}
	if usage != nil {
		t.Fatalf("expected nil usage, got %#v", usage)
	}
	if gotPath != "/v1/coding_plan/vlm" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if gotAuth != "Bearer k" {
		t.Fatalf("unexpected auth: %s", gotAuth)
	}
	if gotSource != "GoClaw" {
		t.Fatalf("unexpected source: %s", gotSource)
	}
	b, _ := json.Marshal(gotPayload)
	if !strings.Contains(string(b), "data:image/png;base64,abc") {
		t.Fatalf("image data URL missing from payload: %s", b)
	}
}
