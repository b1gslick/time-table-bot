package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"

	"time-table-bot/internal/domain"
)

//go:embed schema.sql
var schemaSQL string

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) ApplySchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}

func (s *PostgresStore) BootstrapSuperAdmin(ctx context.Context, username string) error {
	username = normalizeUsername(username)
	if username == "" {
		username = DefaultSuperAdminUsername
	}
	const q = `
INSERT INTO users (telegram_id, username, full_name, role)
VALUES (NULL, $1, '', $2)
ON CONFLICT(username) DO UPDATE SET
	role = EXCLUDED.role,
	updated_at = NOW();
`
	if _, err := s.db.ExecContext(ctx, q, username, domain.RoleSuperAdmin); err != nil {
		return fmt.Errorf("bootstrap super admin: %w", err)
	}
	return nil
}

func (s *PostgresStore) UpsertUser(ctx context.Context, telegramID int64, username, fullName string) (domain.User, error) {
	if telegramID <= 0 {
		return domain.User{}, ErrInvalidArgument
	}
	norm := normalizeUsername(username)
	if norm == "" {
		return domain.User{}, ErrInvalidArgument
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.User{}, fmt.Errorf("begin upsert user tx: %w", err)
	}
	defer rollback(tx)

	var (
		id     int64
		exists bool
		role   domain.Role
		tgID   sql.NullInt64
	)

	row := tx.QueryRowContext(ctx, `
SELECT id, role, telegram_id
FROM users
WHERE telegram_id = $1 OR username = $2
LIMIT 1
`, telegramID, norm)
	switch err := row.Scan(&id, &role, &tgID); {
	case err == nil:
		exists = true
	case errors.Is(err, sql.ErrNoRows):
		exists = false
	default:
		return domain.User{}, fmt.Errorf("find user for upsert: %w", err)
	}

	if exists {
		const uq = `
UPDATE users
SET telegram_id = COALESCE(telegram_id, $1),
	username = $2,
	full_name = $3,
	updated_at = NOW()
WHERE id = $4;
`
		if _, err := tx.ExecContext(ctx, uq, telegramID, norm, strings.TrimSpace(fullName), id); err != nil {
			return domain.User{}, fmt.Errorf("update user: %w", err)
		}
	} else {
		role = domain.RoleUser
		if norm == normalizeUsername(DefaultSuperAdminUsername) {
			role = domain.RoleSuperAdmin
		}
		const iq = `
INSERT INTO users (telegram_id, username, full_name, role)
VALUES ($1, $2, $3, $4)
RETURNING id;
`
		if err := tx.QueryRowContext(ctx, iq, telegramID, norm, strings.TrimSpace(fullName), role).Scan(&id); err != nil {
			return domain.User{}, fmt.Errorf("insert user: %w", err)
		}
	}

	user, err := s.getUserByIDTx(ctx, tx, id)
	if err != nil {
		return domain.User{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.User{}, fmt.Errorf("commit upsert user tx: %w", err)
	}
	return user, nil
}

func (s *PostgresStore) GetUserRole(ctx context.Context, telegramID int64) (domain.Role, error) {
	if telegramID <= 0 {
		return "", ErrInvalidArgument
	}
	var role domain.Role
	err := s.db.QueryRowContext(ctx, "SELECT role FROM users WHERE telegram_id = $1", telegramID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get user role: %w", err)
	}
	return role, nil
}

func (s *PostgresStore) UpsertAdminProfile(ctx context.Context, profile domain.AdminProfile) error {
	if profile.UserID <= 0 {
		return ErrInvalidArgument
	}
	const q = `
INSERT INTO admin_profiles (user_id, display_name, description, timezone, booking_notice)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT(user_id) DO UPDATE SET
	display_name = EXCLUDED.display_name,
	description = EXCLUDED.description,
	timezone = EXCLUDED.timezone,
	booking_notice = EXCLUDED.booking_notice,
	updated_at = NOW();
`
	if _, err := s.db.ExecContext(ctx, q, profile.UserID, profile.DisplayName, profile.Description, profile.Timezone, profile.BookingNotice); err != nil {
		return fmt.Errorf("upsert admin profile: %w", err)
	}
	return nil
}

func (s *PostgresStore) UpsertAdminService(ctx context.Context, service domain.AdminService) (domain.AdminService, error) {
	if service.AdminUserID <= 0 || strings.TrimSpace(service.Name) == "" || service.DurationMin <= 0 {
		return domain.AdminService{}, ErrInvalidArgument
	}
	const q = `
INSERT INTO admin_services (admin_user_id, category, subcategory, name, description, price_text, duration_min, price_cents, is_active)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT(admin_user_id, category, subcategory, name, duration_min) DO UPDATE SET
	description = EXCLUDED.description,
	price_text = EXCLUDED.price_text,
	price_cents = EXCLUDED.price_cents,
	is_active = EXCLUDED.is_active,
	updated_at = NOW()
RETURNING id, admin_user_id, category, subcategory, name, description, price_text, duration_min, price_cents, is_active, created_at, updated_at;
`
	var out domain.AdminService
	if err := s.db.QueryRowContext(
		ctx,
		q,
		service.AdminUserID,
		strings.TrimSpace(service.Category),
		strings.TrimSpace(service.Subcategory),
		strings.TrimSpace(service.Name),
		service.Description,
		service.PriceText,
		service.DurationMin,
		service.PriceCents,
		service.IsActive,
	).Scan(
		&out.ID,
		&out.AdminUserID,
		&out.Category,
		&out.Subcategory,
		&out.Name,
		&out.Description,
		&out.PriceText,
		&out.DurationMin,
		&out.PriceCents,
		&out.IsActive,
		&out.CreatedAt,
		&out.UpdatedAt,
	); err != nil {
		return domain.AdminService{}, fmt.Errorf("upsert admin service: %w", err)
	}
	return out, nil
}

func (s *PostgresStore) ListAdminServices(ctx context.Context, adminUserID int64, onlyActive bool) ([]domain.AdminService, error) {
	if adminUserID <= 0 {
		return nil, ErrInvalidArgument
	}
	q := `
SELECT id, admin_user_id, category, subcategory, name, description, price_text, duration_min, price_cents, is_active, created_at, updated_at
FROM admin_services
WHERE admin_user_id = $1
`
	args := []any{adminUserID}
	if onlyActive {
		q += " AND is_active = TRUE"
	}
	q += " ORDER BY created_at ASC"

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list admin services: %w", err)
	}
	defer rows.Close()

	services := make([]domain.AdminService, 0)
	for rows.Next() {
		var svc domain.AdminService
		if err := rows.Scan(
			&svc.ID, &svc.AdminUserID, &svc.Category, &svc.Subcategory, &svc.Name, &svc.Description, &svc.PriceText,
			&svc.DurationMin, &svc.PriceCents, &svc.IsActive, &svc.CreatedAt, &svc.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan admin service: %w", err)
		}
		services = append(services, svc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate admin services: %w", err)
	}
	return services, nil
}

func (s *PostgresStore) SetAdminSetting(ctx context.Context, adminUserID int64, key, value string) error {
	if adminUserID <= 0 || strings.TrimSpace(key) == "" {
		return ErrInvalidArgument
	}
	const q = `
INSERT INTO admin_settings (admin_user_id, key, value)
VALUES ($1, $2, $3)
ON CONFLICT(admin_user_id, key) DO UPDATE SET
	value = EXCLUDED.value,
	updated_at = NOW();
`
	if _, err := s.db.ExecContext(ctx, q, adminUserID, strings.TrimSpace(key), value); err != nil {
		return fmt.Errorf("set admin setting: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetAdminSettings(ctx context.Context, adminUserID int64) ([]domain.AdminSetting, error) {
	if adminUserID <= 0 {
		return nil, ErrInvalidArgument
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT admin_user_id, key, value, updated_at
FROM admin_settings
WHERE admin_user_id = $1
ORDER BY key ASC
`, adminUserID)
	if err != nil {
		return nil, fmt.Errorf("get admin settings: %w", err)
	}
	defer rows.Close()

	settings := make([]domain.AdminSetting, 0)
	for rows.Next() {
		var st domain.AdminSetting
		if err := rows.Scan(&st.AdminUserID, &st.Key, &st.Value, &st.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan admin setting: %w", err)
		}
		settings = append(settings, st)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate admin settings: %w", err)
	}
	return settings, nil
}

func (s *PostgresStore) CreateScheduleSlot(ctx context.Context, slot domain.ScheduleSlot) (domain.ScheduleSlot, error) {
	if slot.AdminUserID <= 0 || slot.Capacity <= 0 || !slot.EndAt.After(slot.StartAt) {
		return domain.ScheduleSlot{}, ErrInvalidArgument
	}
	if slot.Status == "" {
		slot.Status = domain.SlotStatusOpen
	}
	const q = `
INSERT INTO schedule_slots (admin_user_id, service_id, start_at, end_at, capacity, status, note)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, admin_user_id, service_id, start_at, end_at, capacity, status, note, created_at, updated_at;
`
	var out domain.ScheduleSlot
	var serviceID sql.NullInt64
	if err := s.db.QueryRowContext(
		ctx, q, slot.AdminUserID, slot.ServiceID, slot.StartAt, slot.EndAt, slot.Capacity, slot.Status, slot.Note,
	).Scan(
		&out.ID, &out.AdminUserID, &serviceID, &out.StartAt, &out.EndAt, &out.Capacity,
		&out.Status, &out.Note, &out.CreatedAt, &out.UpdatedAt,
	); err != nil {
		return domain.ScheduleSlot{}, fmt.Errorf("create schedule slot: %w", err)
	}
	out.ServiceID = nullInt64Ptr(serviceID)
	out.AvailableQty = out.Capacity
	return out, nil
}

func (s *PostgresStore) UpdateScheduleSlot(ctx context.Context, slot domain.ScheduleSlot) error {
	if slot.ID <= 0 || slot.Capacity <= 0 || !slot.EndAt.After(slot.StartAt) {
		return ErrInvalidArgument
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE schedule_slots
SET service_id = $1,
	start_at = $2,
	end_at = $3,
	capacity = $4,
	status = $5,
	note = $6,
	updated_at = NOW()
WHERE id = $7;
`, slot.ServiceID, slot.StartAt, slot.EndAt, slot.Capacity, slot.Status, slot.Note, slot.ID)
	if err != nil {
		return fmt.Errorf("update schedule slot: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update schedule slot affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) ListAvailableSlots(ctx context.Context, adminUserID int64, from, to time.Time) ([]domain.ScheduleSlot, error) {
	if adminUserID <= 0 || !to.After(from) {
		return nil, ErrInvalidArgument
	}
	const q = `
SELECT
	s.id, s.admin_user_id, s.service_id, s.start_at, s.end_at, s.capacity, s.status, s.note, s.created_at, s.updated_at,
	COALESCE(SUM(CASE WHEN b.status IN ('booked', 'blocked') THEN 1 ELSE 0 END), 0) AS booked_count
FROM schedule_slots s
LEFT JOIN bookings b ON b.slot_id = s.id
WHERE s.admin_user_id = $1
  AND s.status = 'open'
  AND s.start_at >= $2
  AND s.end_at <= $3
GROUP BY s.id
HAVING COALESCE(SUM(CASE WHEN b.status IN ('booked', 'blocked') THEN 1 ELSE 0 END), 0) < s.capacity
ORDER BY s.start_at ASC;
`
	rows, err := s.db.QueryContext(ctx, q, adminUserID, from, to)
	if err != nil {
		return nil, fmt.Errorf("list available slots: %w", err)
	}
	defer rows.Close()

	slots := make([]domain.ScheduleSlot, 0)
	for rows.Next() {
		var (
			slot      domain.ScheduleSlot
			serviceID sql.NullInt64
		)
		if err := rows.Scan(
			&slot.ID, &slot.AdminUserID, &serviceID, &slot.StartAt, &slot.EndAt, &slot.Capacity,
			&slot.Status, &slot.Note, &slot.CreatedAt, &slot.UpdatedAt, &slot.BookedCount,
		); err != nil {
			return nil, fmt.Errorf("scan available slot: %w", err)
		}
		slot.ServiceID = nullInt64Ptr(serviceID)
		slot.AvailableQty = slot.Capacity - slot.BookedCount
		slots = append(slots, slot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate available slots: %w", err)
	}
	return slots, nil
}

func (s *PostgresStore) CreateBooking(ctx context.Context, booking domain.Booking) (domain.Booking, error) {
	if booking.SlotID <= 0 {
		return domain.Booking{}, ErrInvalidArgument
	}
	if booking.Status == "" {
		booking.Status = domain.BookingStatusBooked
	}
	if booking.Status != domain.BookingStatusBooked && booking.Status != domain.BookingStatusBlocked {
		return domain.Booking{}, ErrInvalidArgument
	}
	if booking.Status == domain.BookingStatusBooked && booking.UserID == nil {
		return domain.Booking{}, ErrInvalidArgument
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Booking{}, fmt.Errorf("begin create booking tx: %w", err)
	}
	defer rollback(tx)

	available, err := slotAvailableTx(ctx, tx, booking.SlotID)
	if err != nil {
		return domain.Booking{}, err
	}
	if !available {
		return domain.Booking{}, ErrSlotUnavailable
	}

	const q = `
INSERT INTO bookings (slot_id, user_id, service_id, status, travel_minutes, note)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, slot_id, user_id, service_id, status, travel_minutes, note, booked_at, updated_at, cancelled_at;
`
	var out domain.Booking
	var (
		userID      sql.NullInt64
		serviceID   sql.NullInt64
		cancelledAt sql.NullTime
	)
	travelMinutes := booking.TravelMinutes
	if travelMinutes < 0 {
		travelMinutes = 0
	}
	if err := tx.QueryRowContext(ctx, q, booking.SlotID, booking.UserID, booking.ServiceID, booking.Status, travelMinutes, booking.Note).Scan(
		&out.ID, &out.SlotID, &userID, &serviceID, &out.Status, &out.TravelMinutes, &out.Note, &out.BookedAt, &out.UpdatedAt, &cancelledAt,
	); err != nil {
		return domain.Booking{}, fmt.Errorf("insert booking: %w", err)
	}
	out.UserID = nullInt64Ptr(userID)
	out.ServiceID = nullInt64Ptr(serviceID)
	out.CancelledAt = nullTimePtr(cancelledAt)

	if err := tx.Commit(); err != nil {
		return domain.Booking{}, fmt.Errorf("commit create booking tx: %w", err)
	}
	return out, nil
}

func (s *PostgresStore) RescheduleBooking(ctx context.Context, bookingID, newSlotID int64) error {
	if bookingID <= 0 || newSlotID <= 0 {
		return ErrInvalidArgument
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin reschedule tx: %w", err)
	}
	defer rollback(tx)

	available, err := slotAvailableTx(ctx, tx, newSlotID)
	if err != nil {
		return err
	}
	if !available {
		return ErrSlotUnavailable
	}

	res, err := tx.ExecContext(ctx, `
UPDATE bookings
SET slot_id = $1,
	status = 'booked',
	cancelled_at = NULL,
	updated_at = NOW()
WHERE id = $2 AND status != 'blocked';
`, newSlotID, bookingID)
	if err != nil {
		return fmt.Errorf("reschedule booking: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("reschedule booking affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit reschedule tx: %w", err)
	}
	return nil
}

func (s *PostgresStore) DeleteBooking(ctx context.Context, bookingID int64, reason string) error {
	if bookingID <= 0 {
		return ErrInvalidArgument
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE bookings
SET status = 'cancelled',
	note = CASE WHEN note = '' THEN $1 ELSE note || ' | ' || $2 END,
	cancelled_at = NOW(),
	updated_at = NOW()
WHERE id = $3 AND status != 'cancelled';
`, strings.TrimSpace(reason), strings.TrimSpace(reason), bookingID)
	if err != nil {
		return fmt.Errorf("delete booking: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete booking affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	if _, err := s.db.ExecContext(ctx, `
UPDATE reminders
SET sent_at = NOW()
WHERE booking_id = $1 AND sent_at IS NULL;
`, bookingID); err != nil {
		return fmt.Errorf("cancel booking reminders: %w", err)
	}
	return nil
}

func (s *PostgresStore) BlockBooking(ctx context.Context, slotID, adminUserID int64, reason string) (domain.Booking, error) {
	if slotID <= 0 || adminUserID <= 0 {
		return domain.Booking{}, ErrInvalidArgument
	}
	b := domain.Booking{
		SlotID:    slotID,
		Status:    domain.BookingStatusBlocked,
		Note:      strings.TrimSpace(reason),
		UserID:    nil,
		ServiceID: nil,
	}
	out, err := s.CreateBooking(ctx, b)
	if err != nil {
		return domain.Booking{}, err
	}
	out.Note = fmt.Sprintf("blocked_by_admin:%d %s", adminUserID, strings.TrimSpace(reason))
	if _, err := s.db.ExecContext(ctx, "UPDATE bookings SET note = $1 WHERE id = $2", out.Note, out.ID); err != nil {
		return domain.Booking{}, fmt.Errorf("tag blocked booking: %w", err)
	}
	return out, nil
}

func (s *PostgresStore) ListRemindersToSend(ctx context.Context, before time.Time, limit int) ([]domain.Reminder, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, booking_id, dedupe_key, chat_id, kind, recipient_role, send_at, sent_at, channel, payload, created_at
FROM reminders
WHERE sent_at IS NULL
  AND send_at <= $1
  AND (
      booking_id IS NULL
      OR EXISTS (
          SELECT 1
          FROM bookings b
          WHERE b.id = reminders.booking_id
            AND b.status = 'booked'
      )
  )
ORDER BY send_at ASC
LIMIT $2;
`, before, limit)
	if err != nil {
		return nil, fmt.Errorf("list reminders to send: %w", err)
	}
	defer rows.Close()

	items := make([]domain.Reminder, 0)
	for rows.Next() {
		var (
			r         domain.Reminder
			bookingID sql.NullInt64
			sentAt    sql.NullTime
		)
		if err := rows.Scan(&r.ID, &bookingID, &r.DedupeKey, &r.ChatID, &r.Kind, &r.RecipientRole, &r.SendAt, &sentAt, &r.Channel, &r.Payload, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan reminder: %w", err)
		}
		if bookingID.Valid {
			r.BookingID = &bookingID.Int64
		}
		r.SentAt = nullTimePtr(sentAt)
		items = append(items, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reminders: %w", err)
	}
	return items, nil
}

func (s *PostgresStore) MarkReminderSent(ctx context.Context, reminderID int64, sentAt time.Time) error {
	if reminderID <= 0 {
		return ErrInvalidArgument
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE reminders
SET sent_at = $1
WHERE id = $2 AND sent_at IS NULL;
`, sentAt, reminderID)
	if err != nil {
		return fmt.Errorf("mark reminder sent: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mark reminder sent affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) getUserByIDTx(ctx context.Context, tx *sql.Tx, id int64) (domain.User, error) {
	var (
		u    domain.User
		tgID sql.NullInt64
	)
	err := tx.QueryRowContext(ctx, `
SELECT id, telegram_id, username, full_name, role, created_at, updated_at
FROM users
WHERE id = $1
`, id).Scan(&u.ID, &tgID, &u.Username, &u.FullName, &u.Role, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, ErrNotFound
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("get user by id: %w", err)
	}
	u.TelegramID = nullInt64Ptr(tgID)
	return u, nil
}

func slotAvailableTx(ctx context.Context, tx *sql.Tx, slotID int64) (bool, error) {
	var status domain.SlotStatus
	var capacity, used int
	err := tx.QueryRowContext(ctx, `
SELECT s.status, s.capacity, COALESCE(SUM(CASE WHEN b.status IN ('booked', 'blocked') THEN 1 ELSE 0 END), 0)
FROM schedule_slots s
LEFT JOIN bookings b ON b.slot_id = s.id
WHERE s.id = $1
GROUP BY s.id, s.status, s.capacity
`, slotID).Scan(&status, &capacity, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, fmt.Errorf("check slot availability: %w", err)
	}
	return status == domain.SlotStatusOpen && used < capacity, nil
}

func normalizeUsername(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "@")
	return strings.ToLower(v)
}

func rollback(tx *sql.Tx) {
	if tx != nil {
		_ = tx.Rollback()
	}
}

func nullInt64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	out := v.Int64
	return &out
}

func nullTimePtr(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	out := v.Time
	return &out
}
