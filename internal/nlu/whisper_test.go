package nlu

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWhisperRecognizerTranscribesThroughLocalCommands(t *testing.T) {
	dir := t.TempDir()
	ffmpegPath := writeExecutable(t, dir, "ffmpeg", `#!/bin/sh
for arg in "$@"; do output="$arg"; done
printf 'wav' > "$output"
`)
	whisperPath := writeExecutable(t, dir, "whisper-cli", `#!/bin/sh
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-of" ]; then
    shift
    output="$1"
  fi
  shift
done
printf 'хочу эпиляцию завтра вечером\n' > "$output.txt"
`)
	modelPath := filepath.Join(dir, "model.bin")
	if err := os.WriteFile(modelPath, []byte("model"), 0o600); err != nil {
		t.Fatalf("write model: %v", err)
	}

	recognizer, err := NewWhisperRecognizer(WhisperConfig{
		CLIPath:    whisperPath,
		FFmpegPath: ffmpegPath,
		ModelPath:  modelPath,
		Threads:    1,
	})
	if err != nil {
		t.Fatalf("NewWhisperRecognizer: %v", err)
	}
	got, err := recognizer.Transcribe(context.Background(), SpeechRequest{
		Audio:    []byte("ogg"),
		MIMEType: "audio/ogg; codecs=opus",
		Language: "ru-RU",
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if got != "хочу эпиляцию завтра вечером" {
		t.Fatalf("transcript = %q", got)
	}
}

func TestWhisperHelpers(t *testing.T) {
	if got := audioExtension("audio/mpeg"); got != ".mp3" {
		t.Fatalf("audioExtension = %q", got)
	}
	if got := normalizeWhisperLanguage("en-US"); got != "en" {
		t.Fatalf("normalizeWhisperLanguage = %q", got)
	}
	if got := normalizeWhisperLanguage("unknown"); got != "auto" {
		t.Fatalf("unknown language = %q", got)
	}
}

func writeExecutable(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}
