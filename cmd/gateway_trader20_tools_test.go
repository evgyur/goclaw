package cmd

import "testing"

func TestBuiltinTrader20ToolsAreReadOnlyGrantSurfaceAndDisabledByDefault(t *testing.T) {
	want := map[string]bool{
		"trader20_capabilities":    false,
		"trader20_status":          false,
		"trader20_positions":       false,
		"trader20_orders":          false,
		"trader20_history":         false,
		"trader20_explain_blocker": false,
		"trader20_runtime_health":  false,
		"trader20_plan_trade":      false,
		"trader20_execute_plan":    false,
	}
	for _, def := range builtinToolSeedData() {
		if _, ok := want[def.Name]; !ok {
			continue
		}
		if def.Category != "trader20" {
			t.Fatalf("%s category = %q", def.Name, def.Category)
		}
		if def.Enabled {
			t.Fatalf("%s must require an explicit grant", def.Name)
		}
		want[def.Name] = true
	}
	for name, found := range want {
		if !found {
			t.Fatalf("missing builtin %s", name)
		}
	}
}
