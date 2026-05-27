package bot

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"time-table-bot/internal/telegram"
)

const defaultSuperAdminUsername = "tim1106"

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

type BookingView struct {
	ID        int64
	AdminName string
	StartAt   time.Time
	EndAt     time.Time
	Status    string
	TravelMin int
}

type ServiceView struct {
	ID          int64
	AdminName   string
	Name        string
	Description string
	DurationMin int
	PriceCents  int64
}

type AvailabilitySlot struct {
	StartAt      time.Time
	EndAt        time.Time
	AdminName    string
	ServiceNames []string
	DurationMin  int
}

type GenerateScheduleRequest struct {
	Month       time.Time
	Months      int
	Weekdays    []time.Weekday
	DayStart    time.Duration
	DayEnd      time.Duration
	DurationMin int
}

type GenerateScheduleResult struct {
	Created int
	Skipped int
}

type ConversationState struct {
	Step           string `json:"step"`
	ServiceIndexes []int  `json:"service_indexes,omitempty"`
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
	AddService(ctx context.Context, adminTelegramID int64, name string, durationMin int, priceText string) error
	ListServices(ctx context.Context, telegramID int64) ([]ServiceView, error)
	MasterIntro(ctx context.Context, telegramID int64) (string, error)
	SetWorkHoursText(ctx context.Context, adminTelegramID int64, text string) error
	SetSessionDuration(ctx context.Context, adminTelegramID int64, durationMin int) error
	GenerateSchedule(ctx context.Context, adminTelegramID int64, req GenerateScheduleRequest) (GenerateScheduleResult, error)

	AddBookingByUsername(ctx context.Context, adminTelegramID int64, username string, start time.Time, travelMin int) error
	DeleteBookingByUsername(ctx context.Context, adminTelegramID int64, username string, start time.Time) error
	RescheduleBookingByUsername(ctx context.Context, adminTelegramID int64, username string, fromStart, toStart time.Time) error
	BlockSlot(ctx context.Context, adminTelegramID int64, start time.Time) error

	ListFreeSlotsForMonth(ctx context.Context, telegramID int64, monthStart time.Time) ([]time.Time, error)
	ListFreeSlotsForServices(ctx context.Context, telegramID int64, serviceIndexes []int, monthStart time.Time) ([]AvailabilitySlot, error)
	ListFreeSlotsForServicesRange(ctx context.Context, telegramID int64, serviceIndexes []int, from, to time.Time) ([]AvailabilitySlot, error)
	ListFreeSlotsForServicesDates(ctx context.Context, telegramID int64, serviceIndexes []int, dates []time.Time) ([]AvailabilitySlot, error)
	RequestMissingMonth(ctx context.Context, telegramID int64, monthStart time.Time) (bool, error)
	ListMyBookings(ctx context.Context, telegramID int64, from time.Time) ([]BookingView, error)
	BookForUser(ctx context.Context, telegramID int64, start time.Time, travelMin int) error
	BookForUserByIndex(ctx context.Context, telegramID int64, index int, travelMin int) (time.Time, error)
	MoveBookingForUser(ctx context.Context, telegramID int64, fromStart, toStart time.Time) (MoveResult, error)
	MoveBookingForUserByIndex(ctx context.Context, telegramID int64, bookingIndex, slotIndex int) (MoveResult, error)

	GetConversationState(ctx context.Context, telegramID int64) (ConversationState, error)
	SetConversationState(ctx context.Context, telegramID int64, state ConversationState) error
	ClearConversationState(ctx context.Context, telegramID int64) error
}

type Bot struct {
	tg                 *telegram.Client
	store              Store
	logger             *log.Logger
	superAdminUsername string
}

func New(tg *telegram.Client, store Store, logger *log.Logger, superAdminUsername ...string) *Bot {
	if logger == nil {
		logger = log.Default()
	}
	username := defaultSuperAdminUsername
	if len(superAdminUsername) > 0 && strings.TrimSpace(superAdminUsername[0]) != "" {
		username = superAdminUsername[0]
	}
	return &Bot{
		tg:                 tg,
		store:              store,
		logger:             logger,
		superAdminUsername: normalizeUsername(username),
	}
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
