package media

import "testing"

func TestGroqAudioFilenameNormalizesTelegramVoiceExtensions(t *testing.T) {
	cases := map[string]string{
		"/tmp/goclaw_media_123.oga": "goclaw_media_123.ogg",
		"/tmp/goclaw_media_123.bin": "goclaw_media_123.ogg",
		"/tmp/goclaw_media_123":     "goclaw_media_123.ogg",
		"/tmp/audio.opus":           "audio.opus",
		"/tmp/audio.ogg":            "audio.ogg",
	}
	for in, want := range cases {
		if got := groqAudioFilename(in); got != want {
			t.Fatalf("groqAudioFilename(%q)=%q want %q", in, got, want)
		}
	}
}
