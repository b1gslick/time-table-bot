package config

import (
	"fmt"
	"os"
)

type Config struct {
	TelegramBotToken   string
	DatabaseURL        string
	SuperAdminUsername string
	Timezone           string
}

func Load() (Config, error) {
	cfg := Config{
		TelegramBotToken:   os.Getenv("TELEGRAM_BOT_TOKEN"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		SuperAdminUsername: envOrDefault("SUPER_ADMIN_USERNAME", "tim1106"),
		Timezone:           envOrDefault("TIMEZONE", "Europe/Nicosia"),
	}

	if cfg.TelegramBotToken == "" {
		return Config{}, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
