package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/trader20control"
)

// Trader20ControlTool exposes exactly one read-only trader20.control.v1 operation.
type Trader20ControlTool struct {
	operation string
	client    *trader20control.Client
}

var trader20ReadOnlyOperations = []string{"capabilities", "status", "positions", "orders", "history", "explain_blocker", "runtime_health"}

func Trader20ReadOnlyOperations() []string {
	return append([]string(nil), trader20ReadOnlyOperations...)
}

func NewTrader20ControlToolsFromEnv() ([]Tool, error) {
	maxStaleness := 30 * time.Second
	if raw := strings.TrimSpace(os.Getenv("TRADER20_MAX_STALENESS_SECONDS")); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil || seconds <= 0 {
			return nil, fmt.Errorf("invalid TRADER20_MAX_STALENESS_SECONDS")
		}
		maxStaleness = time.Duration(seconds) * time.Second
	}
	client, err := trader20control.NewClient(trader20control.Config{
		InfoURL:      os.Getenv("TRADER20_HYPERLIQUID_INFO_URL"),
		Account:      os.Getenv("TRADER20_HYPERLIQUID_ACCOUNT"),
		CandidateSHA: os.Getenv("TRADER20_CANDIDATE_SHA"),
		PolicyHash:   os.Getenv("TRADER20_POLICY_HASH"),
		MaxStaleness: maxStaleness,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Tool, 0, len(trader20ReadOnlyOperations))
	for _, op := range trader20ReadOnlyOperations {
		out = append(out, &Trader20ControlTool{operation: op, client: client})
	}
	return out, nil
}

func NewTrader20ControlTool(operation string, client *trader20control.Client) *Trader20ControlTool {
	return &Trader20ControlTool{operation: operation, client: client}
}

func (t *Trader20ControlTool) Name() string { return "trader20_" + t.operation }
func (t *Trader20ControlTool) Description() string {
	return "Read-only trader20.control.v1 " + t.operation + " operation. Never signs, places, cancels, closes, transfers, or mutates a wallet."
}
func (t *Trader20ControlTool) Parameters() map[string]any {
	properties := map[string]any{}
	required := []string{}
	if t.operation == "history" {
		properties["start_time"] = map[string]any{"type": "string", "description": "RFC3339 inclusive start; maximum range is 7 days"}
		properties["end_time"] = map[string]any{"type": "string", "description": "RFC3339 exclusive end"}
		required = []string{"start_time", "end_time"}
	}
	out := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}
func (t *Trader20ControlTool) Execute(ctx context.Context, args map[string]any) *Result {
	if t.client == nil {
		return ErrorResult("trader20 read-only provider is not configured")
	}
	var env trader20control.Envelope
	var err error
	switch t.operation {
	case "capabilities":
		env = t.client.Capabilities()
	case "status":
		env, err = t.client.Status(ctx)
	case "positions":
		env, err = t.client.Positions(ctx)
	case "orders":
		env, err = t.client.Orders(ctx)
	case "history":
		start, startErr := parseTrader20Time(args, "start_time")
		end, endErr := parseTrader20Time(args, "end_time")
		if startErr != nil {
			return ErrorResult(startErr.Error())
		}
		if endErr != nil {
			return ErrorResult(endErr.Error())
		}
		env, err = t.client.History(ctx, start, end)
	case "explain_blocker":
		env, err = t.client.ExplainBlocker(ctx)
	case "runtime_health":
		env, err = t.client.RuntimeHealth(ctx)
	default:
		return ErrorResult("unsupported trader20 operation")
	}
	raw, marshalErr := json.Marshal(env)
	if marshalErr != nil {
		return ErrorResult(marshalErr.Error())
	}
	if err != nil {
		return ErrorResult(string(raw)).WithError(err)
	}
	return NewResult(string(raw))
}

func parseTrader20Time(args map[string]any, key string) (time.Time, error) {
	raw, _ := args[key].(string)
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be RFC3339", key)
	}
	return parsed, nil
}
