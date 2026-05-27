package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"time-table-bot/internal/bot"
	"time-table-bot/internal/domain"
	"time-table-bot/internal/scheduler"
	"time-table-bot/internal/store"
)

const (
	defaultSessionDuration = 60
	defaultTravelMinutes   = 30
)

type appStore struct {
	db   *sql.DB
	repo *store.PostgresStore
	loc  *time.Location
}

func newAppStore(db *sql.DB, repo *store.PostgresStore, loc *time.Location) *appStore {
	return &appStore{db: db, repo: repo, loc: loc}
}

func (s *appStore) RegisterOrUpdateUser(ctx context.Context, user bot.UserRecord) (bot.UserRecord, error) {
	username := normalizeUsername(user.Username)
	if username == "" {
		username = fmt.Sprintf("telegram_%d", user.TelegramID)
	}
	fullName := strings.TrimSpace(user.FirstName + " " + user.LastName)
	u, err := s.repo.UpsertUser(ctx, user.TelegramID, username, fullName)
	if err != nil {
		return bot.UserRecord{}, err
	}
	return s.userRecordFromDomain(ctx, u)
}

func (s *appStore) GetUserByTelegramID(ctx context.Context, telegramID int64) (bot.UserRecord, error) {
	u, err := s.lookupUser(ctx, "telegram_id = $1", telegramID)
	if err != nil {
		return bot.UserRecord{}, err
	}
	return s.userRecordFromDomain(ctx, u)
}

func (s *appStore) GetUserByUsername(ctx context.Context, username string) (bot.UserRecord, error) {
	u, err := s.lookupUser(ctx, "username = $1", normalizeUsername(username))
	if err != nil {
		return bot.UserRecord{}, err
	}
	return s.userRecordFromDomain(ctx, u)
}

func (s *appStore) SetUserRole(ctx context.Context, username string, role bot.Role) error {
	norm := normalizeUsername(username)
	if norm == "" || !validBotRole(role) {
		return store.ErrInvalidArgument
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO users (username, full_name, role)
VALUES ($1, '', $2)
ON CONFLICT(username) DO UPDATE SET
	role = EXCLUDED.role,
	updated_at = NOW();
`, norm, domain.Role(role))
	if err != nil {
		return fmt.Errorf("set user role: %w", err)
	}
	return nil
}

func (s *appStore) SetUserLanguage(ctx context.Context, telegramID int64, language string) error {
	if language != bot.LangRU && language != bot.LangEN {
		return store.ErrInvalidArgument
	}
	userID, err := s.userIDByTelegram(ctx, telegramID)
	if err != nil {
		return err
	}
	return s.repo.SetAdminSetting(ctx, userID, "language", language)
}

func (s *appStore) SetUserTravelDefault(ctx context.Context, telegramID int64, travelMin int) error {
	if travelMin < 0 {
		return store.ErrInvalidArgument
	}
	userID, err := s.userIDByTelegram(ctx, telegramID)
	if err != nil {
		return err
	}
	return s.repo.SetAdminSetting(ctx, userID, "travel_default", fmt.Sprintf("%d", travelMin))
}

func (s *appStore) SetProfileText(ctx context.Context, adminTelegramID int64, text string) error {
	adminID, err := s.userIDByTelegram(ctx, adminTelegramID)
	if err != nil {
		return err
	}
	return s.repo.UpsertAdminProfile(ctx, domain.AdminProfile{
		UserID:        adminID,
		DisplayName:   "",
		Description:   strings.TrimSpace(text),
		Timezone:      s.loc.String(),
		BookingNotice: 60,
	})
}

func (s *appStore) SetServicesText(ctx context.Context, adminTelegramID int64, text string) error {
	adminID, err := s.userIDByTelegram(ctx, adminTelegramID)
	if err != nil {
		return err
	}
	return s.repo.SetAdminSetting(ctx, adminID, "services_text", strings.TrimSpace(text))
}

func (s *appStore) SetWorkHoursText(ctx context.Context, adminTelegramID int64, text string) error {
	adminID, err := s.userIDByTelegram(ctx, adminTelegramID)
	if err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if err := s.repo.SetAdminSetting(ctx, adminID, "work_hours_text", text); err != nil {
		return err
	}
	parts := strings.Fields(text)
	if len(parts) >= 2 {
		if days, err := parseWeekdaysSetting(parts[0]); err == nil {
			_ = s.repo.SetAdminSetting(ctx, adminID, "work_days", strings.Join(days, ","))
		}
		if start, end, err := parseDayRangeSetting(parts[1]); err == nil && end > start {
			_ = s.repo.SetAdminSetting(ctx, adminID, "work_start", formatClockDuration(start))
			_ = s.repo.SetAdminSetting(ctx, adminID, "work_end", formatClockDuration(end))
		}
	}
	return nil
}

func (s *appStore) SetSessionDuration(ctx context.Context, adminTelegramID int64, durationMin int) error {
	if durationMin <= 0 {
		return store.ErrInvalidArgument
	}
	adminID, err := s.userIDByTelegram(ctx, adminTelegramID)
	if err != nil {
		return err
	}
	return s.repo.SetAdminSetting(ctx, adminID, "session_duration", fmt.Sprintf("%d", durationMin))
}

func (s *appStore) GenerateSchedule(ctx context.Context, adminTelegramID int64, req bot.GenerateScheduleRequest) (bot.GenerateScheduleResult, error) {
	adminID, err := s.userIDByTelegram(ctx, adminTelegramID)
	if err != nil {
		return bot.GenerateScheduleResult{}, err
	}
	if req.Month.IsZero() {
		return bot.GenerateScheduleResult{}, store.ErrInvalidArgument
	}
	if len(req.Weekdays) == 0 {
		raw, err := s.stringSetting(ctx, adminID, "work_days", "")
		if err != nil || raw == "" {
			return bot.GenerateScheduleResult{}, store.ErrInvalidArgument
		}
		days, err := parseStoredWeekdays(raw)
		if err != nil {
			return bot.GenerateScheduleResult{}, err
		}
		req.Weekdays = days
	}
	if req.DayStart == 0 && req.DayEnd == 0 {
		startRaw, err1 := s.stringSetting(ctx, adminID, "work_start", "")
		endRaw, err2 := s.stringSetting(ctx, adminID, "work_end", "")
		if err1 != nil || err2 != nil || startRaw == "" || endRaw == "" {
			return bot.GenerateScheduleResult{}, store.ErrInvalidArgument
		}
		req.DayStart, err = parseClockSetting(startRaw)
		if err != nil {
			return bot.GenerateScheduleResult{}, err
		}
		req.DayEnd, err = parseClockSetting(endRaw)
		if err != nil {
			return bot.GenerateScheduleResult{}, err
		}
	}
	if req.DurationMin <= 0 {
		req.DurationMin, err = s.intSetting(ctx, adminID, "session_duration", defaultSessionDuration)
		if err != nil || req.DurationMin <= 0 {
			req.DurationMin = defaultSessionDuration
		}
	}
	if req.DayEnd <= req.DayStart || req.DurationMin <= 0 {
		return bot.GenerateScheduleResult{}, store.ErrInvalidArgument
	}

	monthStart := time.Date(req.Month.Year(), req.Month.Month(), 1, 0, 0, 0, 0, s.loc)
	monthEnd := monthStart.AddDate(0, 1, 0)
	daySet := map[time.Weekday]bool{}
	for _, day := range req.Weekdays {
		daySet[day] = true
	}

	var result bot.GenerateScheduleResult
	for day := monthStart; day.Before(monthEnd); day = day.AddDate(0, 0, 1) {
		if !daySet[day.Weekday()] {
			continue
		}
		for offset := req.DayStart; offset+time.Duration(req.DurationMin)*time.Minute <= req.DayEnd; offset += time.Duration(req.DurationMin) * time.Minute {
			start := day.Add(offset)
			if _, err := s.slotIDByAdminStart(ctx, adminID, start); err == nil {
				result.Skipped++
				continue
			} else if !errors.Is(err, store.ErrNotFound) {
				return result, err
			}
			_, err := s.repo.CreateScheduleSlot(ctx, domain.ScheduleSlot{
				AdminUserID: adminID,
				StartAt:     start,
				EndAt:       start.Add(time.Duration(req.DurationMin) * time.Minute),
				Capacity:    1,
				Status:      domain.SlotStatusOpen,
			})
			if err != nil {
				return result, err
			}
			result.Created++
		}
	}
	return result, nil
}

func (s *appStore) AddBookingByUsername(ctx context.Context, adminTelegramID int64, username string, start time.Time, travelMin int) error {
	adminID, err := s.userIDByTelegram(ctx, adminTelegramID)
	if err != nil {
		return err
	}
	clientID, err := s.ensureUserByUsername(ctx, username)
	if err != nil {
		return err
	}
	slotID, err := s.ensureSlot(ctx, adminID, start)
	if err != nil {
		return err
	}
	_, err = s.repo.CreateBooking(ctx, domain.Booking{
		SlotID:        slotID,
		UserID:        &clientID,
		Status:        domain.BookingStatusBooked,
		TravelMinutes: normalizeTravel(travelMin),
		Note:          "created_by_admin",
	})
	return err
}

func (s *appStore) DeleteBookingByUsername(ctx context.Context, adminTelegramID int64, username string, start time.Time) error {
	bookingID, err := s.bookingIDByAdminUserStart(ctx, adminTelegramID, username, start)
	if err != nil {
		return err
	}
	return s.repo.DeleteBooking(ctx, bookingID, "cancelled_by_admin")
}

func (s *appStore) RescheduleBookingByUsername(ctx context.Context, adminTelegramID int64, username string, fromStart, toStart time.Time) error {
	adminID, err := s.userIDByTelegram(ctx, adminTelegramID)
	if err != nil {
		return err
	}
	bookingID, err := s.bookingIDByAdminUserStart(ctx, adminTelegramID, username, fromStart)
	if err != nil {
		return err
	}
	newSlotID, err := s.ensureSlot(ctx, adminID, toStart)
	if err != nil {
		return err
	}
	return s.repo.RescheduleBooking(ctx, bookingID, newSlotID)
}

func (s *appStore) BlockSlot(ctx context.Context, adminTelegramID int64, start time.Time) error {
	adminID, err := s.userIDByTelegram(ctx, adminTelegramID)
	if err != nil {
		return err
	}
	slotID, err := s.ensureSlot(ctx, adminID, start)
	if err != nil {
		return err
	}
	_, err = s.repo.BlockBooking(ctx, slotID, adminID, "blocked_by_admin")
	return err
}

func (s *appStore) ListFreeSlotsForMonth(ctx context.Context, telegramID int64, monthStart time.Time) ([]time.Time, error) {
	from := time.Date(monthStart.Year(), monthStart.Month(), 1, 0, 0, 0, 0, s.loc)
	to := from.AddDate(0, 1, 0)
	rows, err := s.db.QueryContext(ctx, `
SELECT s.start_at
FROM schedule_slots s
LEFT JOIN bookings b ON b.slot_id = s.id AND b.status IN ('booked', 'blocked')
WHERE s.status = 'open'
  AND s.start_at >= $1
  AND s.start_at < $2
GROUP BY s.id
HAVING COUNT(b.id) < s.capacity
ORDER BY s.start_at ASC;
`, from, to)
	if err != nil {
		return nil, fmt.Errorf("list free slots: %w", err)
	}
	defer rows.Close()

	var slots []time.Time
	for rows.Next() {
		var start time.Time
		if err := rows.Scan(&start); err != nil {
			return nil, err
		}
		slots = append(slots, start.In(s.loc))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if telegramID > 0 {
		_ = s.saveTimesForUser(ctx, telegramID, "last_free_slots", slots)
	}
	return slots, nil
}

func (s *appStore) ListMyBookings(ctx context.Context, telegramID int64, from time.Time) ([]bot.BookingView, error) {
	userID, err := s.userIDByTelegram(ctx, telegramID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT b.id, COALESCE(a.username, ''), s.start_at, s.end_at, b.status, b.travel_minutes
FROM bookings b
JOIN schedule_slots s ON s.id = b.slot_id
JOIN users a ON a.id = s.admin_user_id
WHERE b.user_id = $1
  AND b.status = 'booked'
  AND s.start_at >= $2
ORDER BY s.start_at ASC
LIMIT 20;
`, userID, from.In(s.loc))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []bot.BookingView
	var times []time.Time
	for rows.Next() {
		var item bot.BookingView
		if err := rows.Scan(&item.ID, &item.AdminName, &item.StartAt, &item.EndAt, &item.Status, &item.TravelMin); err != nil {
			return nil, err
		}
		item.StartAt = item.StartAt.In(s.loc)
		item.EndAt = item.EndAt.In(s.loc)
		items = append(items, item)
		times = append(times, item.StartAt)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_ = s.saveTimesForUser(ctx, telegramID, "last_my_bookings", times)
	return items, nil
}

func (s *appStore) BookForUser(ctx context.Context, telegramID int64, start time.Time, travelMin int) error {
	userID, err := s.userIDByTelegram(ctx, telegramID)
	if err != nil {
		return err
	}
	slotID, err := s.availableSlotIDByStart(ctx, start)
	if err != nil {
		return err
	}
	_, err = s.repo.CreateBooking(ctx, domain.Booking{
		SlotID:        slotID,
		UserID:        &userID,
		Status:        domain.BookingStatusBooked,
		TravelMinutes: normalizeTravel(travelMin),
		Note:          "created_by_user",
	})
	return err
}

func (s *appStore) BookForUserByIndex(ctx context.Context, telegramID int64, index int, travelMin int) (time.Time, error) {
	slots, err := s.loadTimesForUser(ctx, telegramID, "last_free_slots")
	if err != nil {
		return time.Time{}, err
	}
	if index <= 0 || index > len(slots) {
		return time.Time{}, store.ErrInvalidArgument
	}
	start := slots[index-1]
	return start, s.BookForUser(ctx, telegramID, start, travelMin)
}

func (s *appStore) MoveBookingForUser(ctx context.Context, telegramID int64, fromStart, toStart time.Time) (bot.MoveResult, error) {
	userID, err := s.userIDByTelegram(ctx, telegramID)
	if err != nil {
		return bot.MoveResult{}, err
	}

	var (
		bookingID      int64
		adminID        int64
		adminChatID    sql.NullInt64
		clientUsername string
	)
	err = s.db.QueryRowContext(ctx, `
SELECT b.id, s.admin_user_id, a.telegram_id, u.username
FROM bookings b
JOIN schedule_slots s ON s.id = b.slot_id
JOIN users u ON u.id = b.user_id
JOIN users a ON a.id = s.admin_user_id
WHERE b.user_id = $1
  AND s.start_at = $2
  AND b.status = 'booked'
LIMIT 1;
`, userID, fromStart.In(s.loc)).Scan(&bookingID, &adminID, &adminChatID, &clientUsername)
	if errors.Is(err, sql.ErrNoRows) {
		return bot.MoveResult{}, store.ErrNotFound
	}
	if err != nil {
		return bot.MoveResult{}, err
	}

	newSlotID, err := s.availableSlotIDByAdminStart(ctx, adminID, toStart)
	if err != nil {
		return bot.MoveResult{}, err
	}
	if err := s.repo.RescheduleBooking(ctx, bookingID, newSlotID); err != nil {
		return bot.MoveResult{}, err
	}

	result := bot.MoveResult{
		Username:      clientUsername,
		FromStart:     fromStart.In(s.loc),
		ToStart:       toStart.In(s.loc),
		AdminLanguage: bot.LangRU,
	}
	if adminChatID.Valid {
		result.AdminChatID = adminChatID.Int64
	}
	if lang, err := s.stringSetting(ctx, adminID, "language", bot.LangRU); err == nil {
		result.AdminLanguage = lang
	}
	return result, nil
}

func (s *appStore) MoveBookingForUserByIndex(ctx context.Context, telegramID int64, bookingIndex, slotIndex int) (bot.MoveResult, error) {
	bookings, err := s.loadTimesForUser(ctx, telegramID, "last_my_bookings")
	if err != nil {
		return bot.MoveResult{}, err
	}
	slots, err := s.loadTimesForUser(ctx, telegramID, "last_free_slots")
	if err != nil {
		return bot.MoveResult{}, err
	}
	if bookingIndex <= 0 || bookingIndex > len(bookings) || slotIndex <= 0 || slotIndex > len(slots) {
		return bot.MoveResult{}, store.ErrInvalidArgument
	}
	return s.MoveBookingForUser(ctx, telegramID, bookings[bookingIndex-1], slots[slotIndex-1])
}

func (s *appStore) PrepareUpcomingReminders(ctx context.Context, now time.Time) error {
	rows, err := s.db.QueryContext(ctx, `
SELECT b.id, b.travel_minutes, s.start_at, u.telegram_id, u.username, a.telegram_id,
       COALESCE(ul.value, 'ru') AS user_language,
       COALESCE(al.value, 'ru') AS admin_language
FROM bookings b
JOIN schedule_slots s ON s.id = b.slot_id
LEFT JOIN users u ON u.id = b.user_id
JOIN users a ON a.id = s.admin_user_id
LEFT JOIN admin_settings ul ON ul.admin_user_id = u.id AND ul.key = 'language'
LEFT JOIN admin_settings al ON al.admin_user_id = a.id AND al.key = 'language'
WHERE b.status = 'booked'
  AND s.start_at >= $1
  AND s.start_at < $2;
`, now.Add(-36*time.Hour), now.Add(8*24*time.Hour))
	if err != nil {
		return fmt.Errorf("select upcoming bookings for reminders: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			bookingID     int64
			travelMinutes int
			startAt       time.Time
			userChatID    sql.NullInt64
			username      sql.NullString
			adminChatID   sql.NullInt64
			userLanguage  string
			adminLanguage string
		)
		if err := rows.Scan(&bookingID, &travelMinutes, &startAt, &userChatID, &username, &adminChatID, &userLanguage, &adminLanguage); err != nil {
			return err
		}
		startAt = startAt.In(s.loc)
		userLabel := "@client"
		if username.Valid && username.String != "" {
			userLabel = "@" + username.String
		}

		if userChatID.Valid {
			if err := s.upsertReminder(ctx, bookingID, userChatID.Int64, "day_before", "user", startAt.Add(-24*time.Hour), userReminderDayBefore(userLanguage, startAt)); err != nil {
				return err
			}
			if err := s.upsertReminder(ctx, bookingID, userChatID.Int64, "day_of_user", "user", startAt.Add(-time.Duration(normalizeTravel(travelMinutes)+10)*time.Minute), userReminderTravel(userLanguage, startAt)); err != nil {
				return err
			}
		}
		if adminChatID.Valid {
			if err := s.upsertReminder(ctx, bookingID, adminChatID.Int64, "day_before", "admin", startAt.Add(-24*time.Hour), adminReminderDayBefore(adminLanguage, userLabel, startAt)); err != nil {
				return err
			}
		}
	}
	return rows.Err()
}

func (s *appStore) DueReminders(ctx context.Context, now time.Time, limit int) ([]scheduler.Reminder, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, chat_id, payload
FROM reminders
WHERE sent_at IS NULL
  AND send_at <= $1
ORDER BY send_at ASC
LIMIT $2;
`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("select due reminders: %w", err)
	}
	defer rows.Close()

	items := make([]scheduler.Reminder, 0, limit)
	for rows.Next() {
		var r scheduler.Reminder
		if err := rows.Scan(&r.ID, &r.ChatID, &r.Text); err != nil {
			return nil, err
		}
		items = append(items, r)
	}
	return items, rows.Err()
}

func (s *appStore) MarkReminderSent(ctx context.Context, reminderID int64, sentAt time.Time) error {
	return s.repo.MarkReminderSent(ctx, reminderID, sentAt)
}

func (s *appStore) upsertReminder(ctx context.Context, bookingID, chatID int64, kind, recipientRole string, sendAt time.Time, payload string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO reminders (booking_id, chat_id, kind, recipient_role, send_at, channel, payload)
VALUES ($1, $2, $3, $4, $5, 'telegram', $6)
ON CONFLICT(booking_id, kind, recipient_role, chat_id) DO UPDATE SET
	send_at = EXCLUDED.send_at,
	payload = EXCLUDED.payload;
`, bookingID, chatID, kind, recipientRole, sendAt, payload)
	return err
}

func (s *appStore) lookupUser(ctx context.Context, where string, arg any) (domain.User, error) {
	var (
		u    domain.User
		tgID sql.NullInt64
	)
	err := s.db.QueryRowContext(ctx, `
SELECT id, telegram_id, username, full_name, role, created_at, updated_at
FROM users
WHERE `+where+`
LIMIT 1;
`, arg).Scan(&u.ID, &tgID, &u.Username, &u.FullName, &u.Role, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, store.ErrNotFound
	}
	if err != nil {
		return domain.User{}, err
	}
	if tgID.Valid {
		u.TelegramID = &tgID.Int64
	}
	return u, nil
}

func (s *appStore) userRecordFromDomain(ctx context.Context, u domain.User) (bot.UserRecord, error) {
	firstName, lastName := splitName(u.FullName)
	rec := bot.UserRecord{
		Username:  u.Username,
		FirstName: firstName,
		LastName:  lastName,
		Role:      bot.Role(u.Role),
		TravelMin: defaultTravelMinutes,
	}
	if u.TelegramID != nil {
		rec.TelegramID = *u.TelegramID
	}
	if lang, err := s.stringSetting(ctx, u.ID, "language", bot.LangRU); err == nil {
		rec.Language = lang
	}
	travel, err := s.intSetting(ctx, u.ID, "travel_default", defaultTravelMinutes)
	if err == nil {
		rec.TravelMin = travel
	}
	return rec, nil
}

func (s *appStore) userIDByTelegram(ctx context.Context, telegramID int64) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, "SELECT id FROM users WHERE telegram_id = $1", telegramID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, store.ErrNotFound
	}
	return id, err
}

func (s *appStore) ensureUserByUsername(ctx context.Context, username string) (int64, error) {
	norm := normalizeUsername(username)
	if norm == "" {
		return 0, store.ErrInvalidArgument
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `
INSERT INTO users (username, full_name, role)
VALUES ($1, '', 'user')
ON CONFLICT(username) DO UPDATE SET updated_at = NOW()
RETURNING id;
`, norm).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("ensure user by username: %w", err)
	}
	return id, nil
}

func (s *appStore) ensureSlot(ctx context.Context, adminID int64, start time.Time) (int64, error) {
	if slotID, err := s.slotIDByAdminStart(ctx, adminID, start); err == nil {
		return slotID, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return 0, err
	}

	duration, err := s.intSetting(ctx, adminID, "session_duration", defaultSessionDuration)
	if err != nil {
		duration = defaultSessionDuration
	}
	slot, err := s.repo.CreateScheduleSlot(ctx, domain.ScheduleSlot{
		AdminUserID: adminID,
		StartAt:     start.In(s.loc),
		EndAt:       start.In(s.loc).Add(time.Duration(duration) * time.Minute),
		Capacity:    1,
		Status:      domain.SlotStatusOpen,
	})
	if err != nil {
		return 0, err
	}
	return slot.ID, nil
}

func (s *appStore) slotIDByAdminStart(ctx context.Context, adminID int64, start time.Time) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
SELECT id
FROM schedule_slots
WHERE admin_user_id = $1 AND start_at = $2
LIMIT 1;
`, adminID, start.In(s.loc)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, store.ErrNotFound
	}
	return id, err
}

func (s *appStore) availableSlotIDByStart(ctx context.Context, start time.Time) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
SELECT s.id
FROM schedule_slots s
LEFT JOIN bookings b ON b.slot_id = s.id AND b.status IN ('booked', 'blocked')
WHERE s.status = 'open'
  AND s.start_at = $1
GROUP BY s.id
HAVING COUNT(b.id) < s.capacity
ORDER BY s.start_at ASC
LIMIT 1;
`, start.In(s.loc)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, store.ErrSlotUnavailable
	}
	return id, err
}

func (s *appStore) availableSlotIDByAdminStart(ctx context.Context, adminID int64, start time.Time) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
SELECT s.id
FROM schedule_slots s
LEFT JOIN bookings b ON b.slot_id = s.id AND b.status IN ('booked', 'blocked')
WHERE s.admin_user_id = $1
  AND s.status = 'open'
  AND s.start_at = $2
GROUP BY s.id
HAVING COUNT(b.id) < s.capacity
ORDER BY s.start_at ASC
LIMIT 1;
`, adminID, start.In(s.loc)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, store.ErrSlotUnavailable
	}
	return id, err
}

func (s *appStore) bookingIDByAdminUserStart(ctx context.Context, adminTelegramID int64, username string, start time.Time) (int64, error) {
	adminID, err := s.userIDByTelegram(ctx, adminTelegramID)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.db.QueryRowContext(ctx, `
SELECT b.id
FROM bookings b
JOIN schedule_slots s ON s.id = b.slot_id
JOIN users u ON u.id = b.user_id
WHERE s.admin_user_id = $1
  AND u.username = $2
  AND s.start_at = $3
  AND b.status = 'booked'
LIMIT 1;
`, adminID, normalizeUsername(username), start.In(s.loc)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, store.ErrNotFound
	}
	return id, err
}

func (s *appStore) intSetting(ctx context.Context, userID int64, key string, fallback int) (int, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `
SELECT value
FROM admin_settings
WHERE admin_user_id = $1 AND key = $2;
`, userID, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return fallback, nil
	}
	if err != nil {
		return fallback, err
	}
	var out int
	if _, err := fmt.Sscanf(raw, "%d", &out); err != nil || out < 0 {
		return fallback, err
	}
	return out, nil
}

func (s *appStore) stringSetting(ctx context.Context, userID int64, key, fallback string) (string, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `
SELECT value
FROM admin_settings
WHERE admin_user_id = $1 AND key = $2;
`, userID, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return fallback, nil
	}
	if err != nil {
		return fallback, err
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	return raw, nil
}

func (s *appStore) saveTimesForUser(ctx context.Context, telegramID int64, key string, values []time.Time) error {
	userID, err := s.userIDByTelegram(ctx, telegramID)
	if err != nil {
		return err
	}
	raw := make([]string, 0, len(values))
	for _, value := range values {
		raw = append(raw, value.In(s.loc).Format(time.RFC3339))
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return s.repo.SetAdminSetting(ctx, userID, key, string(data))
}

func (s *appStore) loadTimesForUser(ctx context.Context, telegramID int64, key string) ([]time.Time, error) {
	userID, err := s.userIDByTelegram(ctx, telegramID)
	if err != nil {
		return nil, err
	}
	raw, err := s.stringSetting(ctx, userID, key, "")
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return nil, store.ErrNotFound
	}
	var encoded []string
	if err := json.Unmarshal([]byte(raw), &encoded); err != nil {
		return nil, err
	}
	out := make([]time.Time, 0, len(encoded))
	for _, item := range encoded {
		parsed, err := time.Parse(time.RFC3339, item)
		if err != nil {
			return nil, err
		}
		out = append(out, parsed.In(s.loc))
	}
	return out, nil
}

func normalizeUsername(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "@")
	return strings.ToLower(value)
}

func normalizeTravel(minutes int) int {
	if minutes <= 0 {
		return defaultTravelMinutes
	}
	return minutes
}

func validBotRole(role bot.Role) bool {
	return role == bot.RoleUser || role == bot.RoleAdmin || role == bot.RoleSuperAdmin
}

func splitName(fullName string) (string, string) {
	parts := strings.Fields(fullName)
	if len(parts) == 0 {
		return "", ""
	}
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], strings.Join(parts[1:], " ")
}

func parseWeekdaysSetting(raw string) ([]string, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	raw = strings.ReplaceAll(raw, " ", "")
	raw = strings.ReplaceAll(raw, "пн", "mon")
	raw = strings.ReplaceAll(raw, "вт", "tue")
	raw = strings.ReplaceAll(raw, "ср", "wed")
	raw = strings.ReplaceAll(raw, "чт", "thu")
	raw = strings.ReplaceAll(raw, "пт", "fri")
	raw = strings.ReplaceAll(raw, "сб", "sat")
	raw = strings.ReplaceAll(raw, "вс", "sun")
	if strings.Contains(raw, "-") && !strings.Contains(raw, ",") {
		parts := strings.Split(raw, "-")
		if len(parts) != 2 {
			return nil, store.ErrInvalidArgument
		}
		start, ok := weekdayName(parts[0])
		if !ok {
			return nil, store.ErrInvalidArgument
		}
		end, ok := weekdayName(parts[1])
		if !ok {
			return nil, store.ErrInvalidArgument
		}
		var out []string
		for day := start; ; day = (day + 1) % 7 {
			out = append(out, weekdayString(day))
			if day == end {
				return out, nil
			}
		}
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		day, ok := weekdayName(part)
		if !ok {
			return nil, store.ErrInvalidArgument
		}
		out = append(out, weekdayString(day))
	}
	return out, nil
}

func parseStoredWeekdays(raw string) ([]time.Weekday, error) {
	var out []time.Weekday
	for _, part := range strings.Split(raw, ",") {
		day, ok := weekdayName(part)
		if !ok {
			return nil, store.ErrInvalidArgument
		}
		out = append(out, day)
	}
	return out, nil
}

func weekdayName(raw string) (time.Weekday, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "mon", "monday", "1":
		return time.Monday, true
	case "tue", "tuesday", "2":
		return time.Tuesday, true
	case "wed", "wednesday", "3":
		return time.Wednesday, true
	case "thu", "thursday", "4":
		return time.Thursday, true
	case "fri", "friday", "5":
		return time.Friday, true
	case "sat", "saturday", "6":
		return time.Saturday, true
	case "sun", "sunday", "7":
		return time.Sunday, true
	default:
		return time.Sunday, false
	}
}

func weekdayString(day time.Weekday) string {
	switch day {
	case time.Monday:
		return "mon"
	case time.Tuesday:
		return "tue"
	case time.Wednesday:
		return "wed"
	case time.Thursday:
		return "thu"
	case time.Friday:
		return "fri"
	case time.Saturday:
		return "sat"
	default:
		return "sun"
	}
}

func parseDayRangeSetting(raw string) (time.Duration, time.Duration, error) {
	parts := strings.Split(strings.TrimSpace(raw), "-")
	if len(parts) != 2 {
		return 0, 0, store.ErrInvalidArgument
	}
	start, err := parseClockSetting(parts[0])
	if err != nil {
		return 0, 0, err
	}
	end, err := parseClockSetting(parts[1])
	if err != nil {
		return 0, 0, err
	}
	return start, end, nil
}

func parseClockSetting(raw string) (time.Duration, error) {
	var hour, minute int
	if _, err := fmt.Sscanf(strings.TrimSpace(raw), "%d:%d", &hour, &minute); err != nil {
		return 0, err
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, store.ErrInvalidArgument
	}
	return time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute, nil
}

func formatClockDuration(value time.Duration) string {
	totalMinutes := int(value / time.Minute)
	return fmt.Sprintf("%02d:%02d", totalMinutes/60, totalMinutes%60)
}

func userReminderDayBefore(language string, startAt time.Time) string {
	if language == bot.LangEN {
		return fmt.Sprintf("Reminder: you have a booking on %s.", startAt.Format("02.01.2006 15:04"))
	}
	return fmt.Sprintf("Напоминание: у вас запись %s.", startAt.Format("02.01.2006 15:04"))
}

func userReminderTravel(language string, startAt time.Time) string {
	if language == bot.LangEN {
		return fmt.Sprintf("Your booking starts at %s. Consider your travel time.", startAt.Format("15:04"))
	}
	return fmt.Sprintf("Скоро запись в %s. Учтите время в пути.", startAt.Format("15:04"))
}

func adminReminderDayBefore(language, userLabel string, startAt time.Time) string {
	if language == bot.LangEN {
		return fmt.Sprintf("Reminder: tomorrow client %s has a booking at %s.", userLabel, startAt.Format("02.01.2006 15:04"))
	}
	return fmt.Sprintf("Напоминание: завтра запись у клиента %s в %s.", userLabel, startAt.Format("02.01.2006 15:04"))
}
