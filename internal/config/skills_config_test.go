package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSkillsConfigMaxUploadSizeBytesDefaultsAndClamps(t *testing.T) {
	tests := []struct {
		name string
		mb   int
		want int64
	}{
		{name: "default", mb: 0, want: int64(DefaultSkillMaxUploadSizeMB) << 20},
		{name: "negative uses default", mb: -5, want: int64(DefaultSkillMaxUploadSizeMB) << 20},
		{name: "custom", mb: 50, want: 50 << 20},
		{name: "maximum", mb: 500, want: 500 << 20},
		{name: "above maximum", mb: 999, want: int64(MaxSkillMaxUploadSizeMB) << 20},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := (SkillsConfig{MaxUploadSizeMB: tc.mb}).MaxUploadSizeBytes()
			if got != tc.want {
				t.Fatalf("MaxUploadSizeBytes() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestLoadSkillsMaxUploadSizeFromConfigAndEnv(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json5")
	if err := os.WriteFile(cfgPath, []byte("{\"skills\":{\"max_upload_size_mb\":64}}"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Skills.MaxUploadSizeMB != 64 {
		t.Fatalf("config max_upload_size_mb = %d, want 64", cfg.Skills.MaxUploadSizeMB)
	}

	t.Setenv("GOCLAW_SKILLS_MAX_UPLOAD_SIZE_MB", "128")
	cfg, err = Load(cfgPath)
	if err != nil {
		t.Fatalf("load config with env: %v", err)
	}
	if cfg.Skills.MaxUploadSizeMB != 128 {
		t.Fatalf("env max upload size = %d, want 128", cfg.Skills.MaxUploadSizeMB)
	}
}
