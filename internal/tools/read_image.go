package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// --- Context helpers for media images ---

const ctxMediaImages toolContextKey = "tool_media_images"

// WithMediaImages stores base64-encoded images in context for read_image tool access.
func WithMediaImages(ctx context.Context, images []providers.ImageContent) context.Context {
	return context.WithValue(ctx, ctxMediaImages, images)
}

// MediaImagesFromCtx retrieves stored images from context.
func MediaImagesFromCtx(ctx context.Context) []providers.ImageContent {
	v, _ := ctx.Value(ctxMediaImages).([]providers.ImageContent)
	return v
}

// --- ReadImageTool ---

// visionProviderPriority is the order in which providers are tried for vision.
// claude-cli follows anthropic so installations with a native Anthropic API key
// keep using the faster direct API, while claude-cli-only setups still resolve.
var visionProviderPriority = []string{"minimax", "openrouter", "gemini", "anthropic", "claude-cli", "dashscope"}

// visionModelDefaults maps provider names to preferred vision models.
// Empty string lets the provider pick its own default model.
var visionModelDefaults = map[string]string{
	"minimax":    "MiniMax-M2.7-highspeed",
	"openrouter": "google/gemini-2.5-flash-image",
	"gemini":     "gemini-2.5-flash",
	"anthropic":  "",
	"claude-cli": "",
	"dashscope":  "qwen3-vl",
}

// ReadImageTool uses a vision-capable provider to describe images attached to the current message.
type ReadImageTool struct {
	registry *providers.Registry
}

func NewReadImageTool(registry *providers.Registry) *ReadImageTool {
	return &ReadImageTool{registry: registry}
}

func (t *ReadImageTool) Name() string { return "read_image" }

func (t *ReadImageTool) Description() string {
	return "Analyze images using vision AI. Works with: (1) images sent by the user (<media:image> tags), (2) workspace/generated image files (pass a file path)."
}

func (t *ReadImageTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "What you want to know about the image(s). E.g. 'Describe this image in detail' or 'What text is in this image?'",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Optional file path to an image in the workspace. Use this for generated images or attachments. If omitted, analyzes images from the conversation.",
			},
		},
		"required": []string{"prompt"},
	}
}

// maxImageFileBytes is the max size for loading workspace images (10MB).
const maxImageFileBytes = 10 * 1024 * 1024

func (t *ReadImageTool) Execute(ctx context.Context, args map[string]any) *Result {
	prompt, _ := args["prompt"].(string)
	if prompt == "" {
		prompt = "Describe this image in detail."
	}

	// If path is provided, load image from workspace file
	images := MediaImagesFromCtx(ctx)
	if imgPath, _ := args["path"].(string); imgPath != "" {
		fileImages, err := t.loadImageFromPath(ctx, imgPath)
		if err != nil {
			return ErrorResult(err.Error())
		}
		images = fileImages
	}

	if len(images) == 0 {
		return ErrorResult("No images available. Either send an image in the chat or provide a file path with the 'path' parameter.")
	}

	chain := ResolveMediaProviderChain(ctx, "read_image", "", "",
		visionProviderPriority, visionModelDefaults, t.registry)

	// Inject prompt and images into each chain entry's params
	for i := range chain {
		if chain[i].Params == nil {
			chain[i].Params = make(map[string]any)
		}
		chain[i].Params["prompt"] = prompt
		chain[i].Params["images"] = images
	}

	if len(chain) == 0 {
		return ErrorResult("No vision provider configured. Ask the user to add a vision-capable provider (e.g. Gemini, Anthropic, OpenRouter) in the system settings.")
	}

	chainResult, err := ExecuteWithChain(ctx, chain, t.registry, t.callProvider)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Image analysis failed — all vision providers returned errors: %v. The user may need to check their provider API keys or configuration.", err))
	}

	result := NewResult(string(chainResult.Data))
	result.Usage = chainResult.Usage
	result.Provider = chainResult.Provider
	result.Model = chainResult.Model
	return result
}

// callProvider dispatches the vision call using provider.Chat().
func (t *ReadImageTool) callProvider(ctx context.Context, cp credentialProvider, providerName, model string, params map[string]any) ([]byte, *providers.Usage, error) {
	prompt := GetParamString(params, "prompt", "Describe this image in detail.")
	images, _ := params["images"].([]providers.ImageContent)

	if providerName == "minimax" && cp != nil {
		return callMiniMaxCodingPlanVLM(ctx, cp, prompt, images, model)
	}

	// Get the full provider for Chat() access
	p, err := t.registry.Get(ctx, providerName)
	if err != nil {
		return nil, nil, fmt.Errorf("provider %q not available: %w", providerName, err)
	}

	slog.Info("read_image: calling vision provider", "provider", providerName, "model", model, "images", len(images))

	opts := map[string]any{
		"max_tokens":  1024,
		"temperature": 0.3,
	}
	// claude-cli spawns the Claude CLI binary; loading its built-in MCP
	// toolset costs latency we don't need for a one-shot vision call. Keep
	// this flag scoped to claude-cli so other providers don't receive
	// options they ignore (or worse, choke on in the future).
	if providerName == "claude-cli" {
		opts["disable_tools"] = true
	}

	resp, err := p.Chat(ctx, providers.ChatRequest{
		Messages: []providers.Message{
			{
				Role:    "user",
				Content: prompt,
				Images:  images,
			},
		},
		Model:   model,
		Options: opts,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("vision provider error: %w", err)
	}

	return []byte(resp.Content), resp.Usage, nil
}

// loadImageFromPath reads an image file from the workspace and returns it as ImageContent.
func (t *ReadImageTool) loadImageFromPath(ctx context.Context, path string) ([]providers.ImageContent, error) {
	// Infer MIME type from extension
	ext := strings.ToLower(filepath.Ext(path))
	mimeTypes := map[string]string{
		".jpg": "image/jpeg", ".jpeg": "image/jpeg",
		".png": "image/png", ".gif": "image/gif",
		".webp": "image/webp", ".bmp": "image/bmp",
	}
	mime, ok := mimeTypes[ext]
	if !ok {
		return nil, fmt.Errorf("unsupported image format: %s (supported: jpg, png, gif, webp, bmp)", ext)
	}

	// Resolve path within workspace (respect workspace restriction).
	workspace := ToolWorkspaceFromCtx(ctx)
	resolved, err := resolvePathWithAllowed(path, workspace, effectiveRestrict(ctx, true), allowedWithTeamWorkspace(ctx, nil))
	if err != nil {
		return nil, fmt.Errorf("invalid image path: %w", err)
	}
	if err := checkDeniedPath(resolved, workspace, nil); err != nil {
		return nil, err
	}

	// Pre-check file size before loading into memory.
	fi, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("failed to stat image file: %w", err)
	}
	if fi.Size() > maxImageFileBytes {
		return nil, fmt.Errorf("image file too large (%d bytes, max %d)", fi.Size(), maxImageFileBytes)
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("failed to read image file: %w", err)
	}

	return []providers.ImageContent{{
		MimeType: mime,
		Data:     base64.StdEncoding.EncodeToString(data),
	}}, nil
}

func callMiniMaxCodingPlanVLM(ctx context.Context, cp credentialProvider, prompt string, images []providers.ImageContent, model string) ([]byte, *providers.Usage, error) {
	if len(images) == 0 {
		return nil, nil, fmt.Errorf("minimax vision requires at least one image")
	}
	apiKey := cp.APIKey()
	if apiKey == "" {
		return nil, nil, fmt.Errorf("minimax vision requires API key")
	}
	base := strings.TrimRight(cp.APIBase(), "/")
	base = strings.TrimSuffix(base, "/v1")
	if base == "" {
		base = "https://api.minimax.io"
	}
	image := images[0]
	imageData := image.Data
	if !strings.HasPrefix(imageData, "data:") {
		mime := image.MimeType
		if mime == "" {
			mime = "image/jpeg"
		}
		imageData = fmt.Sprintf("data:%s;base64,%s", mime, image.Data)
	}
	payload := map[string]any{
		"model": model,
		"messages": []map[string]any{{
			"role": "user",
			"content": []map[string]any{
				{"type": "text", "text": prompt},
				{"type": "image_url", "image_url": map[string]string{"url": imageData}},
			},
		}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, fmt.Errorf("minimax vlm marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/coding_plan/vlm", bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("minimax vlm request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("MM-API-Source", "GoClaw")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("minimax vlm request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("minimax vlm read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("minimax vlm returned %d: %s", resp.StatusCode, string(respBody))
	}
	var result struct {
		Content  string `json:"content"`
		BaseResp struct {
			StatusCode int    `json:"status_code"`
			StatusMsg  string `json:"status_msg"`
		} `json:"base_resp"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, nil, fmt.Errorf("minimax vlm parse response: %w", err)
	}
	if result.BaseResp.StatusCode != 0 {
		return nil, nil, fmt.Errorf("minimax vlm api error %d: %s", result.BaseResp.StatusCode, result.BaseResp.StatusMsg)
	}
	if result.Content == "" {
		return nil, nil, fmt.Errorf("minimax vlm returned empty content")
	}
	return []byte(result.Content), nil, nil
}
