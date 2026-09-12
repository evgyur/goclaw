package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

var trader20CommandRoutes = map[string]string{
	"/start": "menu", "/menu": "menu", "/dash": "dash", "/status": "dash",
	"/pos": "pos", "/positions": "pos", "/orders": "orders", "/fills": "fills",
	"/history": "fills", "/copied": "copied", "/leaders": "copied", "/risk": "risk",
	"/blocks": "blocks", "/analytics": "analytics", "/funding": "funding",
	"/markets": "markets", "/hip3": "markets", "/performance": "perf:24h",
	"/perf": "perf:24h", "/report": "perf:24h", "/links": "links",
	"/export": "export", "/alerts": "alerts",
	"/pause": "pause", "/resume": "resume", "/kill": "kill", "/plan": "plan", "/execute": "execute", "/cancel": "cancel",
}

func trader20ModeEnabled() bool {
	return strings.TrimSpace(os.Getenv("TRADER20_HYPERLIQUID_ACCOUNT")) != ""
}

func trader20MenuCommands() []telego.BotCommand {
	return []telego.BotCommand{
		{Command: "menu", Description: "Open trader20 button menu"},
		{Command: "dash", Description: "Read-only trader20 dashboard"},
		{Command: "pos", Description: "Current positions"},
		{Command: "orders", Description: "Open orders"},
		{Command: "fills", Description: "Fills and history"},
		{Command: "risk", Description: "Risk and reconciliation"},
		{Command: "blocks", Description: "Why entry is blocked"},
		{Command: "performance", Description: "Copy performance evidence"},
		{Command: "markets", Description: "Market readback"},
		{Command: "alerts", Description: "Alert delivery status"},
		{Command: "pause", Description: "Pause new Trader20 entries"},
		{Command: "resume", Description: "Resume entries after live gates"},
		{Command: "kill", Description: "Latch fail-closed entry kill"},
		{Command: "plan", Description: "Prepare an exact bounded trade plan"},
		{Command: "execute", Description: "Execute an explicitly approved plan"},
		{Command: "cancel", Description: "Cancel a pending exact plan"},
	}
}

func trader20Keyboard() *telego.InlineKeyboardMarkup {
	button := func(text, route string) telego.InlineKeyboardButton {
		return telego.InlineKeyboardButton{Text: text, CallbackData: "hl:" + route}
	}
	return &telego.InlineKeyboardMarkup{InlineKeyboard: [][]telego.InlineKeyboardButton{
		{button("📊 Dashboard", "dash"), button("📈 Positions", "pos")},
		{button("📬 Orders", "orders"), button("🧾 Fills", "fills")},
		{button("🛡 Risk", "risk"), button("🛑 Blocks", "blocks")},
		{button("⏱ Performance", "perf:24h"), button("🅷 Markets", "markets")},
		{button("🧾 Plan trade", "plan"), button("✅ Execute plan", "execute")},
		{button("✖ Cancel plan", "cancel"), button("⏸ Pause entries", "pause")},
		{button("⚙️ Alerts", "alerts"), button("🧭 Menu", "menu")},
	}}
}

func trader20MenuText() string {
	return "🧭 trader20 management cockpit\n\nStatus is evidence-backed. Planning never trades. Execution requires a fresh exact plan hash and returns an authoritative receipt from the incumbent single writer."
}

func trader20RoutePrompt(route string, now time.Time) string {
	end := now.UTC().Truncate(time.Second)
	start := end.Add(-7 * 24 * time.Hour)
	historyWindow := fmt.Sprintf("start_time=%s and end_time=%s", start.Format(time.RFC3339), end.Format(time.RFC3339))
	prefix := "This is the Haraldr trader20 management cockpit with read-only evidence and bounded controls. Use only trader20_* tools. Never claim an effect until the tool returns an authoritative receipt. "
	switch route {
	case "dash":
		return prefix + "Call trader20_status, trader20_positions, trader20_orders, and trader20_runtime_health; return a concise dashboard with explicit evidence timestamps and degraded state."
	case "pos":
		return prefix + "Call trader20_positions and summarize each open position."
	case "orders":
		return prefix + "Call trader20_orders and summarize every open order."
	case "fills":
		return prefix + "Call trader20_history with " + historyWindow + "; summarize fills and state that the contract window is seven days."
	case "risk", "blocks":
		return prefix + "Call trader20_explain_blocker, trader20_status, and trader20_runtime_health; explain the first decisive read-only or degraded gate plainly."
	case "pause":
		return prefix + "The owner explicitly requested entry pause. Call trader20_pause_entries with a short reason, then report the returned state."
	case "resume":
		return prefix + "The owner explicitly requested entry resume. Call trader20_resume_entries; it must fail closed unless activation, websocket, timer, and kill gates pass."
	case "kill":
		return prefix + "The owner explicitly requested the irreversible-in-chat kill latch. Call trader20_latch_kill with a short reason. Never offer an in-chat unlatch."
	case "plan":
		return prefix + "Collect all exact plan fields and call trader20_plan_trade only when complete. Planning is not execution."
	case "execute":
		return prefix + "Execute only the exact plan hash explicitly approved in this message using trader20_execute_plan. Never infer approval from an earlier plan."
	case "cancel":
		return prefix + "Cancel only the exact pending plan hash named in this message using trader20_cancel_pending_plan. Ask for the hash if it is absent."
	case "perf:1h", "perf:24h", "perf:7d", "perf:30d":
		return prefix + "Call trader20_history with " + historyWindow + "; summarize only directly supported performance evidence. If the requested route exceeds seven days, state the contract limit rather than extrapolating."
	default:
		return prefix + "Call trader20_capabilities and trader20_explain_blocker. The requested legacy route is " + route + "; if the frozen contract does not expose its data, say so plainly and list the supported read-only operations."
	}
}

func (c *Channel) sendTrader20Menu(ctx context.Context, chatID int64) {
	msg := tu.Message(tu.ID(chatID), trader20MenuText())
	msg.ReplyMarkup = trader20Keyboard()
	if _, err := c.bot.SendMessage(ctx, msg); err != nil {
		slog.Warn("trader20 menu delivery failed", "chat_id", chatID, "error", err)
	}
}

func (c *Channel) publishTrader20Route(route, chatID, senderID string, isGroup bool) {
	peerKind := "direct"
	if isGroup {
		peerKind = "group"
	}
	c.Bus().PublishInbound(bus.InboundMessage{
		Channel: c.Name(), SenderID: senderID, ChatID: chatID,
		Content: trader20RoutePrompt(route, time.Now()), PeerKind: peerKind,
		AgentID: c.AgentID(), UserID: senderID, TenantID: c.TenantID(),
		Metadata: map[string]string{"command": "trader20_route", "route": route, "local_key": chatID},
	})
}

func (c *Channel) handleTrader20Command(ctx context.Context, cmd string, chatID int64, senderID string, isGroup bool) bool {
	if !trader20ModeEnabled() {
		return false
	}
	route, ok := trader20CommandRoutes[cmd]
	if !ok {
		return false
	}
	if route == "menu" {
		c.sendTrader20Menu(ctx, chatID)
		return true
	}
	c.publishTrader20Route(route, fmt.Sprintf("%d", chatID), strings.SplitN(senderID, "|", 2)[0], isGroup)
	return true
}

func (c *Channel) handleTrader20Callback(ctx context.Context, query *telego.CallbackQuery) bool {
	if !trader20ModeEnabled() || !strings.HasPrefix(query.Data, "hl:") || query.Message == nil {
		return false
	}
	userID := fmt.Sprintf("%d", query.From.ID)
	// Callback queries bypass the normal message gate. Require an explicit
	// deployment allowlist so a forwarded legacy keyboard cannot grant access.
	if !c.HasAllowList() || !c.IsAllowed(userID) {
		return true
	}
	route := strings.TrimPrefix(query.Data, "hl:")
	if _, ok := trader20CommandRoutes["/"+route]; !ok && !strings.HasPrefix(route, "perf:") {
		route = "dash"
	}
	chat := query.Message.GetChat()
	if route == "menu" {
		c.sendTrader20Menu(ctx, chat.ID)
		return true
	}
	isGroup := chat.Type == "group" || chat.Type == "supergroup"
	c.publishTrader20Route(route, fmt.Sprintf("%d", chat.ID), userID, isGroup)
	return true
}
