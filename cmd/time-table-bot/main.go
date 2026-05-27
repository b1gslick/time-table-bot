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
	if err := repo.BootstrapSuperAdmin(ctx); err != nil {
		logger.Fatalf("bootstrap super admin error: %v", err)
	}

	appStore := newAppStore(db, repo, loc)
	tg := telegram.NewClient(cfg.TelegramBotToken)

	svc := scheduler.New(appStore, telegramSender{client: tg}, loc, logger)
	go svc.Start(ctx)
	go func() {
		if err := bot.New(tg, appStore, logger).Run(ctx); err != nil && ctx.Err() == nil {
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
