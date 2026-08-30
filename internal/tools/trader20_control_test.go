package tools

import (
	"context"
	"strings"
	"testing"
)

func TestTrader20ToolsRemainVisibleAndFailClosedWhenUnconfigured(t *testing.T) {
	for _, operation := range Trader20ReadOnlyOperations() {
		tool := NewTrader20ControlTool(operation, nil)
		if tool.Name() != "trader20_"+operation {
			t.Fatalf("name = %q", tool.Name())
		}
		if !strings.Contains(strings.ToLower(tool.Description()), "never signs") {
			t.Fatalf("description lacks safety boundary: %s", tool.Description())
		}
		result := tool.Execute(context.Background(), map[string]any{})
		if !result.IsError || !strings.Contains(result.ForLLM, "not configured") {
			t.Fatalf("unconfigured %s result = %#v", operation, result)
		}
	}
}

func TestTrader20HistorySchemaRequiresBoundedTimes(t *testing.T) {
	tool := NewTrader20ControlTool("history", nil)
	params := tool.Parameters()
	required, ok := params["required"].([]string)
	if !ok || len(required) != 2 {
		t.Fatalf("required = %#v", params["required"])
	}
	if params["additionalProperties"] != false {
		t.Fatal("history schema permits undeclared parameters")
	}
}
