package trader20control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	ProtocolVersion = "trader20.control.v1"
	DefaultInfoURL  = "https://api.hyperliquid.xyz/info"
	maxResponseSize = 4 << 20
)

var accountPattern = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)
var gitSHAPattern = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
var hashPattern = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// Doer is the narrow HTTP seam used by the read-only Hyperliquid client.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Config contains identity and freshness bindings. Account is request-only and
// is never copied into an Envelope.
type Config struct {
	InfoURL           string
	Account           string
	CandidateSHA      string
	PolicyHash        string
	MaxStaleness      time.Duration
	HTTPClient        Doer
	AllowLoopbackHTTP bool   // tests and isolated local adapters only
	ControlSocket     string // dedicated local broker; never a network URL
}

// Envelope is the normalized GoClaw/MCP response contract.
type Envelope struct {
	Protocol        string          `json:"protocol"`
	Operation       string          `json:"operation"`
	CapturedAt      time.Time       `json:"captured_at"`
	SourceTimestamp *time.Time      `json:"source_timestamp,omitempty"`
	Stale           bool            `json:"stale"`
	Degraded        bool            `json:"degraded"`
	Reason          string          `json:"reason,omitempty"`
	CandidateSHA    string          `json:"candidate_sha,omitempty"`
	PolicyHash      string          `json:"policy_hash,omitempty"`
	Data            json.RawMessage `json:"data"`
	EffectAttempted bool            `json:"effect_attempted,omitempty"`
}

// Client intentionally has no exchange/signing/order mutation method.
type Client struct {
	cfg Config
	now func() time.Time
}

func NewClient(cfg Config) (*Client, error) {
	cfg.InfoURL = strings.TrimSpace(cfg.InfoURL)
	if cfg.InfoURL == "" {
		cfg.InfoURL = DefaultInfoURL
	}
	if cfg.ControlSocket == "" {
		if err := validateInfoURL(cfg.InfoURL, cfg.AllowLoopbackHTTP); err != nil {
			return nil, err
		}
	} else {
		const controlDir = "/run/trader20-haraldr-control"
		clean := filepath.Clean(cfg.ControlSocket)
		if clean != cfg.ControlSocket || filepath.Dir(clean) != controlDir || filepath.Base(clean) != "control.sock" {
			return nil, errors.New("control socket must be the exact dedicated socket path")
		}
		resolved, err := filepath.EvalSymlinks(controlDir)
		if err != nil || resolved != controlDir {
			return nil, errors.New("control socket directory identity invalid")
		}
	}
	cfg.Account = strings.TrimSpace(cfg.Account)
	if !accountPattern.MatchString(cfg.Account) {
		return nil, errors.New("Hyperliquid account is missing or malformed")
	}
	if cfg.CandidateSHA != "" && !gitSHAPattern.MatchString(cfg.CandidateSHA) {
		return nil, errors.New("candidate SHA must be 40 hexadecimal characters")
	}
	if cfg.PolicyHash != "" && !hashPattern.MatchString(cfg.PolicyHash) {
		return nil, errors.New("policy hash must be 64 hexadecimal characters")
	}
	if cfg.MaxStaleness <= 0 {
		cfg.MaxStaleness = 30 * time.Second
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 12 * time.Second}
	}
	return &Client{cfg: cfg, now: time.Now}, nil
}

func validateInfoURL(raw string, allowLoopbackHTTP bool) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid Hyperliquid info URL: %w", err)
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/info" {
		return errors.New("Hyperliquid endpoint must be the exact /info endpoint without credentials, query, or fragment")
	}
	host := strings.ToLower(u.Hostname())
	if u.Scheme == "https" && host == "api.hyperliquid.xyz" {
		return nil
	}
	if allowLoopbackHTTP && u.Scheme == "http" && (host == "127.0.0.1" || host == "::1" || host == "localhost") {
		return nil
	}
	return errors.New("Hyperliquid endpoint must be https://api.hyperliquid.xyz/info")
}

func (c *Client) Capabilities() Envelope {
	if c.cfg.ControlSocket != "" {
		env, err := c.Control(context.Background(), "capabilities", map[string]any{}, "")
		if err == nil {
			return env
		}
		return c.failure("capabilities", err)
	}
	data, _ := json.Marshal(map[string]any{
		"operations":                  []string{"capabilities", "status", "positions", "orders", "history", "explain_blocker", "runtime_health", "plan_trade", "execute_plan"},
		"provider_endpoint":           "/info",
		"control_endpoint_configured": false,
		"write_capabilities":          []string{"plan_trade", "execute_plan"},
		"signing_available":           false,
		"account_configured":          true,
	})
	return c.envelope("capabilities", data, nil, false, "")
}

func (c *Client) Status(ctx context.Context) (Envelope, error) {
	if c.cfg.ControlSocket != "" {
		return c.Control(ctx, "status", map[string]any{}, "")
	}
	raw, err := c.info(ctx, "clearinghouseState", map[string]any{"user": c.cfg.Account})
	if err != nil {
		return c.failure("status", err), err
	}
	var state map[string]json.RawMessage
	if err := json.Unmarshal(raw, &state); err != nil {
		return c.failure("status", err), err
	}
	data, _ := json.Marshal(map[string]json.RawMessage{
		"margin_summary":       state["marginSummary"],
		"cross_margin_summary": state["crossMarginSummary"],
		"withdrawable":         state["withdrawable"],
	})
	return c.envelope("status", data, c.observedNow(), false, ""), nil
}

func (c *Client) Positions(ctx context.Context) (Envelope, error) {
	if c.cfg.ControlSocket != "" {
		return c.Control(ctx, "positions", map[string]any{}, "")
	}
	raw, err := c.info(ctx, "clearinghouseState", map[string]any{"user": c.cfg.Account})
	if err != nil {
		return c.failure("positions", err), err
	}
	var state map[string]json.RawMessage
	if err := json.Unmarshal(raw, &state); err != nil {
		return c.failure("positions", err), err
	}
	data := state["assetPositions"]
	if len(data) == 0 {
		data = json.RawMessage(`[]`)
	}
	return c.envelope("positions", data, c.observedNow(), false, ""), nil
}

func (c *Client) Orders(ctx context.Context) (Envelope, error) {
	if c.cfg.ControlSocket != "" {
		return c.Control(ctx, "orders", map[string]any{}, "")
	}
	raw, err := c.info(ctx, "frontendOpenOrders", map[string]any{"user": c.cfg.Account})
	if err != nil {
		return c.failure("orders", err), err
	}
	return c.envelope("orders", raw, c.observedNow(), false, ""), nil
}

func (c *Client) History(ctx context.Context, start, end time.Time) (Envelope, error) {
	if start.IsZero() || end.IsZero() || !start.Before(end) {
		err := errors.New("history requires a valid start before end")
		return c.failure("history", err), err
	}
	if end.Sub(start) > 7*24*time.Hour {
		err := errors.New("history range exceeds 7 days")
		return c.failure("history", err), err
	}
	if c.cfg.ControlSocket != "" {
		return c.Control(ctx, "history", map[string]any{"start_time": start.UTC().Format(time.RFC3339), "end_time": end.UTC().Format(time.RFC3339)}, "")
	}
	raw, err := c.info(ctx, "userFillsByTime", map[string]any{"user": c.cfg.Account, "startTime": start.UnixMilli(), "endTime": end.UnixMilli()})
	if err != nil {
		return c.failure("history", err), err
	}
	return c.envelope("history", raw, c.observedNow(), false, ""), nil
}

func (c *Client) RuntimeHealth(ctx context.Context) (Envelope, error) {
	if c.cfg.ControlSocket != "" {
		return c.Control(ctx, "runtime_health", map[string]any{}, "")
	}
	raw, err := c.info(ctx, "allMids", nil)
	if err != nil {
		return c.failure("runtime_health", err), err
	}
	var mids map[string]json.RawMessage
	if err := json.Unmarshal(raw, &mids); err != nil {
		return c.failure("runtime_health", err), err
	}
	data, _ := json.Marshal(map[string]any{"provider_reachable": true, "markets_observed": len(mids), "account_configured": true, "signing_available": false})
	return c.envelope("runtime_health", data, c.observedNow(), false, ""), nil
}

func (c *Client) ExplainBlocker(ctx context.Context) (Envelope, error) {
	if c.cfg.ControlSocket != "" {
		return c.Control(ctx, "explain_blocker", map[string]any{}, "")
	}
	reasons := make([]string, 0, 2)
	if strings.TrimSpace(c.cfg.CandidateSHA) == "" {
		reasons = append(reasons, "candidate identity unavailable")
	}
	if strings.TrimSpace(c.cfg.PolicyHash) == "" {
		reasons = append(reasons, "policy identity unavailable")
	}
	if len(reasons) == 0 {
		if _, err := c.RuntimeHealth(ctx); err != nil {
			reasons = append(reasons, "provider readback unavailable")
		}
	}
	data, _ := json.Marshal(map[string]any{"blocked": len(reasons) > 0, "reasons": reasons, "trading_available": false})
	return c.envelope("explain_blocker", data, nil, len(reasons) > 0, strings.Join(reasons, "; ")), nil
}

func (c *Client) Control(ctx context.Context, operation string, params map[string]any, actorID string) (Envelope, error) {
	if c.cfg.ControlSocket == "" {
		err := errors.New("control transport unavailable")
		return c.failure(operation, err), err
	}
	allowed := map[string]bool{"capabilities": true, "status": true, "positions": true, "orders": true, "history": true, "explain_blocker": true, "runtime_health": true, "pause_entries": true, "resume_entries": true, "latch_kill": true, "cancel_pending_plan": true, "plan_trade": true, "execute_plan": true}
	if !allowed[operation] {
		err := errors.New("unsupported trader20 operation")
		return c.failure(operation, err), err
	}
	body, err := json.Marshal(map[string]any{"operation": operation, "params": params, "actor_id": actorID})
	if err != nil {
		return c.failure(operation, err), err
	}
	info, err := os.Lstat(c.cfg.ControlSocket)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		err = errors.New("control socket identity invalid")
		return c.failure(operation, err), err
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", c.cfg.ControlSocket)
	if err != nil {
		return c.failure(operation, err), err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(65 * time.Second))
	if _, err = conn.Write(append(body, '\n')); err != nil {
		return c.failure(operation, err), err
	}
	raw, err := io.ReadAll(io.LimitReader(conn, maxResponseSize+1))
	if err != nil {
		return c.failure(operation, err), err
	}
	if len(raw) > maxResponseSize {
		err = errors.New("control response exceeds 4 MiB")
		return c.failure(operation, err), err
	}
	var env Envelope
	if err = json.Unmarshal(bytes.TrimSpace(raw), &env); err != nil {
		return c.failure(operation, err), err
	}
	if env.Protocol != ProtocolVersion || (env.Operation != operation && env.Operation != "denied") {
		err = errors.New("control response binding invalid")
		return c.failure(operation, err), err
	}
	if env.Operation == "denied" || (env.Degraded && len(env.Data) == 0) {
		err = errors.New(env.Reason)
		return env, err
	}
	return env, nil
}

func (c *Client) info(ctx context.Context, typ string, payload map[string]any) (json.RawMessage, error) {
	allowed := map[string]bool{"clearinghouseState": true, "frontendOpenOrders": true, "userFillsByTime": true, "allMids": true}
	if !allowed[typ] {
		return nil, fmt.Errorf("blocked Hyperliquid info type %q", typ)
	}
	body := map[string]any{"type": typ}
	for k, v := range payload {
		body[k] = v
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.InfoURL, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return nil, err
	}
	if len(out) > maxResponseSize {
		return nil, errors.New("Hyperliquid response exceeds 4 MiB")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Hyperliquid info returned HTTP %d", resp.StatusCode)
	}
	if !json.Valid(out) {
		return nil, errors.New("Hyperliquid info returned invalid JSON")
	}
	return out, nil
}

func (c *Client) envelope(op string, data json.RawMessage, source *time.Time, degraded bool, reason string) Envelope {
	now := c.now().UTC()
	bindings := make([]string, 0, 2)
	if c.cfg.CandidateSHA == "" {
		bindings = append(bindings, "candidate identity unavailable")
	}
	if c.cfg.PolicyHash == "" {
		bindings = append(bindings, "policy identity unavailable")
	}
	if len(bindings) > 0 {
		degraded = true
		if reason == "" {
			reason = strings.Join(bindings, "; ")
		}
	}
	stale := source != nil && now.Sub(*source) > c.cfg.MaxStaleness
	if stale {
		degraded = true
		if reason == "" {
			reason = "source data is stale"
		}
	}
	if len(data) == 0 {
		data = json.RawMessage(`null`)
	}
	return Envelope{Protocol: ProtocolVersion, Operation: op, CapturedAt: now, SourceTimestamp: source, Stale: stale, Degraded: degraded, Reason: reason, CandidateSHA: c.cfg.CandidateSHA, PolicyHash: c.cfg.PolicyHash, Data: data}
}

func (c *Client) observedNow() *time.Time {
	t := c.now().UTC()
	return &t
}

func (c *Client) failure(op string, err error) Envelope {
	return c.envelope(op, json.RawMessage(`null`), nil, true, err.Error())
}
