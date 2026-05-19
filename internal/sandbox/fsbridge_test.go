package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFsBridgeWriteFileDoesNotInvokeShell(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "docker.log")
	stdinPath := filepath.Join(tmp, "stdin.txt")
	dockerPath := filepath.Join(tmp, "docker")

	script := `#!/bin/sh
{
  echo CALL
  i=0
  for arg in "$@"; do
    echo "ARG[$i]=$arg"
    i=$((i + 1))
  done
} >> "$DOCKER_LOG"

for arg in "$@"; do
  if [ "$arg" = "tee" ]; then
    cat > "$DOCKER_STDIN"
    break
  fi
done
`
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOCKER_LOG", logPath)
	t.Setenv("DOCKER_STDIN", stdinPath)

	bridge := NewFsBridge("container-id", "/workspace")
	maliciousPath := `nested/evil$(touch /tmp/goclaw-fsbridge-pwned);name.txt`
	content := "safe content"

	if err := bridge.WriteFile(context.Background(), maliciousPath, content, false); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read docker log: %v", err)
	}
	log := string(logBytes)

	if strings.Contains(log, "ARG[3]=sh") || strings.Contains(log, "ARG[4]=-c") || strings.Contains(log, "cat >") {
		t.Fatalf("WriteFile invoked shell command path; log:\n%s", log)
	}
	if !strings.Contains(log, "ARG[3]=tee") {
		t.Fatalf("expected write command to use tee without shell; log:\n%s", log)
	}
	if !strings.Contains(log, "ARG[4]=--") {
		t.Fatalf("expected tee delimiter before filename; log:\n%s", log)
	}
	if !strings.Contains(log, `ARG[5]=/workspace/`+maliciousPath) {
		t.Fatalf("expected malicious filename to remain one argv entry; log:\n%s", log)
	}

	stdinBytes, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read captured stdin: %v", err)
	}
	if string(stdinBytes) != content {
		t.Fatalf("stdin content = %q, want %q", string(stdinBytes), content)
	}
}

func TestFsBridgeWriteFileAppendUsesTeeAppendArg(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "docker.log")
	dockerPath := filepath.Join(tmp, "docker")

	script := `#!/bin/sh
{
  echo CALL
  i=0
  for arg in "$@"; do
    echo "ARG[$i]=$arg"
    i=$((i + 1))
  done
} >> "$DOCKER_LOG"
cat >/dev/null
`
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOCKER_LOG", logPath)

	bridge := NewFsBridge("container-id", "/workspace")
	if err := bridge.WriteFile(context.Background(), "append.txt", "more", true); err != nil {
		t.Fatalf("WriteFile append returned error: %v", err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read docker log: %v", err)
	}
	log := string(logBytes)
	if !strings.Contains(log, "ARG[3]=tee") || !strings.Contains(log, "ARG[4]=-a") || !strings.Contains(log, "ARG[5]=--") {
		t.Fatalf("expected append write to use `tee -a -- <path>`; log:\n%s", log)
	}
}
