package store

import (
	"context"
	"errors"
	"time"

	"time-table-bot/internal/domain"
)

const DefaultSuperAdminUsername = "tim1106"

var (
	ErrNotFound        = errors.New("store: not found")
	ErrConflict        = errors.New("store: conflict")
	ErrInvalidArgument = errors.New("store: invalid argument")
	ErrSlotUnavailable = errors.New("store: slot unavailable")
)

type Repository interface {
	BootstrapSuperAdmin(ctx context.Context) error
	UpsertUser(ctx context.Context, telegramID int64, username, fullName string) (domain.User, error)
	GetUserRole(ctx context.Context, telegramID int64) (domain.Role, error)

	UpsertAdminProfile(ctx context.Context, profile domain.AdminProfile) error
	UpsertAdminService(ctx context.Context, service domain.AdminService) (domain.AdminService, error)
	ListAdminServices(ctx context.Context, adminUserID int64, onlyActive bool) ([]domain.AdminService, error)
	SetAdminSetting(ctx context.Context, adminUserID int64, key, value string) error
	GetAdminSettings(ctx context.Context, adminUserID int64) ([]domain.AdminSetting, error)

	CreateScheduleSlot(ctx context.Context, slot domain.ScheduleSlot) (domain.ScheduleSlot, error)
	UpdateScheduleSlot(ctx context.Context, slot domain.ScheduleSlot) error
	ListAvailableSlots(ctx context.Context, adminUserID int64, from, to time.Time) ([]domain.ScheduleSlot, error)

	CreateBooking(ctx context.Context, booking domain.Booking) (domain.Booking, error)
	RescheduleBooking(ctx context.Context, bookingID, newSlotID int64) error
	DeleteBooking(ctx context.Context, bookingID int64, reason string) error
	BlockBooking(ctx context.Context, slotID, adminUserID int64, reason string) (domain.Booking, error)

	ListRemindersToSend(ctx context.Context, before time.Time, limit int) ([]domain.Reminder, error)
	MarkReminderSent(ctx context.Context, reminderID int64, sentAt time.Time) error
}
