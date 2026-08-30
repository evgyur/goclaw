package trader20control

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PointerWriter atomically stores immutable envelopes and replaces current.json.
// It mutates only its configured local cache directory and has no provider effect.
type PointerWriter struct{ Root string }

type Pointer struct {
	Protocol   string `json:"protocol"`
	Operation  string `json:"operation"`
	Snapshot   string `json:"snapshot"`
	SHA256     string `json:"sha256"`
	CapturedAt string `json:"captured_at"`
}

func (w PointerWriter) Write(env Envelope) (Pointer, error) {
	root, err := filepath.Abs(strings.TrimSpace(w.Root))
	if err != nil || strings.TrimSpace(w.Root) == "" {
		return Pointer{}, errors.New("snapshot root is required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return Pointer{}, err
	}
	raw, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return Pointer{}, err
	}
	snapshotBytes := append(raw, '\n')
	sum := sha256.Sum256(snapshotBytes)
	name := fmt.Sprintf("%s-%s.json", env.CapturedAt.UTC().Format("20060102T150405.000000000Z"), hex.EncodeToString(sum[:8]))
	if err := atomicWrite(filepath.Join(root, name), snapshotBytes, 0o600); err != nil {
		return Pointer{}, err
	}
	ptr := Pointer{Protocol: env.Protocol, Operation: env.Operation, Snapshot: name, SHA256: hex.EncodeToString(sum[:]), CapturedAt: env.CapturedAt.UTC().Format("2006-01-02T15:04:05.000000000Z")}
	ptrRaw, err := json.MarshalIndent(ptr, "", "  ")
	if err != nil {
		return Pointer{}, err
	}
	if err := atomicWrite(filepath.Join(root, "current.json"), append(ptrRaw, '\n'), 0o600); err != nil {
		return Pointer{}, err
	}
	return ptr, nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".trader20-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
