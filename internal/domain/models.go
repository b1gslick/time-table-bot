package domain

import "time"

type Role string

const (
	RoleSuperAdmin Role = "super_admin"
	RoleAdmin      Role = "admin"
	RoleUser       Role = "user"
)

type SlotStatus string

const (
	SlotStatusOpen   SlotStatus = "open"
	SlotStatusClosed SlotStatus = "closed"
)

type BookingStatus string

const (
	BookingStatusBooked    BookingStatus = "booked"
	BookingStatusCancelled BookingStatus = "cancelled"
	BookingStatusBlocked   BookingStatus = "blocked"
)

type User struct {
	ID         int64
	TelegramID *int64
	Username   string
	FullName   string
	Role       Role
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type AdminProfile struct {
	UserID        int64
	DisplayName   string
	Description   string
	Timezone      string
	BookingNotice int
	UpdatedAt     time.Time
}

type AdminService struct {
	ID          int64
	AdminUserID int64
	Category    string
	Subcategory string
	Name        string
	Description string
	DurationMin int
	PriceCents  int64
	IsActive    bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type AdminSetting struct {
	AdminUserID int64
	Key         string
	Value       string
	UpdatedAt   time.Time
}

type ScheduleSlot struct {
	ID           int64
	AdminUserID  int64
	ServiceID    *int64
	StartAt      time.Time
	EndAt        time.Time
	Capacity     int
	Status       SlotStatus
	Note         string
	BookedCount  int
	AvailableQty int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Booking struct {
	ID            int64
	SlotID        int64
	UserID        *int64
	ServiceID     *int64
	Status        BookingStatus
	TravelMinutes int
	Note          string
	BookedAt      time.Time
	UpdatedAt     time.Time
	CancelledAt   *time.Time
}

type Reminder struct {
	ID            int64
	BookingID     *int64
	DedupeKey     string
	ChatID        int64
	Kind          string
	RecipientRole string
	SendAt        time.Time
	SentAt        *time.Time
	Channel       string
	Payload       string
	CreatedAt     time.Time
}
