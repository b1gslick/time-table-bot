package nlu

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type WhisperConfig struct {
	CLIPath    string
	FFmpegPath string
	ModelPath  string
	Threads    int
	Timeout    time.Duration
}

type WhisperRecognizer struct {
	cliPath    string
	ffmpegPath string
	modelPath  string
	threads    int
	timeout    time.Duration
	worker     chan struct{}
}

func NewWhisperRecognizer(cfg WhisperConfig) (*WhisperRecognizer, error) {
	cliPath, err := executablePath(cfg.CLIPath, "whisper-cli")
	if err != nil {
		return nil, err
	}
	ffmpegPath, err := executablePath(cfg.FFmpegPath, "ffmpeg")
	if err != nil {
		return nil, err
	}
	modelPath := strings.TrimSpace(cfg.ModelPath)
	if modelPath == "" {
		return nil, fmt.Errorf("whisper model path is required")
	}
	info, err := os.Stat(modelPath)
	if err != nil {
		return nil, fmt.Errorf("whisper model is unavailable: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("whisper model is unavailable: path is a directory")
	}
	threads := cfg.Threads
	if threads <= 0 {
		threads = 2
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Minute
	}
	return &WhisperRecognizer{
		cliPath:    cliPath,
		ffmpegPath: ffmpegPath,
		modelPath:  modelPath,
		threads:    threads,
		timeout:    timeout,
		worker:     make(chan struct{}, 1),
	}, nil
}

func (r *WhisperRecognizer) Transcribe(ctx context.Context, req SpeechRequest) (string, error) {
	if r == nil {
		return "", fmt.Errorf("whisper recognizer is nil")
	}
	if len(req.Audio) == 0 {
		return "", fmt.Errorf("audio is required")
	}
	select {
	case r.worker <- struct{}{}:
		defer func() { <-r.worker }()
	case <-ctx.Done():
		return "", ctx.Err()
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	dir, err := os.MkdirTemp("", "time-table-bot-speech-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)

	sourcePath := filepath.Join(dir, "source"+audioExtension(req.MIMEType))
	if err := os.WriteFile(sourcePath, req.Audio, 0o600); err != nil {
		return "", err
	}
	wavPath := filepath.Join(dir, "audio.wav")
	if output, err := exec.CommandContext(ctx, r.ffmpegPath,
		"-nostdin", "-hide_banner", "-loglevel", "error", "-y",
		"-i", sourcePath, "-ar", "16000", "-ac", "1", "-c:a", "pcm_s16le", wavPath,
	).CombinedOutput(); err != nil {
		return "", commandError("ffmpeg", err, output)
	}

	outputPrefix := filepath.Join(dir, "transcript")
	language := normalizeWhisperLanguage(req.Language)
	args := []string{
		"-m", r.modelPath,
		"-f", wavPath,
		"-l", language,
		"-t", strconv.Itoa(r.threads),
		"-otxt", "-of", outputPrefix,
		"-nt", "-np", "-sns", "-ng",
	}
	if output, err := exec.CommandContext(ctx, r.cliPath, args...).CombinedOutput(); err != nil {
		return "", commandError("whisper-cli", err, output)
	}
	transcript, err := os.ReadFile(outputPrefix + ".txt")
	if err != nil {
		return "", fmt.Errorf("read whisper transcript: %w", err)
	}
	text := strings.TrimSpace(string(transcript))
	if text == "" {
		return "", fmt.Errorf("whisper transcript is empty")
	}
	return text, nil
}

func executablePath(configured, fallback string) (string, error) {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		configured = fallback
	}
	path, err := exec.LookPath(configured)
	if err != nil {
		return "", fmt.Errorf("%s is unavailable: %w", fallback, err)
	}
	return path, nil
}

func audioExtension(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0])) {
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/mp4", "audio/m4a", "audio/x-m4a":
		return ".m4a"
	case "audio/wav", "audio/x-wav":
		return ".wav"
	case "audio/flac":
		return ".flac"
	default:
		return ".ogg"
	}
}

func normalizeWhisperLanguage(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) >= 2 {
		switch value[:2] {
		case "ru", "en":
			return value[:2]
		}
	}
	return "auto"
}

func commandError(name string, err error, output []byte) error {
	const maxOutput = 500
	if len(output) > maxOutput {
		output = output[len(output)-maxOutput:]
	}
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		return fmt.Errorf("%s failed: %w", name, err)
	}
	return fmt.Errorf("%s failed: %w: %s", name, err, detail)
}
