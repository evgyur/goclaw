package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveWithinRejectsTraversalAbsoluteAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "repo", "pkg")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}

	resolved, rel, err := resolveWithin(root, "repo/pkg")
	if err != nil || resolved != inside || rel != "repo/pkg" {
		t.Fatalf("valid relative path rejected: resolved=%q rel=%q err=%v", resolved, rel, err)
	}
	for _, bad := range []string{"", "../repo", "/etc", `repo\pkg`, "escape"} {
		if _, _, err := resolveWithin(root, bad); err == nil {
			t.Errorf("unsafe workdir %q accepted", bad)
		}
	}
}

func TestValidatePackageRejectsTraversalAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	for _, good := range []string{".", "./...", "./pkg"} {
		if err := validatePackage(root, good); err != nil {
			t.Errorf("valid package %q rejected: %v", good, err)
		}
	}
	for _, bad := range []string{"../pkg", "/tmp/pkg", `..\pkg`, "./linked", "example.com/remote"} {
		if err := validatePackage(root, bad); err == nil {
			t.Errorf("unsafe package %q accepted", bad)
		}
	}
}

func TestCommandAllowlistIsExactAndBuildOutputIsTemporary(t *testing.T) {
	for _, allowed := range []string{"go_test", "go_vet"} {
		args, cleanup, err := commandArgs(execArgs{Command: allowed, TimeoutSeconds: 10}, []string{"./..."})
		if err != nil || len(args) == 0 {
			t.Fatalf("allowed command %q rejected: %v", allowed, err)
		}
		cleanup()
	}
	args, cleanup, err := commandArgs(execArgs{Command: "go_build", TimeoutSeconds: 10}, []string{"./cmd/goclaw"})
	if err != nil {
		t.Fatal(err)
	}
	output := args[2]
	if !strings.HasPrefix(output, "/tmp/coding-exec-build-") {
		t.Fatalf("build output is not isolated in /tmp: %q", output)
	}
	cleanup()
	if _, err := os.Stat(filepath.Dir(output)); !os.IsNotExist(err) {
		t.Fatalf("build output directory was not removed: %v", err)
	}
	for _, denied := range []string{"sh", "bash", "curl", "git", "go_install", "docker", "systemctl"} {
		if _, cleanup, err := commandArgs(execArgs{Command: denied}, []string{"./..."}); err == nil {
			cleanup()
			t.Errorf("denied command %q accepted", denied)
		}
	}
}

func TestMCPListsOnlyCodingExec(t *testing.T) {
	cfg := config{root: t.TempDir(), outputLimit: 4096}
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	response := httptest.NewRecorder()
	cfg.handleMCP(response, request)
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	result := body["result"].(map[string]any)
	tools := result["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["name"] != toolName {
		t.Fatalf("unexpected tool surface: %#v", tools)
	}
}

func TestMCPRejectsUnknownArgumentsWithoutExecuting(t *testing.T) {
	cfg := config{root: t.TempDir(), outputLimit: 4096}
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"coding_exec","arguments":{"workdir":".","command":"go_test","shell":"curl example.com"}}}`
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	response := httptest.NewRecorder()
	cfg.handleMCP(response, request)
	if !strings.Contains(response.Body.String(), "unknown field") {
		t.Fatalf("unknown argument was not rejected: %s", response.Body.String())
	}
}

func TestLimitedBufferBoundsCombinedOutput(t *testing.T) {
	buffer := &limitedBuffer{remaining: 5}
	if n, err := buffer.Write([]byte("123456789")); err != nil || n != 9 {
		t.Fatalf("writer contract failed: n=%d err=%v", n, err)
	}
	if got := buffer.buf.String(); got != "12345" || !buffer.truncated {
		t.Fatalf("output was not bounded: %q truncated=%v", got, buffer.truncated)
	}
}

func TestExecuteRunsOnlyTheAllowlistedGoTestInCheckout(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/smoke\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testSource := "package smoke\nimport \"testing\"\nfunc TestSmoke(t *testing.T) {}\n"
	if err := os.WriteFile(filepath.Join(root, "smoke_test.go"), []byte(testSource), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config{root: root, outputLimit: 4096}
	result, err := cfg.execute(context.Background(), execArgs{
		Workdir: ".", Command: "go_test", Packages: []string{"./..."}, TimeoutSeconds: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.ExitCode != 0 || result.TimedOut {
		t.Fatalf("bounded go test failed: %#v", result)
	}
}
