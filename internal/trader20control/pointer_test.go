package trader20control

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPointerWriterCreatesHashedImmutableSnapshotAndAtomicPointer(t *testing.T) {
	root := t.TempDir()
	env := Envelope{Protocol: ProtocolVersion, Operation: "status", CapturedAt: time.Unix(1, 2).UTC(), Data: json.RawMessage(`{"ok":true}`)}
	ptr, err := (PointerWriter{Root: root}).Write(env)
	if err != nil {
		t.Fatal(err)
	}
	if ptr.Snapshot == "current.json" || filepath.Base(ptr.Snapshot) != ptr.Snapshot {
		t.Fatalf("unsafe snapshot name %q", ptr.Snapshot)
	}
	raw, err := os.ReadFile(filepath.Join(root, ptr.Snapshot))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if ptr.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("hash = %q, want %x", ptr.SHA256, sum)
	}
	currentRaw, err := os.ReadFile(filepath.Join(root, "current.json"))
	if err != nil {
		t.Fatal(err)
	}
	var current Pointer
	if err := json.Unmarshal(currentRaw, &current); err != nil {
		t.Fatal(err)
	}
	if current != ptr {
		t.Fatalf("current = %#v, want %#v", current, ptr)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
}
