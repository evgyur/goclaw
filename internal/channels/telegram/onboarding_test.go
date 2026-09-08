package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mymmrac/telego"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func TestOnboardingTextAndButtons(t *testing.T) {
	for _, action := range []string{onboardingActionHome, onboardingActionAsk, onboardingActionExamples, onboardingActionSources} {
		text, ok := onboardingText(action)
		if !ok || text == "" {
			t.Fatalf("missing onboarding content for %q", action)
		}
		keyboard := onboardingKeyboard(action)
		if keyboard == nil || len(keyboard.InlineKeyboard) == 0 {
			t.Fatalf("missing keyboard for %q", action)
		}
	}
	if _, ok := onboardingText("unknown"); ok {
		t.Fatal("unknown onboarding action accepted")
	}
	home := onboardingKeyboard(onboardingActionHome)
	if !strings.HasPrefix(onboardingWelcome, "👋") || !strings.Contains(onboardingWelcome, "🔎") || !strings.Contains(onboardingWelcome, "🚗") || !strings.Contains(onboardingWelcome, "💡") {
		t.Fatal("welcome emoji hierarchy missing")
	}
	if home.InlineKeyboard[0][0].Text != "✍️ Задать вопрос" || home.InlineKeyboard[1][0].Text != "💡 Примеры" || home.InlineKeyboard[1][1].Text != "🔗 Об источниках" {
		t.Fatalf("unexpected onboarding button copy: %#v", home.InlineKeyboard)
	}
	if got := home.InlineKeyboard[0][0].CallbackData; got != onboardingCallbackPrefix+onboardingActionAsk {
		t.Fatalf("ask callback = %q", got)
	}
}

func TestOnboardingMenuIsProductOnly(t *testing.T) {
	commands := OnboardingMenuCommands()
	if len(commands) != 2 || commands[0].Command != "start" || commands[1].Command != "help" {
		t.Fatalf("unexpected onboarding menu: %#v", commands)
	}
}

func TestOnboardingCallbackAuthorization(t *testing.T) {
	cfg := config.TelegramConfig{
		Token:             "123:test",
		DMPolicy:          "allowlist",
		AllowFrom:         config.FlexibleStringSlice{"617744661"},
		OnboardingEnabled: true,
	}
	ch := &Channel{BaseChannel: channels.NewBaseChannel(channels.TypeTelegram, nil, cfg.AllowFrom), config: cfg}
	message := &telego.Message{MessageID: 1, Chat: telego.Chat{ID: 617744661, Type: "private"}}
	allowed := &telego.CallbackQuery{From: telego.User{ID: 617744661}, Message: message, Data: onboardingCallbackPrefix + onboardingActionHome}
	if !ch.onboardingCallbackAllowed(allowed) {
		t.Fatal("owner private callback rejected")
	}
	foreign := *allowed
	foreign.From.ID = 42
	if ch.onboardingCallbackAllowed(&foreign) {
		t.Fatal("foreign callback accepted")
	}
	groupMessage := *message
	groupMessage.Chat.Type = "supergroup"
	group := *allowed
	group.Message = &groupMessage
	if ch.onboardingCallbackAllowed(&group) {
		t.Fatal("group callback accepted")
	}
	disabled := *ch
	disabled.config.OnboardingEnabled = false
	if disabled.onboardingCallbackAllowed(allowed) {
		t.Fatal("disabled onboarding callback accepted")
	}
	emptyAllowlist := &Channel{BaseChannel: channels.NewBaseChannel(channels.TypeTelegram, nil, nil), config: config.TelegramConfig{OnboardingEnabled: true, DMPolicy: "allowlist"}}
	if emptyAllowlist.onboardingCallbackAllowed(allowed) {
		t.Fatal("empty allowlist accepted onboarding callback")
	}
}

func TestFactoryPropagatesOnboardingEnabled(t *testing.T) {
	creds := json.RawMessage(`{"token":"123456789:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghi"}`)
	cfg := json.RawMessage(`{"dm_policy":"allowlist","allow_from":["617744661"],"onboarding_enabled":true}`)
	built, err := buildChannel("lixiang", creds, cfg, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ch := built.(*Channel)
	if !ch.config.OnboardingEnabled {
		t.Fatal("factory dropped onboarding_enabled")
	}
}

func TestStartVariantsAreHandledWithoutAgentFallback(t *testing.T) {
	var sends atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			sends.Add(1)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"result": map[string]any{
				"message_id": 2,
				"date":       1,
				"chat":       map[string]any{"id": 617744661, "type": "private"},
			},
		})
	}))
	defer server.Close()
	cfg := config.TelegramConfig{Token: "123456789:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghi", APIServer: server.URL, DMPolicy: "allowlist", AllowFrom: config.FlexibleStringSlice{"617744661"}, OnboardingEnabled: true}
	ch, err := New(cfg, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	message := &telego.Message{MessageID: 1, Chat: telego.Chat{ID: 617744661, Type: "private"}, From: &telego.User{ID: 617744661}}
	for _, text := range []string{"/start", "/start payload", "/start@lixiangchataibot", "/help"} {
		if !ch.handleBotCommand(context.Background(), message, 617744661, "617744661", "617744661", text, "617744661", false, false, 0) {
			t.Fatalf("onboarding command fell through: %q", text)
		}
	}
	if sends.Load() != 4 {
		t.Fatalf("send count = %d", sends.Load())
	}
	disabled := *ch
	disabled.config.OnboardingEnabled = false
	if disabled.handleBotCommand(context.Background(), message, 617744661, "617744661", "617744661", "/start", "617744661", false, false, 0) {
		t.Fatal("default /start behavior changed for non-onboarding channel")
	}
}
