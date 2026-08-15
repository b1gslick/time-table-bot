package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"time-table-bot/internal/bot"
	"time-table-bot/internal/config"
	"time-table-bot/internal/nlu"
	"time-table-bot/internal/scheduler"
	"time-table-bot/internal/store"
	"time-table-bot/internal/telegram"
)

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags|log.Lmicroseconds)

	cfg, err := config.Load()
	if err != nil {
		logger.Fatalf("config error: %v", err)
	}

	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		logger.Fatalf("timezone error: %v", err)
	}
	time.Local = loc

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		logger.Fatalf("open db error: %v", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		logger.Fatalf("ping db error: %v", err)
	}

	repo := store.NewPostgresStore(db)
	if err := repo.ApplySchema(ctx); err != nil {
		logger.Fatalf("apply schema error: %v", err)
	}
	if err := repo.BootstrapSuperAdmin(ctx, cfg.SuperAdminUsername); err != nil {
		logger.Fatalf("bootstrap super admin error: %v", err)
	}

	appStore := newAppStore(db, repo, loc)
	tg := telegram.NewClient(cfg.TelegramBotToken)
	bookingBot := bot.New(tg, appStore, logger, cfg.SuperAdminUsername)
	var qwenParser *nlu.QwenParser
	if cfg.QwenAPIKey != "" {
		qwenParser, err = nlu.NewQwenParser(nlu.QwenConfig{
			APIKey:  cfg.QwenAPIKey,
			BaseURL: cfg.QwenBaseURL,
			Model:   cfg.QwenModel,
		})
		if err != nil {
			logger.Fatalf("qwen nlu error: %v", err)
		}
		bookingBot.SetBookingIntentParser(qwenParser)
		logger.Printf("qwen nlu enabled model=%s", cfg.QwenModel)
	}
	if cfg.WhisperModelPath != "" {
		recognizer, err := nlu.NewWhisperRecognizer(nlu.WhisperConfig{
			CLIPath:    cfg.WhisperCLIPath,
			FFmpegPath: cfg.WhisperFFmpegPath,
			ModelPath:  cfg.WhisperModelPath,
			Threads:    cfg.WhisperThreads,
		})
		if err != nil {
			logger.Fatalf("whisper asr error: %v", err)
		}
		bookingBot.SetSpeechRecognizer(recognizer)
		logger.Printf("whisper asr enabled model=%s threads=%d", cfg.WhisperModelPath, cfg.WhisperThreads)
	}
	if cfg.TesseractCLIPath != "" {
		recognizer, err := nlu.NewTesseractRecognizer(nlu.TesseractConfig{
			CLIPath:   cfg.TesseractCLIPath,
			Languages: cfg.TesseractLanguages,
		})
		if err != nil {
			logger.Fatalf("tesseract OCR error: %v", err)
		}
		var imageRecognizer nlu.ImageTextRecognizer = recognizer
		if qwenParser != nil {
			imageRecognizer = nlu.NewFallbackImageTextRecognizer(recognizer, qwenParser)
		}
		bookingBot.SetImageTextRecognizer(imageRecognizer)
		logger.Printf("tesseract OCR enabled languages=%s", cfg.TesseractLanguages)
	}

	svc := scheduler.New(appStore, telegramSender{client: tg}, loc, logger)
	go svc.Start(ctx)
	go func() {
		if err := bookingBot.Run(ctx); err != nil && ctx.Err() == nil {
			logger.Printf("bot stopped: %v", err)
			cancel()
		}
	}()

	logger.Printf("time-table-bot started")
	<-ctx.Done()
	logger.Printf("time-table-bot stopped")
}

type telegramSender struct {
	client *telegram.Client
}

func (s telegramSender) SendMessage(ctx context.Context, chatID int64, text string) error {
	return s.client.SendMessage(ctx, telegram.SendMessageRequest{
		ChatID: chatID,
		Text:   text,
	})
}
