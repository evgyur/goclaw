package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

const scoutCommandTimeout = 25 * time.Second

type scoutCommandResult struct {
	OK        bool             `json:"ok"`
	Action    string           `json:"action"`
	Error     string           `json:"error"`
	Job       map[string]any   `json:"job"`
	Source    map[string]any   `json:"source"`
	Cards     []map[string]any `json:"cards"`
	Promotion map[string]any   `json:"promotion"`
	Muted     map[string]any   `json:"muted"`
}

// handleScoutCommand bridges Telegram /scout commands to the governed Scout runtime.
// The runtime is intentionally isolated from Telegram and mem0g; this wrapper only
// executes the audited file-backed contract and renders a compact operator reply.
func (c *Channel) handleScoutCommand(ctx context.Context, chatID int64, text string, setThread func(*telego.SendMessageParams)) {
	result, stderr, err := runScoutRuntime(ctx, text)
	var reply string
	if err != nil {
		reply = fmt.Sprintf("Scout error: %s", strings.TrimSpace(err.Error()))
		if stderr != "" {
			reply += "\n" + truncateForTelegram(stderr, 1200)
		}
	} else if !result.OK {
		reply = "Scout blocked: " + firstNonEmpty(result.Error, "unknown runtime error")
	} else {
		reply = formatScoutResult(result)
	}
	msg := tu.Message(tu.ID(chatID), reply)
	msg.ParseMode = telego.ModeHTML
	setThread(msg)
	_, _ = c.bot.SendMessage(ctx, msg)
}

func runScoutRuntime(ctx context.Context, text string) (*scoutCommandResult, string, error) {
	runtimePath := firstNonEmpty(os.Getenv("SCOUT_RUNTIME"), "/opt/goclaw-inbox/scout/runtime/scout_runtime.py")
	repoRoot := firstNonEmpty(os.Getenv("SCOUT_REPO_ROOT"), filepath.Dir(filepath.Dir(filepath.Dir(runtimePath))))
	stateRoot := firstNonEmpty(os.Getenv("SCOUT_STATE_ROOT"), "/opt/goclaw-inbox/scout/state")
	python := firstNonEmpty(os.Getenv("SCOUT_PYTHON"), "python3")

	cmdCtx, cancel := context.WithTimeout(ctx, scoutCommandTimeout)
	defer cancel()

	args := []string{runtimePath, "--state-root", stateRoot, "--repo-root", repoRoot, text}
	cmd := exec.CommandContext(cmdCtx, python, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// Policy blocks return JSON on stderr with exit 2. Preserve the user-facing reason.
		if parsed := parseScoutJSON(stderr.Bytes()); parsed != nil {
			return parsed, strings.TrimSpace(stderr.String()), nil
		}
		return nil, strings.TrimSpace(stderr.String()), err
	}
	parsed := parseScoutJSON(stdout.Bytes())
	if parsed == nil {
		return nil, strings.TrimSpace(stderr.String()), fmt.Errorf("invalid scout runtime JSON")
	}
	return parsed, strings.TrimSpace(stderr.String()), nil
}

func parseScoutJSON(raw []byte) *scoutCommandResult {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var result scoutCommandResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil
	}
	return &result
}

func formatScoutResult(result *scoutCommandResult) string {
	switch result.Action {
	case "watch":
		return fmt.Sprintf("Scout watch ok\n┈ source: <code>%s</code>", htmlEscape(asString(result.Source["id"])))
	case "pull":
		jobID := htmlEscape(asString(result.Job["id"]))
		return fmt.Sprintf("Scout pull staged\n┈ job: <code>%s</code>\n┈ items: %v\n┈ route: <code>%s/%s</code>\n┈ next: <code>/scout review %s limit=100</code>", jobID, result.Job["item_count"], htmlEscape(mapString(result.Job, "model_route", "provider")), htmlEscape(mapString(result.Job, "model_route", "model")), jobID)
	case "review":
		var b strings.Builder
		fmt.Fprintf(&b, "Scout review ready\n┈ cards: %d\n", len(result.Cards))
		limit := len(result.Cards)
		if limit > 100 {
			limit = 100
		}
		for i := 0; i < limit; i++ {
			card := result.Cards[i]
			fmt.Fprintf(&b, "\n%d. <b>%s</b>\n┈ %s\n┈ %s\n┈ save: <code>/scout promote %s</code>", i+1, htmlEscape(asString(card["repo"])), htmlEscape(truncateForTelegram(asString(card["reason"]), 220)), htmlEscape(asString(card["github_url"])), htmlEscape(asString(card["finding_id"])))
		}
		return b.String()
	case "promote":
		return fmt.Sprintf("Scout promotion staged for mem0g\n┈ finding: <code>%s</code>\n┈ status: %s", htmlEscape(asString(result.Promotion["finding_id"])), htmlEscape(asString(result.Promotion["status"])))
	case "mute":
		return fmt.Sprintf("Scout muted\n┈ pattern: <code>%s</code>", htmlEscape(asString(result.Muted["pattern"])))
	default:
		return "Scout ok"
	}
}

func mapString(m map[string]any, key string, nested string) string {
	child, _ := m[key].(map[string]any)
	return asString(child[nested])
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func truncateForTelegram(s string, max int) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max-1]) + "…"
}

func htmlEscape(s string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return replacer.Replace(s)
}
