package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	TelegramBotToken   string
	DatabaseURL        string
	SuperAdminUsername string
	Timezone           string
	QwenAPIKey         string
	QwenBaseURL        string
	QwenModel          string
	WhisperCLIPath     string
	WhisperFFmpegPath  string
	WhisperModelPath   string
	WhisperThreads     int
}

func Load() (Config, error) {
	cfg := Config{
		TelegramBotToken:   os.Getenv("TELEGRAM_BOT_TOKEN"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		SuperAdminUsername: envOrDefault("SUPER_ADMIN_USERNAME", "tim1106"),
		Timezone:           envOrDefault("TIMEZONE", "Europe/Nicosia"),
		QwenAPIKey:         envFirst("QWEN_API_KEY", "DASHSCOPE_API_KEY"),
		QwenBaseURL:        envFirst("QWEN_BASE_URL", "DASHSCOPE_BASE_URL"),
		QwenModel:          envOrDefault("QWEN_MODEL", "qwen3.7-plus"),
		WhisperCLIPath:     os.Getenv("WHISPER_CLI_PATH"),
		WhisperFFmpegPath:  os.Getenv("WHISPER_FFMPEG_PATH"),
		WhisperModelPath:   os.Getenv("WHISPER_MODEL_PATH"),
		WhisperThreads:     envIntOrDefault("WHISPER_THREADS", 2),
	}

	if cfg.TelegramBotToken == "" {
		return Config{}, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	return cfg, nil
}

func envIntOrDefault(key string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(key))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envFirst(keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}
