package bot

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"time-table-bot/internal/telegram"
)

const superAdminUsername = "tim1106"

type Role string

const (
	RoleUser       Role = "user"
	RoleAdmin      Role = "admin"
	RoleSuperAdmin Role = "super_admin"
)

type UserRecord struct {
	TelegramID int64
	Username   string
	FirstName  string
	LastName   string
	Role       Role
	TravelMin  int
	Language   string
}

type Booking struct {
	Username      string
	StartTime     time.Time
	DurationMin   int
	TravelTimeMin int
}

type MoveResult struct {
	AdminChatID   int64
	AdminLanguage string
	Username      string
	FromStart     time.Time
	ToStart       time.Time
}

type Store interface {
	RegisterOrUpdateUser(ctx context.Context, user UserRecord) (UserRecord, error)
	GetUserByTelegramID(ctx context.Context, telegramID int64) (UserRecord, error)
	GetUserByUsername(ctx context.Context, username string) (UserRecord, error)
	SetUserRole(ctx context.Context, username string, role Role) error
	SetUserLanguage(ctx context.Context, telegramID int64, language string) error
	SetUserTravelDefault(ctx context.Context, telegramID int64, travelMin int) error

	SetProfileText(ctx context.Context, adminTelegramID int64, text string) error
	SetServicesText(ctx context.Context, adminTelegramID int64, text string) error
	SetWorkHoursText(ctx context.Context, adminTelegramID int64, text string) error
	SetSessionDuration(ctx context.Context, adminTelegramID int64, durationMin int) error

	AddBookingByUsername(ctx context.Context, adminTelegramID int64, username string, start time.Time, travelMin int) error
	DeleteBookingByUsername(ctx context.Context, adminTelegramID int64, username string, start time.Time) error
	RescheduleBookingByUsername(ctx context.Context, adminTelegramID int64, username string, fromStart, toStart time.Time) error
	BlockSlot(ctx context.Context, adminTelegramID int64, start time.Time) error

	ListFreeSlotsForMonth(ctx context.Context, monthStart time.Time) ([]time.Time, error)
	BookForUser(ctx context.Context, telegramID int64, start time.Time, travelMin int) error
	MoveBookingForUser(ctx context.Context, telegramID int64, fromStart, toStart time.Time) (MoveResult, error)
}

type Bot struct {
	tg     *telegram.Client
	store  Store
	logger *log.Logger
}

func New(tg *telegram.Client, store Store, logger *log.Logger) *Bot {
	if logger == nil {
		logger = log.Default()
	}
	return &Bot{tg: tg, store: store, logger: logger}
}

func (b *Bot) Run(ctx context.Context) error {
	var offset int64 = 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		updates, err := b.tg.GetUpdates(ctx, offset, 60)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			b.logger.Printf("getUpdates error: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}

		for _, upd := range updates {
			offset = upd.UpdateID + 1
			if upd.Message == nil || strings.TrimSpace(upd.Message.Text) == "" {
				continue
			}
			if err := b.handleMessage(ctx, upd.Message); err != nil {
				b.logger.Printf("handleMessage error: %v", err)
			}
		}
	}
}
