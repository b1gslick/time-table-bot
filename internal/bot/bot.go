package bot

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"time-table-bot/internal/nlu"
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

type ScheduleGridSlot struct {
	AdminName string
	StartAt   time.Time
	EndAt     time.Time
	Status    string
	Capacity  int
	Booked    int
	Blocked   int
	Available int
}

type ScheduleMonth struct {
	Month     time.Time
	SlotCount int
}

type ScheduleDay struct {
	Date      time.Time
	SlotCount int
}

type ScheduleWeekday struct {
	Weekday   time.Weekday
	SlotCount int
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
	Date        time.Time
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

type ContactAlias struct {
	Alias       string
	ContactType string
	Contact     string
}

type ScheduleImportDraft struct {
	Client         string   `json:"client"`
	ContactType    string   `json:"contact_type,omitempty"`
	Contact        string   `json:"contact,omitempty"`
	ServiceIndexes []int    `json:"service_indexes,omitempty"`
	ServiceQueries []string `json:"service_queries,omitempty"`
	DurationMin    int      `json:"duration_min,omitempty"`
	StartAt        string   `json:"start_at,omitempty"`
	Confidence     float64  `json:"confidence,omitempty"`
}

type ServiceImportDraft struct {
	Category    string  `json:"category,omitempty"`
	Subcategory string  `json:"subcategory,omitempty"`
	Name        string  `json:"name,omitempty"`
	DurationMin int     `json:"duration_min,omitempty"`
	PriceText   string  `json:"price_text,omitempty"`
	Confidence  float64 `json:"confidence,omitempty"`
}

type FinanceEntryDraft struct {
	BookingID   int64   `json:"booking_id,omitempty"`
	Kind        string  `json:"kind,omitempty"`
	Category    string  `json:"category,omitempty"`
	AmountCents int64   `json:"amount_cents,omitempty"`
	Currency    string  `json:"currency,omitempty"`
	OccurredAt  string  `json:"occurred_at,omitempty"`
	Description string  `json:"description,omitempty"`
	Source      string  `json:"source,omitempty"`
	Confidence  float64 `json:"confidence,omitempty"`
}

type FinanceEntryInput struct {
	BookingID   int64
	Kind        string
	Category    string
	AmountCents int64
	Currency    string
	OccurredAt  time.Time
	Description string
	Source      string
}

type FinanceUnresolved struct {
	BookingID    int64
	StartAt      time.Time
	Client       string
	ServiceNames []string
	Reason       string
}

type FinanceBucket struct {
	StartAt      time.Time
	Label        string
	IncomeCents  int64
	ExpenseCents int64
}

type FinanceReport struct {
	From               time.Time
	To                 time.Time
	Currency           string
	BookingIncomeCents int64
	ManualIncomeCents  int64
	ExpenseCents       int64
	ExpenseCategories  map[string]int64
	Unresolved         []FinanceUnresolved
	Buckets            []FinanceBucket
}

type ConversationState struct {
	Step                  string                `json:"step"`
	Category              string                `json:"category,omitempty"`
	Subcategory           string                `json:"subcategory,omitempty"`
	ServiceName           string                `json:"service_name,omitempty"`
	ServiceDescription    string                `json:"service_description,omitempty"`
	ServiceIndex          int                   `json:"service_index,omitempty"`
	Username              string                `json:"username,omitempty"`
	FromDateTime          string                `json:"from_datetime,omitempty"`
	ContactType           string                `json:"contact_type,omitempty"`
	BookingDraft          string                `json:"booking_draft,omitempty"`
	DateFrom              string                `json:"date_from,omitempty"`
	DateTo                string                `json:"date_to,omitempty"`
	SlotDay               string                `json:"slot_day,omitempty"`
	SlotPeriod            string                `json:"slot_period,omitempty"`
	PendingSlotIndex      int                   `json:"pending_slot_index,omitempty"`
	ServiceIndexes        []int                 `json:"service_indexes,omitempty"`
	VisibleServiceIndexes []int                 `json:"visible_service_indexes,omitempty"`
	VisibleSlotIndexes    []int                 `json:"visible_slot_indexes,omitempty"`
	WeekdayIndex          int                   `json:"weekday_index,omitempty"`
	WeeklyHours           []WeekdayHours        `json:"weekly_hours,omitempty"`
	GenerateMode          string                `json:"generate_mode,omitempty"`
	GenerateWeekdays      []time.Weekday        `json:"generate_weekdays,omitempty"`
	ScheduleImportEntries []ScheduleImportDraft `json:"schedule_import_entries,omitempty"`
	ServiceImportEntries  []ServiceImportDraft  `json:"service_import_entries,omitempty"`
	FinanceEntries        []FinanceEntryDraft   `json:"finance_entries,omitempty"`
	FinanceForcedKind     string                `json:"finance_forced_kind,omitempty"`
	FinanceReportPeriod   string                `json:"finance_report_period,omitempty"`
	FinanceResolveBooking int64                 `json:"finance_resolve_booking,omitempty"`
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
	UpsertContactAlias(ctx context.Context, adminTelegramID int64, alias, contactType, contact string) (int64, error)
	DeleteContactAlias(ctx context.Context, adminTelegramID int64, alias string) error
	ListContactAliases(ctx context.Context, adminTelegramID int64) ([]ContactAlias, error)
	AddFinanceEntry(ctx context.Context, adminTelegramID int64, entry FinanceEntryInput) error
	FinanceReport(ctx context.Context, adminTelegramID int64, from, to time.Time, period string) (FinanceReport, error)

	GetProfileText(ctx context.Context, adminTelegramID int64) (string, error)
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
	ListScheduleMonths(ctx context.Context, adminTelegramID int64) ([]ScheduleMonth, error)
	ListScheduleDays(ctx context.Context, adminTelegramID int64, monthStart time.Time) ([]ScheduleDay, error)
	ListScheduleWeekdays(ctx context.Context, adminTelegramID int64, monthStart time.Time) ([]ScheduleWeekday, error)

	AddBookingByUsername(ctx context.Context, adminTelegramID int64, username string, start time.Time) (BookingChangeResult, error)
	AddBookingByPhone(ctx context.Context, adminTelegramID int64, phone string, start time.Time) (BookingChangeResult, error)
	AddBookingForContactByIndex(ctx context.Context, adminTelegramID int64, contactType, contact string, index int) (BookingChangeResult, error)
	CanImportBooking(ctx context.Context, adminTelegramID int64, serviceIndexes []int, start time.Time) error
	AddImportedBooking(ctx context.Context, adminTelegramID int64, contactType, contact string, serviceIndexes []int, start time.Time) (BookingChangeResult, error)
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
	AdminSchedule(ctx context.Context, telegramID int64, from, to time.Time) ([]ScheduleGridSlot, error)
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
	SendPhoto(ctx context.Context, reqBody telegram.SendPhotoRequest) error
	AnswerCallbackQuery(ctx context.Context, reqBody telegram.AnswerCallbackQueryRequest) error
}

type TelegramFileClient interface {
	GetFile(ctx context.Context, fileID string) (telegram.File, error)
	DownloadFile(ctx context.Context, filePath string, maxBytes int64) ([]byte, error)
}

type Bot struct {
	tg                 TelegramClient
	store              Store
	logger             *log.Logger
	superAdminUsername string
	bookingParser      nlu.BookingIntentParser
	adminBookingParser nlu.AdminBookingIntentParser
	speechRecognizer   nlu.SpeechRecognizer
	imageRecognizer    nlu.ImageTextRecognizer
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

func (b *Bot) SetBookingIntentParser(parser nlu.BookingIntentParser) {
	b.bookingParser = parser
}

func (b *Bot) SetAdminBookingIntentParser(parser nlu.AdminBookingIntentParser) {
	b.adminBookingParser = parser
}

func (b *Bot) SetSpeechRecognizer(recognizer nlu.SpeechRecognizer) {
	b.speechRecognizer = recognizer
}

func (b *Bot) SetImageTextRecognizer(recognizer nlu.ImageTextRecognizer) {
	b.imageRecognizer = recognizer
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
			if upd.Message != nil && (strings.TrimSpace(upd.Message.Text) != "" || upd.Message.Voice != nil || upd.Message.Audio != nil || len(upd.Message.Photo) > 0 || isImageDocument(upd.Message.Document)) {
				if err := b.HandleMessage(ctx, upd.Message); err != nil {
					b.logger.Printf("handleMessage error: %v", err)
				}
			}
		}
	}
}
