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
	TelegramID  int64
	Username    string
	FirstName   string
	LastName    string
	Role        Role
	Language    string
	LanguageSet bool
}

type Booking struct {
	Username    string
	StartTime   time.Time
	DurationMin int
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
}

type ServiceView struct {
	ID          int64
	AdminName   string
	Category    string
	Subcategory string
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

type CalendarDay struct {
	Date       time.Time
	OpenSlots  int
	Booked     int
	Blocked    int
	Closed     int
	TotalSlots int
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
	Step                  string `json:"step"`
	Category              string `json:"category,omitempty"`
	Subcategory           string `json:"subcategory,omitempty"`
	ServiceName           string `json:"service_name,omitempty"`
	ServiceIndexes        []int  `json:"service_indexes,omitempty"`
	VisibleServiceIndexes []int  `json:"visible_service_indexes,omitempty"`
}

type Store interface {
	RegisterOrUpdateUser(ctx context.Context, user UserRecord) (UserRecord, error)
	GetUserByTelegramID(ctx context.Context, telegramID int64) (UserRecord, error)
	GetUserByUsername(ctx context.Context, username string) (UserRecord, error)
	SetUserRole(ctx context.Context, username string, role Role) error
	SetUserLanguage(ctx context.Context, telegramID int64, language string) error

	SetProfileText(ctx context.Context, adminTelegramID int64, text string) error
	SetServicesText(ctx context.Context, adminTelegramID int64, text string) error
	AddService(ctx context.Context, adminTelegramID int64, name string, durationMin int, priceText string) error
	ListServices(ctx context.Context, telegramID int64) ([]ServiceView, error)
	MasterIntro(ctx context.Context, telegramID int64) (string, error)
	SetWorkHoursText(ctx context.Context, adminTelegramID int64, text string) error
	SetSessionDuration(ctx context.Context, adminTelegramID int64, durationMin int) error
	GenerateSchedule(ctx context.Context, adminTelegramID int64, req GenerateScheduleRequest) (GenerateScheduleResult, error)

	AddBookingByUsername(ctx context.Context, adminTelegramID int64, username string, start time.Time) error
	DeleteBookingByUsername(ctx context.Context, adminTelegramID int64, username string, start time.Time) error
	RescheduleBookingByUsername(ctx context.Context, adminTelegramID int64, username string, fromStart, toStart time.Time) error
	BlockSlot(ctx context.Context, adminTelegramID int64, start time.Time) error

	ListFreeSlotsForMonth(ctx context.Context, telegramID int64, monthStart time.Time) ([]time.Time, error)
	ListFreeSlotsForServices(ctx context.Context, telegramID int64, serviceIndexes []int, monthStart time.Time) ([]AvailabilitySlot, error)
	ListFreeSlotsForServicesRange(ctx context.Context, telegramID int64, serviceIndexes []int, from, to time.Time) ([]AvailabilitySlot, error)
	ListFreeSlotsForServicesDates(ctx context.Context, telegramID int64, serviceIndexes []int, dates []time.Time) ([]AvailabilitySlot, error)
	RequestMissingMonth(ctx context.Context, telegramID int64, monthStart time.Time) (bool, error)
	AdminCalendar(ctx context.Context, telegramID int64, monthStart time.Time) ([]CalendarDay, error)
	ListMyBookings(ctx context.Context, telegramID int64, from time.Time) ([]BookingView, error)
	BookForUser(ctx context.Context, telegramID int64, start time.Time) error
	BookForUserByIndex(ctx context.Context, telegramID int64, index int) (time.Time, error)
	MoveBookingForUser(ctx context.Context, telegramID int64, fromStart, toStart time.Time) (MoveResult, error)
	MoveBookingForUserByIndex(ctx context.Context, telegramID int64, bookingIndex, slotIndex int) (MoveResult, error)

	GetConversationState(ctx context.Context, telegramID int64) (ConversationState, error)
	SetConversationState(ctx context.Context, telegramID int64, state ConversationState) error
	ClearConversationState(ctx context.Context, telegramID int64) error
}

type TelegramClient interface {
	GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]telegram.Update, error)
	SendMessage(ctx context.Context, reqBody telegram.SendMessageRequest) error
	AnswerCallbackQuery(ctx context.Context, reqBody telegram.AnswerCallbackQueryRequest) error
}

type Bot struct {
	tg                 TelegramClient
	store              Store
	logger             *log.Logger
	superAdminUsername string
}

func New(tg TelegramClient, store Store, logger *log.Logger, superAdminUsername ...string) *Bot {
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
	var lastIdleLog time.Time
	b.logger.Printf("telegram polling started")
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
		if len(updates) > 0 {
			b.logger.Printf("telegram updates received count=%d first_update_id=%d last_update_id=%d", len(updates), updates[0].UpdateID, updates[len(updates)-1].UpdateID)
		} else if time.Since(lastIdleLog) >= 5*time.Minute {
			b.logger.Printf("telegram poll ok updates=0")
			lastIdleLog = time.Now()
		}

		for _, upd := range updates {
			offset = upd.UpdateID + 1
			if upd.CallbackQuery != nil {
				if err := b.HandleCallback(ctx, upd.CallbackQuery); err != nil {
					b.logger.Printf("handleCallback error: %v", err)
				}
				continue
			}
			if upd.Message != nil && strings.TrimSpace(upd.Message.Text) != "" {
				if err := b.HandleMessage(ctx, upd.Message); err != nil {
					b.logger.Printf("handleMessage error: %v", err)
				}
			}
		}
	}
}
