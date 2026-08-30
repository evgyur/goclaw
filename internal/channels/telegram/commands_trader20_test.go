package telegram

import (
	"strings"
	"testing"
	"time"
)

func TestTrader20RoutePromptUsesBoundedHistoryWindow(t *testing.T) {
	now := time.Date(2026, 8, 30, 20, 0, 0, 0, time.UTC)
	prompt := trader20RoutePrompt("fills", now)
	for _, want := range []string{"2026-08-23T20:00:00Z", "2026-08-30T20:00:00Z", "trader20_history", "read-only"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, prompt)
		}
	}
}

func TestTrader20KeyboardPreservesLegacyCallbackPrefix(t *testing.T) {
	keyboard := trader20Keyboard()
	if len(keyboard.InlineKeyboard) == 0 {
		t.Fatal("empty trader20 keyboard")
	}
	for _, row := range keyboard.InlineKeyboard {
		for _, button := range row {
			if !strings.HasPrefix(button.CallbackData, "hl:") {
				t.Fatalf("unexpected callback data %q", button.CallbackData)
			}
		}
	}
}

func TestTrader20UnsupportedLegacyRouteFailsClosedToCapabilities(t *testing.T) {
	prompt := trader20RoutePrompt("funding", time.Unix(0, 0))
	if !strings.Contains(prompt, "trader20_capabilities") || !strings.Contains(prompt, "does not expose") {
		t.Fatalf("unsupported route prompt is not fail-closed: %s", prompt)
	}
}
