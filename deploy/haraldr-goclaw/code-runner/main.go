// haraldr-code-runner exposes one policy-constrained MCP tool for offline Go builds and tests.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	toolName        = "coding_exec"
	defaultRoot     = "/workspace"
	defaultListen   = ":8090"
	defaultTimeout  = 60
	maxTimeout      = 120
	defaultOutput   = 65536
	maxRequestBytes = 131072
)

var (
	packagePattern = regexp.MustCompile(`^\.?/?[A-Za-z0-9_./-]*$`)
	runPattern     = regexp.MustCompile(`^[A-Za-z0-9_./|()^$*+?\[\]{}:-]{0,256}$`)
)

type config struct {
	root        string
	listen      string
	outputLimit int
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type execArgs struct {
	Workdir        string   `json:"workdir"`
	Command        string   `json:"command"`
	Packages       []string `json:"packages,omitempty"`
	Run            string   `json:"run,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
}

type execResult struct {
	OK              bool   `json:"ok"`
	Command         string `json:"command"`
	Workdir         string `json:"workdir"`
	ExitCode        int    `json:"exit_code"`
	TimedOut        bool   `json:"timed_out"`
	Output          string `json:"output"`
	OutputTruncated bool   `json:"output_truncated"`
	DurationMS      int64  `json:"duration_ms"`
}

type limitedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	remaining int
	truncated bool
}

func (w *limitedBuffer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	original := len(p)
	if len(p) > w.remaining {
		p = p[:w.remaining]
		w.truncated = true
	}
	if len(p) > 0 {
		_, _ = w.buf.Write(p)
		w.remaining -= len(p)
	}
	return original, nil
}

func main() {
	cfg := config{
		root:        envOr("CODING_EXEC_ROOT", defaultRoot),
		listen:      envOr("CODING_EXEC_LISTEN", defaultListen),
		outputLimit: envInt("CODING_EXEC_MAX_OUTPUT_BYTES", defaultOutput),
	}
	root, err := filepath.EvalSymlinks(cfg.root)
	if err != nil {
		log.Fatalf("resolve checkout root: %v", err)
	}
	cfg.root = filepath.Clean(root)
	if cfg.outputLimit < 1024 || cfg.outputLimit > 1048576 {
		log.Fatal("CODING_EXEC_MAX_OUTPUT_BYTES must be between 1024 and 1048576")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /mcp", cfg.handleMCP)
	server := &http.Server{
		Addr:              cfg.listen,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      (maxTimeout + 10) * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	log.Printf("coding runner listening on %s for root %s", cfg.listen, cfg.root)
	log.Fatal(server.ListenAndServe())
}

func (cfg config) handleMCP(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeRPC(w, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "request too large or unreadable"}})
		return
	}
	var request rpcRequest
	if err := json.Unmarshal(body, &request); err != nil || request.JSONRPC != "2.0" {
		writeRPC(w, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "invalid JSON-RPC request"}})
		return
	}
	response := rpcResponse{JSONRPC: "2.0", ID: request.ID}
	switch request.Method {
	case "initialize":
		response.Result = map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]string{"name": "haraldr-code-runner", "version": "a04-v1"},
		}
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
		return
	case "tools/list":
		response.Result = map[string]any{"tools": []any{toolDefinition()}}
	case "tools/call":
		var params callParams
		if err := strictJSON(request.Params, &params); err != nil || params.Name != toolName {
			response.Error = &rpcError{Code: -32602, Message: "only coding_exec is available"}
			break
		}
		var args execArgs
		if err := strictJSON(params.Arguments, &args); err != nil {
			response.Error = &rpcError{Code: -32602, Message: "invalid coding_exec arguments: " + err.Error()}
			break
		}
		result, err := cfg.execute(r.Context(), args)
		if err != nil {
			response.Result = map[string]any{
				"content": []map[string]string{{"type": "text", "text": mustJSON(map[string]any{"ok": false, "error": err.Error()})}},
				"isError": true,
			}
			break
		}
		response.Result = map[string]any{
			"content":           []map[string]string{{"type": "text", "text": mustJSON(result)}},
			"structuredContent": result,
			"isError":           !result.OK,
		}
	default:
		response.Error = &rpcError{Code: -32601, Message: "method not found"}
	}
	writeRPC(w, response)
}

func toolDefinition() map[string]any {
	return map[string]any{
		"name":        toolName,
		"description": "Run one bounded, network-isolated Go build or test action inside the checkout.",
		"inputSchema": map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"workdir", "command"},
			"properties": map[string]any{
				"workdir":         map[string]any{"type": "string", "description": "Checkout-relative directory; absolute and escaping paths are rejected."},
				"command":         map[string]any{"type": "string", "enum": []string{"go_test", "go_build", "go_vet"}},
				"packages":        map[string]any{"type": "array", "maxItems": 32, "items": map[string]any{"type": "string"}},
				"run":             map[string]any{"type": "string", "maxLength": 256},
				"timeout_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": maxTimeout},
			},
		},
	}
}

func (cfg config) execute(parent context.Context, args execArgs) (execResult, error) {
	cwd, rel, err := resolveWithin(cfg.root, args.Workdir)
	if err != nil {
		return execResult{}, fmt.Errorf("workdir rejected: %w", err)
	}
	if args.TimeoutSeconds == 0 {
		args.TimeoutSeconds = defaultTimeout
	}
	if args.TimeoutSeconds < 1 || args.TimeoutSeconds > maxTimeout {
		return execResult{}, errors.New("timeout_seconds is outside the 1..120 bound")
	}
	packages := args.Packages
	if len(packages) == 0 {
		packages = []string{"./..."}
	}
	if len(packages) > 32 {
		return execResult{}, errors.New("too many package selectors")
	}
	for _, pkg := range packages {
		if err := validatePackage(cwd, pkg); err != nil {
			return execResult{}, err
		}
	}
	if args.Command != "go_test" && args.Run != "" {
		return execResult{}, errors.New("run is permitted only for go_test")
	}
	if !runPattern.MatchString(args.Run) {
		return execResult{}, errors.New("run contains unsupported characters")
	}

	ctx, cancel := context.WithTimeout(parent, time.Duration(args.TimeoutSeconds)*time.Second)
	defer cancel()
	argv, cleanup, err := commandArgs(args, packages)
	if err != nil {
		return execResult{}, err
	}
	defer cleanup()
	cmd := exec.CommandContext(ctx, "go", argv...)
	cmd.Dir = cwd
	cmd.Env = []string{
		"PATH=/usr/local/go/bin:/usr/bin:/bin",
		"HOME=/tmp/home", "GOCACHE=/tmp/go-build", "GOMODCACHE=/opt/go/pkg/mod",
		"GOFLAGS=-mod=readonly -buildvcs=false",
		"GOPROXY=off", "GONOSUMDB=*", "GOSUMDB=off", "CGO_ENABLED=0",
	}
	output := &limitedBuffer{remaining: cfg.outputLimit}
	cmd.Stdout = output
	cmd.Stderr = output
	started := time.Now()
	runErr := cmd.Run()
	duration := time.Since(started).Milliseconds()
	exitCode := 0
	if runErr != nil {
		exitCode = -1
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}
	result := execResult{
		OK: runErr == nil, Command: args.Command, Workdir: rel, ExitCode: exitCode,
		TimedOut: ctx.Err() == context.DeadlineExceeded, Output: output.buf.String(),
		OutputTruncated: output.truncated, DurationMS: duration,
	}
	return result, nil
}

func commandArgs(args execArgs, packages []string) ([]string, func(), error) {
	switch args.Command {
	case "go_test":
		argv := []string{"test", "-count=1", "-timeout=" + strconv.Itoa(args.TimeoutSeconds) + "s"}
		if args.Run != "" {
			argv = append(argv, "-run", args.Run)
		}
		return append(argv, packages...), func() {}, nil
	case "go_vet":
		return append([]string{"vet"}, packages...), func() {}, nil
	case "go_build":
		if len(packages) != 1 {
			return nil, func() {}, errors.New("go_build requires exactly one package selector")
		}
		dir, err := os.MkdirTemp("/tmp", "coding-exec-build-")
		if err != nil {
			return nil, func() {}, fmt.Errorf("create bounded build output: %w", err)
		}
		return []string{"build", "-o", filepath.Join(dir, "artifact"), packages[0]}, func() { _ = os.RemoveAll(dir) }, nil
	default:
		return nil, func() {}, errors.New("command is not in the build/test allowlist")
	}
}

func resolveWithin(root, requested string) (string, string, error) {
	if requested == "" {
		return "", "", errors.New("workdir is required")
	}
	if filepath.IsAbs(requested) || strings.Contains(requested, `\`) {
		return "", "", errors.New("absolute or backslash path")
	}
	clean := filepath.Clean(requested)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", "", errors.New("path traversal")
	}
	candidate := filepath.Join(root, clean)
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", "", errors.New("workdir is not a directory")
	}
	if !isWithin(root, resolved) {
		return "", "", errors.New("symlink escape")
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", "", err
	}
	return resolved, filepath.ToSlash(rel), nil
}

func validatePackage(cwd, pkg string) error {
	if pkg == "" || (pkg != "." && !strings.HasPrefix(pkg, "./")) || filepath.IsAbs(pkg) || strings.Contains(pkg, `\`) || !packagePattern.MatchString(pkg) {
		return fmt.Errorf("package selector %q rejected", pkg)
	}
	for _, part := range strings.Split(filepath.ToSlash(pkg), "/") {
		if part == ".." {
			return fmt.Errorf("package selector %q traverses", pkg)
		}
	}
	probe := strings.TrimSuffix(pkg, "/...")
	if probe == "." || probe == "" {
		probe = "."
	}
	candidate := filepath.Join(cwd, probe)
	if _, err := os.Lstat(candidate); err == nil {
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil || !isWithin(cwd, resolved) {
			return fmt.Errorf("package selector %q escapes through a symlink", pkg)
		}
	}
	return nil
}

func isWithin(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func strictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing JSON data")
	}
	return nil
}

func writeRPC(w http.ResponseWriter, response rpcResponse) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func mustJSON(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envInt(name string, fallback int) int {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		log.Fatalf("%s must be an integer", name)
	}
	return parsed
}
