package telegram

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/audio/proxy_stt"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// telegramCreds maps the credentials JSON from the channel_instances table.
type telegramCreds struct {
	Token     string `json:"token"`
	Proxy     string `json:"proxy,omitempty"`
	APIServer string `json:"api_server,omitempty"`
}

// telegramInstanceConfig maps the non-secret config JSONB from the channel_instances table.
type telegramInstanceConfig struct {
	APIServer       string   `json:"api_server,omitempty"`
	Proxy           string   `json:"proxy,omitempty"`
	DMPolicy        string   `json:"dm_policy,omitempty"`
	GroupPolicy     string   `json:"group_policy,omitempty"`
	RequireMention  *bool    `json:"require_mention,omitempty"`
	MentionMode     string   `json:"mention_mode,omitempty"`
	HistoryLimit    int      `json:"history_limit,omitempty"`
	DMStream        *bool    `json:"dm_stream,omitempty"`
	GroupStream     *bool    `json:"group_stream,omitempty"`
	DraftTransport  *bool    `json:"draft_transport,omitempty"`  // sendMessageDraft for DM streaming (default true)
	ReasoningStream *bool    `json:"reasoning_stream,omitempty"` // show reasoning as separate message (default true)
	ReactionLevel   string   `json:"reaction_level,omitempty"`
	MediaMaxMB      int64    `json:"media_max_mb,omitempty"`
	MediaMaxBytes   int64    `json:"media_max_bytes,omitempty"` // deprecated: use media_max_mb
	LinkPreview     *bool    `json:"link_preview,omitempty"`
	BlockReply      *bool    `json:"block_reply,omitempty"`
	ForceIPv4       bool     `json:"force_ipv4,omitempty"`
	AllowFrom       []string `json:"allow_from,omitempty"`

	STTProxyURL       string `json:"stt_proxy_url,omitempty"`
	STTProvider       string `json:"stt_provider,omitempty"`
	STTModel          string `json:"stt_model,omitempty"`
	STTAPIKey         string `json:"stt_api_key,omitempty"`
	STTTenantID       string `json:"stt_tenant_id,omitempty"`
	STTTimeoutSeconds int    `json:"stt_timeout_seconds,omitempty"`
}

// Factory creates a Telegram channel from DB instance data (no extra stores).
func Factory(name string, creds json.RawMessage, cfg json.RawMessage,
	msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {
	return buildChannel(name, creds, cfg, msgBus, pairingSvc, nil)
}

// FactoryWithStores returns a ChannelFactory that includes optional stores via functional options.
func FactoryWithStores(agentStore store.AgentStore, configPermStore store.ConfigPermissionStore, teamStore store.TeamStore, subagentTaskStore store.SubagentTaskStore, pendingStore store.PendingMessageStore) channels.ChannelFactory {
	return FactoryWithStoresAndAudio(agentStore, configPermStore, teamStore, subagentTaskStore, pendingStore, nil)
}

// FactoryWithStoresAndAudio returns a ChannelFactory with all stores and STT support.
func FactoryWithStoresAndAudio(agentStore store.AgentStore, configPermStore store.ConfigPermissionStore, teamStore store.TeamStore, subagentTaskStore store.SubagentTaskStore, pendingStore store.PendingMessageStore, audioMgr *audio.Manager) channels.ChannelFactory {
	return func(name string, creds json.RawMessage, cfg json.RawMessage,
		msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {
		return buildChannel(name, creds, cfg, msgBus, pairingSvc, audioMgr,
			WithAgentStore(agentStore),
			WithConfigPermStore(configPermStore),
			WithTeamStore(teamStore),
			WithSubagentTaskStore(subagentTaskStore),
			WithPendingMessageStore(pendingStore),
		)
	}
}

func buildChannel(name string, creds json.RawMessage, cfg json.RawMessage,
	msgBus *bus.MessageBus, pairingSvc store.PairingStore, audioMgr *audio.Manager, opts ...Option) (channels.Channel, error) {

	var c telegramCreds
	if len(creds) > 0 {
		if err := json.Unmarshal(creds, &c); err != nil {
			return nil, fmt.Errorf("decode telegram credentials: %w", err)
		}
	}
	if c.Token == "" {
		return nil, fmt.Errorf("telegram token is required")
	}

	var ic telegramInstanceConfig
	if len(cfg) > 0 {
		if err := json.Unmarshal(cfg, &ic); err != nil {
			return nil, fmt.Errorf("decode telegram config: %w", err)
		}
	}

	// Prefer config values; fall back to credentials for backward compat.
	proxy := ic.Proxy
	if proxy == "" {
		proxy = c.Proxy
	}
	apiServer := ic.APIServer
	if apiServer == "" {
		apiServer = c.APIServer
	}
	sttProvider := firstNonEmpty(ic.STTProvider, os.Getenv("GOCLAW_TELEGRAM_STT_PROVIDER"))
	sttModel := firstNonEmpty(ic.STTModel, os.Getenv("GOCLAW_TELEGRAM_STT_MODEL"))
	sttProxyURL := firstNonEmpty(ic.STTProxyURL, os.Getenv("GOCLAW_TELEGRAM_STT_PROXY_URL"))
	sttAPIKey := firstNonEmpty(ic.STTAPIKey, os.Getenv("GOCLAW_TELEGRAM_STT_API_KEY"), os.Getenv("GOCLAW_GROQ_API_KEY"))
	sttTenantID := firstNonEmpty(ic.STTTenantID, os.Getenv("GOCLAW_TELEGRAM_STT_TENANT_ID"))
	sttTimeoutSeconds := ic.STTTimeoutSeconds
	if sttTimeoutSeconds <= 0 {
		if v := os.Getenv("GOCLAW_TELEGRAM_STT_TIMEOUT_SECONDS"); v != "" {
			var parsed int
			if _, err := fmt.Sscanf(v, "%d", &parsed); err == nil && parsed > 0 {
				sttTimeoutSeconds = parsed
			}
		}
	}

	tgCfg := config.TelegramConfig{
		Enabled:         true,
		Token:           c.Token,
		Proxy:           proxy,
		APIServer:       apiServer,
		AllowFrom:       ic.AllowFrom,
		DMPolicy:        ic.DMPolicy,
		GroupPolicy:     ic.GroupPolicy,
		RequireMention:  ic.RequireMention,
		MentionMode:     ic.MentionMode,
		HistoryLimit:    ic.HistoryLimit,
		DMStream:        ic.DMStream,
		GroupStream:     ic.GroupStream,
		DraftTransport:  ic.DraftTransport,
		ReasoningStream: ic.ReasoningStream,
		ReactionLevel:   ic.ReactionLevel,
		MediaMaxBytes:   resolveMediaMaxBytes(ic),
		LinkPreview:     ic.LinkPreview,
		BlockReply:      ic.BlockReply,
		ForceIPv4:       ic.ForceIPv4,

		STTProxyURL:       sttProxyURL,
		STTProvider:       sttProvider,
		STTModel:          sttModel,
		STTAPIKey:         sttAPIKey,
		STTTenantID:       sttTenantID,
		STTTimeoutSeconds: sttTimeoutSeconds,
	}

	// DB instances default to "pairing" for groups (secure by default).
	// Config-based channels keep "open" default for backward compat.
	if tgCfg.GroupPolicy == "" {
		tgCfg.GroupPolicy = "pairing"
	}

	ch, err := New(tgCfg, msgBus, pairingSvc, audioMgr, opts...)
	if err != nil {
		return nil, err
	}

	// Override the channel name from DB instance.
	ch.SetName(name)
	registerInstanceSTT(audioMgr, name, sttProvider, sttModel, sttProxyURL, sttAPIKey, sttTenantID, sttTimeoutSeconds)
	return ch, nil
}

// resolveMediaMaxBytes converts media_max_mb (preferred) to bytes,
// falling back to the deprecated media_max_bytes for backward compat.
func resolveMediaMaxBytes(ic telegramInstanceConfig) int64 {
	if ic.MediaMaxMB > 0 {
		return ic.MediaMaxMB * 1024 * 1024
	}
	return ic.MediaMaxBytes
}

func registerInstanceSTT(audioMgr *audio.Manager, channelName, provider, model, proxyURL, apiKey, tenantID string, timeoutSeconds int) {
	if audioMgr == nil {
		return
	}
	if proxyURL == "" && provider != "groq" {
		return
	}
	audioMgr.RegisterChannelSTT(channelName, proxy_stt.NewProvider(media.STTConfig{
		Provider:       provider,
		Model:          model,
		ProxyURL:       proxyURL,
		APIKey:         apiKey,
		TenantID:       tenantID,
		TimeoutSeconds: timeoutSeconds,
	}))
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
