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
	TelegramID    int64
	Username      string
	FirstName     string
	LastName      string
	Role          Role
	ActualRole    Role
	ViewAdminName string
	ViewRole      Role
	Language      string
	LanguageSet   bool
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

type BookingChangeResult struct {
	AdminChatID   int64
	AdminLanguage string
	Username      string
	StartAt       time.Time
	EndAt         time.Time
	NewStartAt    time.Time
	NewEndAt      time.Time
	ServiceNames  []string
}

type BookingView struct {
	ID           int64
	AdminName    string
	Username     string
	ServiceNames []string
	StartAt      time.Time
	EndAt        time.Time
	Status       string
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
	AdminName  string
	Date       time.Time
	OpenSlots  int
	Booked     int
	Blocked    int
	Closed     int
	TotalSlots int
}

type WeekdayHours struct {
	Weekday time.Weekday `json:"weekday"`
	Working bool         `json:"working"`
	Start   string       `json:"start,omitempty"`
	End     string       `json:"end,omitempty"`
}

type AdminView struct {
	Username       string
	Role           Role
	ActiveServices int
	OpenSlots      int
	BookedSlots    int
}

type SuperAdminView struct {
	Role          Role
	AdminUsername string
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

type DeleteScheduleResult struct {
	Deleted int
}

type BlockDateResult struct {
	Date        time.Time
	ClosedSlots int
}

type ConversationState struct {
	Step                  string         `json:"step"`
	Category              string         `json:"category,omitempty"`
	Subcategory           string         `json:"subcategory,omitempty"`
	ServiceName           string         `json:"service_name,omitempty"`
	ServiceDescription    string         `json:"service_description,omitempty"`
	ServiceIndex          int            `json:"service_index,omitempty"`
	Username              string         `json:"username,omitempty"`
	FromDateTime          string         `json:"from_datetime,omitempty"`
	ContactType           string         `json:"contact_type,omitempty"`
	SlotDay               string         `json:"slot_day,omitempty"`
	SlotPeriod            string         `json:"slot_period,omitempty"`
	ServiceIndexes        []int          `json:"service_indexes,omitempty"`
	VisibleServiceIndexes []int          `json:"visible_service_indexes,omitempty"`
	VisibleSlotIndexes    []int          `json:"visible_slot_indexes,omitempty"`
	WeekdayIndex          int            `json:"weekday_index,omitempty"`
	WeeklyHours           []WeekdayHours `json:"weekly_hours,omitempty"`
}

type Store interface {
	RegisterOrUpdateUser(ctx context.Context, user UserRecord) (UserRecord, error)
	GetUserByTelegramID(ctx context.Context, telegramID int64) (UserRecord, error)
	GetUserByUsername(ctx context.Context, username string) (UserRecord, error)
	ListAdmins(ctx context.Context) ([]AdminView, error)
	GetSuperAdminView(ctx context.Context, telegramID int64) (SuperAdminView, error)
	SetSuperAdminView(ctx context.Context, telegramID int64, view SuperAdminView) error
	SetUserRole(ctx context.Context, username string, role Role) error
	SetUserLanguage(ctx context.Context, telegramID int64, language string) error

	SetProfileText(ctx context.Context, adminTelegramID int64, text string) error
	GetServicesText(ctx context.Context, adminTelegramID int64) (string, error)
	SetServicesText(ctx context.Context, adminTelegramID int64, text string) error
	SetCategoryOrder(ctx context.Context, adminTelegramID int64, categories []string) error
	AddService(ctx context.Context, adminTelegramID int64, name string, durationMin int, priceText string) error
	EditServiceByIndex(ctx context.Context, adminTelegramID int64, index int, name string, durationMin int, priceText string) error
	DeleteServiceByIndex(ctx context.Context, adminTelegramID int64, index int) error
	ListServices(ctx context.Context, telegramID int64) ([]ServiceView, error)
	MasterIntro(ctx context.Context, telegramID int64) (string, error)
	SetWorkHoursText(ctx context.Context, adminTelegramID int64, text string) error
	SetWeeklyHours(ctx context.Context, adminTelegramID int64, hours []WeekdayHours) error
	SetSessionDuration(ctx context.Context, adminTelegramID int64, durationMin int) error
	GenerateSchedule(ctx context.Context, adminTelegramID int64, req GenerateScheduleRequest) (GenerateScheduleResult, error)
	DeleteScheduleMonth(ctx context.Context, adminTelegramID int64, monthStart time.Time) (DeleteScheduleResult, error)
	BlockScheduleDate(ctx context.Context, adminTelegramID int64, date time.Time) (BlockDateResult, error)

	AddBookingByUsername(ctx context.Context, adminTelegramID int64, username string, start time.Time) (BookingChangeResult, error)
	AddBookingByPhone(ctx context.Context, adminTelegramID int64, phone string, start time.Time) (BookingChangeResult, error)
	AddBookingForContactByIndex(ctx context.Context, adminTelegramID int64, contactType, contact string, index int) (BookingChangeResult, error)
	DeleteBookingByUsername(ctx context.Context, adminTelegramID int64, username string, start time.Time) (BookingChangeResult, error)
	DeleteBookingByID(ctx context.Context, adminTelegramID int64, bookingID int64) (BookingChangeResult, error)
	RescheduleBookingByUsername(ctx context.Context, adminTelegramID int64, username string, fromStart, toStart time.Time) (BookingChangeResult, error)
	BlockSlot(ctx context.Context, adminTelegramID int64, start time.Time) error

	ListFreeSlotsForMonth(ctx context.Context, telegramID int64, monthStart time.Time) ([]time.Time, error)
	ListFreeSlotsForServices(ctx context.Context, telegramID int64, serviceIndexes []int, monthStart time.Time) ([]AvailabilitySlot, error)
	ListFreeSlotsForServicesRange(ctx context.Context, telegramID int64, serviceIndexes []int, from, to time.Time) ([]AvailabilitySlot, error)
	ListFreeSlotsForServicesDates(ctx context.Context, telegramID int64, serviceIndexes []int, dates []time.Time) ([]AvailabilitySlot, error)
	ListCachedAvailability(ctx context.Context, telegramID int64) ([]AvailabilitySlot, error)
	RequestMissingMonth(ctx context.Context, telegramID int64, monthStart time.Time) (bool, error)
	AdminCalendar(ctx context.Context, telegramID int64, monthStart time.Time) ([]CalendarDay, error)
	ListAdminBookingsRange(ctx context.Context, telegramID int64, from, to time.Time) ([]BookingView, error)
	ListMyBookings(ctx context.Context, telegramID int64, from time.Time) ([]BookingView, error)
	DeleteMyBookingByID(ctx context.Context, telegramID int64, bookingID int64) (BookingChangeResult, error)
	ListMoveTargetsForBooking(ctx context.Context, telegramID int64, bookingID int64, from, to time.Time) ([]AvailabilitySlot, error)
	MoveMyBookingByIDToIndex(ctx context.Context, telegramID int64, bookingID int64, slotIndex int) (MoveResult, error)
	BookForUser(ctx context.Context, telegramID int64, start time.Time) (BookingChangeResult, error)
	BookForUserByIndex(ctx context.Context, telegramID int64, index int) (BookingChangeResult, error)
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
