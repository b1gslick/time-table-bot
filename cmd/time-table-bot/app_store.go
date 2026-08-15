package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"time-table-bot/internal/bot"
	"time-table-bot/internal/domain"
	"time-table-bot/internal/scheduler"
	"time-table-bot/internal/store"
)

const (
	defaultSessionDuration = 60
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

func (s *appStore) ListAdmins(ctx context.Context) ([]bot.AdminView, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT u.username,
       u.role,
       COUNT(DISTINCT svc.id) FILTER (WHERE svc.is_active = TRUE) AS active_services,
       COUNT(DISTINCT sl.id) FILTER (
           WHERE sl.status = 'open'
             AND NOT EXISTS (
                 SELECT 1 FROM bookings b
                 WHERE b.slot_id = sl.id AND b.status IN ('booked', 'blocked')
             )
       ) AS open_slots,
       COUNT(DISTINCT b.id) FILTER (WHERE b.status = 'booked') AS booked_slots
FROM users u
LEFT JOIN admin_services svc ON svc.admin_user_id = u.id
LEFT JOIN schedule_slots sl ON sl.admin_user_id = u.id
LEFT JOIN bookings b ON b.slot_id = sl.id
WHERE u.role IN ('admin', 'super_admin')
GROUP BY u.username, u.role
ORDER BY u.role = 'super_admin' DESC, u.username ASC;
`)
	if err != nil {
		return nil, fmt.Errorf("list admins: %w", err)
	}
	defer rows.Close()

	var out []bot.AdminView
	for rows.Next() {
		var item bot.AdminView
		if err := rows.Scan(&item.Username, &item.Role, &item.ActiveServices, &item.OpenSlots, &item.BookedSlots); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *appStore) GetSuperAdminView(ctx context.Context, telegramID int64) (bot.SuperAdminView, error) {
	userID, err := s.userIDByTelegram(ctx, telegramID)
	if err != nil {
		return bot.SuperAdminView{}, err
	}
	raw, err := s.stringSetting(ctx, userID, "super_admin_view", "")
	if err != nil {
		return bot.SuperAdminView{}, err
	}
	if raw == "" {
		return bot.SuperAdminView{}, store.ErrNotFound
	}
	var view bot.SuperAdminView
	if err := json.Unmarshal([]byte(raw), &view); err != nil {
		return bot.SuperAdminView{}, err
	}
	if view.Role == "" {
		return bot.SuperAdminView{}, store.ErrNotFound
	}
	return view, nil
}

func (s *appStore) SetSuperAdminView(ctx context.Context, telegramID int64, view bot.SuperAdminView) error {
	userID, err := s.userIDByTelegram(ctx, telegramID)
	if err != nil {
		return err
	}
	if view.Role == "" || view.Role == bot.RoleSuperAdmin {
		_, err := s.db.ExecContext(ctx, `
DELETE FROM admin_settings
WHERE admin_user_id = $1 AND key = 'super_admin_view';
`, userID)
		return err
	}
	if view.Role != bot.RoleAdmin && view.Role != bot.RoleUser {
		return store.ErrInvalidArgument
	}
	view.AdminUsername = normalizeUsername(view.AdminUsername)
	if view.Role == bot.RoleAdmin {
		if view.AdminUsername == "" {
			return store.ErrInvalidArgument
		}
		target, err := s.lookupUser(ctx, "username = $1", view.AdminUsername)
		if err != nil {
			return err
		}
		if target.Role != domain.RoleAdmin && target.Role != domain.RoleSuperAdmin {
			return store.ErrInvalidArgument
		}
	} else {
		view.AdminUsername = ""
	}
	data, err := json.Marshal(view)
	if err != nil {
		return err
	}
	return s.repo.SetAdminSetting(ctx, userID, "super_admin_view", string(data))
}

func (s *appStore) SetProfileText(ctx context.Context, adminTelegramID int64, text string) error {
	adminID, ok, err := s.targetAdminIDForServiceScope(ctx, adminTelegramID)
	if err != nil {
		return err
	}
	if !ok {
		return store.ErrInvalidArgument
	}
	return s.repo.UpsertAdminProfile(ctx, domain.AdminProfile{
		UserID:        adminID,
		DisplayName:   "",
		Description:   strings.TrimSpace(text),
		Timezone:      s.loc.String(),
		BookingNotice: 60,
	})
}

func (s *appStore) GetProfileText(ctx context.Context, adminTelegramID int64) (string, error) {
	adminID, ok, err := s.targetAdminIDForServiceScope(ctx, adminTelegramID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", store.ErrInvalidArgument
	}
	var description string
	err = s.db.QueryRowContext(ctx, `
SELECT COALESCE(description, '')
FROM admin_profiles
WHERE user_id = $1;
`, adminID).Scan(&description)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(description), nil
}

func (s *appStore) SetServicesText(ctx context.Context, adminTelegramID int64, text string) error {
	adminID, ok, err := s.targetAdminIDForServiceScope(ctx, adminTelegramID)
	if err != nil {
		return err
	}
	if !ok {
		return store.ErrInvalidArgument
	}
	return s.repo.SetAdminSetting(ctx, adminID, "services_text", strings.TrimSpace(text))
}

func (s *appStore) GetServicesText(ctx context.Context, adminTelegramID int64) (string, error) {
	adminID, ok, err := s.targetAdminIDForServiceScope(ctx, adminTelegramID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", store.ErrInvalidArgument
	}
	return s.stringSetting(ctx, adminID, "services_text", "")
}

func (s *appStore) SetCategoryOrder(ctx context.Context, adminTelegramID int64, categories []string) error {
	adminID, ok, err := s.targetAdminIDForServiceScope(ctx, adminTelegramID)
	if err != nil {
		return err
	}
	if !ok {
		return store.ErrInvalidArgument
	}
	data, err := json.Marshal(categories)
	if err != nil {
		return err
	}
	return s.repo.SetAdminSetting(ctx, adminID, "category_order", string(data))
}

func (s *appStore) AddService(ctx context.Context, adminTelegramID int64, name string, durationMin int, priceText string) error {
	if strings.TrimSpace(name) == "" || durationMin <= 0 {
		return store.ErrInvalidArgument
	}
	adminID, ok, err := s.targetAdminIDForServiceScope(ctx, adminTelegramID)
	if err != nil {
		return err
	}
	if !ok {
		return store.ErrInvalidArgument
	}
	category, subcategory, serviceName := parseServicePath(name)
	_, err = s.repo.UpsertAdminService(ctx, domain.AdminService{
		AdminUserID: adminID,
		Category:    category,
		Subcategory: subcategory,
		Name:        serviceName,
		Description: strings.TrimSpace(priceText),
		DurationMin: durationMin,
		IsActive:    true,
	})
	return err
}

func (s *appStore) DeleteServiceByIndex(ctx context.Context, adminTelegramID int64, index int) error {
	if index <= 0 {
		return store.ErrInvalidArgument
	}
	adminID, ok, err := s.targetAdminIDForServiceScope(ctx, adminTelegramID)
	if err != nil {
		return err
	}
	if !ok {
		return store.ErrInvalidArgument
	}
	services, err := s.ListServices(ctx, adminTelegramID)
	if err != nil {
		return err
	}
	if index > len(services) {
		return store.ErrInvalidArgument
	}
	serviceID := services[index-1].ID
	result, err := s.db.ExecContext(ctx, `
UPDATE admin_services
SET is_active = FALSE, updated_at = NOW()
WHERE id = $1 AND admin_user_id = $2 AND is_active = TRUE;
`, serviceID, adminID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return store.ErrNotFound
	}
	_ = s.clearSettingForUser(ctx, adminTelegramID, "last_services")
	_ = s.clearSettingForUser(ctx, adminTelegramID, "last_availability_slots")
	return nil
}

func (s *appStore) EditServiceByIndex(ctx context.Context, adminTelegramID int64, index int, name string, durationMin int, priceText string) error {
	if index <= 0 || strings.TrimSpace(name) == "" || durationMin <= 0 {
		return store.ErrInvalidArgument
	}
	services, err := s.ListServices(ctx, adminTelegramID)
	if err != nil {
		return err
	}
	if index > len(services) {
		return store.ErrInvalidArgument
	}
	category, subcategory, serviceName := parseServicePath(name)
	result, err := s.db.ExecContext(ctx, `
UPDATE admin_services
SET category = $1,
    subcategory = $2,
    name = $3,
    description = $4,
    duration_min = $5,
    updated_at = NOW()
WHERE id = $6 AND is_active = TRUE;
`, category, subcategory, serviceName, strings.TrimSpace(priceText), durationMin, services[index-1].ID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return store.ErrNotFound
	}
	_ = s.clearSettingForUser(ctx, adminTelegramID, "last_services")
	_ = s.clearSettingForUser(ctx, adminTelegramID, "last_availability_slots")
	return nil
}

func (s *appStore) ListServices(ctx context.Context, telegramID int64) ([]bot.ServiceView, error) {
	targetAdminID, hasTargetAdmin, err := s.targetAdminIDForServiceScope(ctx, telegramID)
	if err != nil {
		return nil, err
	}

	query := `
SELECT svc.id, COALESCE(a.username, ''), svc.category, svc.subcategory, svc.name, svc.description, svc.duration_min, svc.price_cents
FROM admin_services svc
JOIN users a ON a.id = svc.admin_user_id
WHERE svc.is_active = TRUE
`
	args := []any{}
	if hasTargetAdmin {
		query += " AND svc.admin_user_id = $1"
		args = append(args, targetAdminID)
	}
	query += " ORDER BY a.username ASC, svc.created_at ASC;"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	defer rows.Close()

	var services []bot.ServiceView
	for rows.Next() {
		var item bot.ServiceView
		if err := rows.Scan(&item.ID, &item.AdminName, &item.Category, &item.Subcategory, &item.Name, &item.Description, &item.DurationMin, &item.PriceCents); err != nil {
			return nil, err
		}
		services = append(services, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if hasTargetAdmin {
		if order, err := s.categoryOrder(ctx, targetAdminID); err == nil && len(order) > 0 {
			sortServicesByCategoryOrder(services, order)
		}
	}
	if telegramID > 0 {
		ids := make([]int64, 0, len(services))
		for _, service := range services {
			ids = append(ids, service.ID)
		}
		_ = s.saveInt64sForUser(ctx, telegramID, "last_services", ids)
	}
	return services, nil
}

func (s *appStore) MasterIntro(ctx context.Context, telegramID int64) (string, error) {
	targetAdminID, hasTargetAdmin, err := s.targetAdminIDForServiceScope(ctx, telegramID)
	if err != nil {
		return "", err
	}
	query := `
SELECT u.username,
       COALESCE(p.description, '') AS description,
       COALESCE(st.value, '') AS services_text
FROM users u
LEFT JOIN admin_profiles p ON p.user_id = u.id
LEFT JOIN admin_settings st ON st.admin_user_id = u.id AND st.key = 'services_text'
WHERE u.role IN ('admin', 'super_admin')
`
	args := []any{}
	if hasTargetAdmin {
		query += "  AND u.id = $1\n"
		args = append(args, targetAdminID)
	}
	query += `
ORDER BY u.username ASC
LIMIT 5;
`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var parts []string
	for rows.Next() {
		var username, description, servicesText string
		if err := rows.Scan(&username, &description, &servicesText); err != nil {
			return "", err
		}
		var sb strings.Builder
		sb.WriteString("@")
		sb.WriteString(username)
		if strings.TrimSpace(description) != "" {
			sb.WriteString("\n")
			sb.WriteString(strings.TrimSpace(description))
		}
		if strings.TrimSpace(servicesText) != "" {
			sb.WriteString("\n")
			sb.WriteString(masterServicesText(servicesText, username))
		}
		parts = append(parts, sb.String())
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(parts) == 0 {
		return "", nil
	}
	return strings.Join(parts, "\n\n"), nil
}

func masterServicesText(text, username string) string {
	text = strings.TrimSpace(text)
	username = strings.TrimPrefix(strings.TrimSpace(username), "@")
	if text == "" || username == "" {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		normalized := strings.ToLower(strings.TrimSpace(line))
		if strings.Contains(normalized, "если вас интерес") &&
			strings.Contains(normalized, "услуг") &&
			strings.Contains(normalized, "обращ") {
			lines[i] = "Если вас интересует какая-либо из услуг, обращайтесь: @" + username
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func (s *appStore) SetWorkHoursText(ctx context.Context, adminTelegramID int64, text string) error {
	adminID, err := s.effectiveAdminIDByTelegram(ctx, adminTelegramID)
	if err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if err := s.repo.SetAdminSetting(ctx, adminID, "work_hours_text", text); err != nil {
		return err
	}
	_, _ = s.db.ExecContext(ctx, `
DELETE FROM admin_settings
WHERE admin_user_id = $1 AND key = 'weekly_hours';
`, adminID)
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
	_ = s.clearScheduleCacheForAdminID(ctx, adminID)
	return nil
}

func (s *appStore) SetWeeklyHours(ctx context.Context, adminTelegramID int64, hours []bot.WeekdayHours) error {
	adminID, err := s.effectiveAdminIDByTelegram(ctx, adminTelegramID)
	if err != nil {
		return err
	}
	normalized := make([]bot.WeekdayHours, 0, len(hours))
	var workDays []string
	var summary []string
	var firstStart, firstEnd string
	for _, item := range hours {
		if item.Weekday < time.Sunday || item.Weekday > time.Saturday {
			return store.ErrInvalidArgument
		}
		entry := bot.WeekdayHours{Weekday: item.Weekday, Working: item.Working}
		if item.Working {
			start, err := parseClockSetting(item.Start)
			if err != nil {
				return err
			}
			end, err := parseClockSetting(item.End)
			if err != nil || end <= start {
				return store.ErrInvalidArgument
			}
			entry.Start = formatClockDuration(start)
			entry.End = formatClockDuration(end)
			workDays = append(workDays, weekdayString(item.Weekday))
			summary = append(summary, fmt.Sprintf("%s %s-%s", weekdayString(item.Weekday), entry.Start, entry.End))
			if firstStart == "" {
				firstStart = entry.Start
				firstEnd = entry.End
			}
		}
		normalized = append(normalized, entry)
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return err
	}
	if err := s.repo.SetAdminSetting(ctx, adminID, "weekly_hours", string(data)); err != nil {
		return err
	}
	_ = s.repo.SetAdminSetting(ctx, adminID, "work_hours_text", strings.Join(summary, "; "))
	_ = s.repo.SetAdminSetting(ctx, adminID, "work_days", strings.Join(workDays, ","))
	if firstStart != "" {
		_ = s.repo.SetAdminSetting(ctx, adminID, "work_start", firstStart)
		_ = s.repo.SetAdminSetting(ctx, adminID, "work_end", firstEnd)
	}
	_ = s.clearScheduleCacheForAdminID(ctx, adminID)
	return nil
}

func (s *appStore) SetSessionDuration(ctx context.Context, adminTelegramID int64, durationMin int) error {
	if durationMin <= 0 {
		return store.ErrInvalidArgument
	}
	adminID, err := s.effectiveAdminIDByTelegram(ctx, adminTelegramID)
	if err != nil {
		return err
	}
	if err := s.repo.SetAdminSetting(ctx, adminID, "session_duration", fmt.Sprintf("%d", durationMin)); err != nil {
		return err
	}
	_ = s.clearScheduleCacheForAdminID(ctx, adminID)
	return nil
}

func (s *appStore) GenerateSchedule(ctx context.Context, adminTelegramID int64, req bot.GenerateScheduleRequest) (bot.GenerateScheduleResult, error) {
	adminID, err := s.effectiveAdminIDByTelegram(ctx, adminTelegramID)
	if err != nil {
		return bot.GenerateScheduleResult{}, err
	}
	if req.Month.IsZero() && req.Date.IsZero() {
		return bot.GenerateScheduleResult{}, store.ErrInvalidArgument
	}
	if req.DurationMin <= 0 {
		req.DurationMin, err = s.intSetting(ctx, adminID, "session_duration", defaultSessionDuration)
		if err != nil || req.DurationMin <= 0 {
			req.DurationMin = defaultSessionDuration
		}
	}
	if req.DurationMin <= 0 {
		return bot.GenerateScheduleResult{}, store.ErrInvalidArgument
	}
	if req.Months <= 0 {
		req.Months = 1
	}
	if req.Months > 12 {
		return bot.GenerateScheduleResult{}, store.ErrInvalidArgument
	}

	if !req.Date.IsZero() {
		if req.DayEnd <= req.DayStart {
			return bot.GenerateScheduleResult{}, store.ErrInvalidArgument
		}
		day := dateOnlyLocal(req.Date, s.loc)
		blocked, err := s.blockedDateSet(ctx, adminID)
		if err != nil {
			return bot.GenerateScheduleResult{}, err
		}
		var result bot.GenerateScheduleResult
		if !blocked[day.Format("2006-01-02")] {
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
		_ = s.clearScheduleCacheForAdminID(ctx, adminID)
		return result, nil
	}

	rules, err := s.scheduleRules(ctx, adminID, req)
	if err != nil {
		return bot.GenerateScheduleResult{}, err
	}
	blocked, err := s.blockedDateSet(ctx, adminID)
	if err != nil {
		return bot.GenerateScheduleResult{}, err
	}

	monthStart := time.Date(req.Month.Year(), req.Month.Month(), 1, 0, 0, 0, 0, s.loc)
	var result bot.GenerateScheduleResult
	for monthOffset := 0; monthOffset < req.Months; monthOffset++ {
		currentMonthStart := monthStart.AddDate(0, monthOffset, 0)
		currentMonthEnd := currentMonthStart.AddDate(0, 1, 0)
		for day := currentMonthStart; day.Before(currentMonthEnd); day = day.AddDate(0, 0, 1) {
			if blocked[day.Format("2006-01-02")] {
				continue
			}
			rule, ok := rules[day.Weekday()]
			if !ok {
				continue
			}
			for offset := rule.Start; offset+time.Duration(req.DurationMin)*time.Minute <= rule.End; offset += time.Duration(req.DurationMin) * time.Minute {
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
	}
	_ = s.clearScheduleCacheForAdminID(ctx, adminID)
	return result, nil
}

type scheduleRule struct {
	Start time.Duration
	End   time.Duration
}

var weeklyScheduleOrder = []time.Weekday{
	time.Monday,
	time.Tuesday,
	time.Wednesday,
	time.Thursday,
	time.Friday,
	time.Saturday,
	time.Sunday,
}

func (s *appStore) scheduleRules(ctx context.Context, adminID int64, req bot.GenerateScheduleRequest) (map[time.Weekday]scheduleRule, error) {
	if len(req.Weekdays) > 0 {
		if req.DayEnd <= req.DayStart {
			return nil, store.ErrInvalidArgument
		}
		out := make(map[time.Weekday]scheduleRule, len(req.Weekdays))
		for _, day := range req.Weekdays {
			out[day] = scheduleRule{Start: req.DayStart, End: req.DayEnd}
		}
		return out, nil
	}

	if rules, err := s.weeklyScheduleRules(ctx, adminID); err == nil && len(rules) > 0 {
		return rules, nil
	}

	raw, err := s.stringSetting(ctx, adminID, "work_days", "")
	if err != nil || raw == "" {
		return nil, store.ErrInvalidArgument
	}
	days, err := parseStoredWeekdays(raw)
	if err != nil {
		return nil, err
	}
	startRaw, err1 := s.stringSetting(ctx, adminID, "work_start", "")
	endRaw, err2 := s.stringSetting(ctx, adminID, "work_end", "")
	if err1 != nil || err2 != nil || startRaw == "" || endRaw == "" {
		return nil, store.ErrInvalidArgument
	}
	start, err := parseClockSetting(startRaw)
	if err != nil {
		return nil, err
	}
	end, err := parseClockSetting(endRaw)
	if err != nil || end <= start {
		return nil, store.ErrInvalidArgument
	}
	out := make(map[time.Weekday]scheduleRule, len(days))
	for _, day := range days {
		out[day] = scheduleRule{Start: start, End: end}
	}
	return out, nil
}

func (s *appStore) weeklyScheduleRules(ctx context.Context, adminID int64) (map[time.Weekday]scheduleRule, error) {
	raw, err := s.stringSetting(ctx, adminID, "weekly_hours", "")
	if err != nil || raw == "" {
		return nil, store.ErrNotFound
	}
	var hours []bot.WeekdayHours
	if err := json.Unmarshal([]byte(raw), &hours); err != nil {
		return nil, err
	}
	out := make(map[time.Weekday]scheduleRule)
	for _, item := range hours {
		if !item.Working {
			continue
		}
		start, err := parseClockSetting(item.Start)
		if err != nil {
			return nil, err
		}
		end, err := parseClockSetting(item.End)
		if err != nil || end <= start {
			return nil, store.ErrInvalidArgument
		}
		out[item.Weekday] = scheduleRule{Start: start, End: end}
	}
	return out, nil
}

func (s *appStore) DeleteScheduleMonth(ctx context.Context, adminTelegramID int64, monthStart time.Time) (bot.DeleteScheduleResult, error) {
	adminID, err := s.effectiveAdminIDByTelegram(ctx, adminTelegramID)
	if err != nil {
		return bot.DeleteScheduleResult{}, err
	}
	if monthStart.IsZero() {
		return bot.DeleteScheduleResult{}, store.ErrInvalidArgument
	}
	from := time.Date(monthStart.In(s.loc).Year(), monthStart.In(s.loc).Month(), 1, 0, 0, 0, 0, s.loc)
	to := from.AddDate(0, 1, 0)
	result, err := s.db.ExecContext(ctx, `
DELETE FROM schedule_slots
WHERE admin_user_id = $1 AND start_at >= $2 AND start_at < $3;
`, adminID, from, to)
	if err != nil {
		return bot.DeleteScheduleResult{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return bot.DeleteScheduleResult{}, err
	}
	_ = s.clearScheduleCacheForAdminID(ctx, adminID)
	return bot.DeleteScheduleResult{Deleted: int(affected)}, nil
}

func (s *appStore) ListScheduleMonths(ctx context.Context, adminTelegramID int64) ([]bot.ScheduleMonth, error) {
	adminID, err := s.effectiveAdminIDByTelegram(ctx, adminTelegramID)
	if err != nil {
		return nil, err
	}
	now := time.Now().In(s.loc)
	rows, err := s.db.QueryContext(ctx, `
SELECT date_trunc('month', start_at AT TIME ZONE $3)::date AS month,
       COUNT(*) AS slot_count
FROM schedule_slots
WHERE admin_user_id = $1
  AND start_at >= $2
GROUP BY month
ORDER BY month ASC;
`, adminID, now, s.loc.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []bot.ScheduleMonth
	for rows.Next() {
		var (
			month time.Time
			count int
		)
		if err := rows.Scan(&month, &count); err != nil {
			return nil, err
		}
		out = append(out, bot.ScheduleMonth{
			Month:     time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, s.loc),
			SlotCount: count,
		})
	}
	return out, rows.Err()
}

func (s *appStore) ListScheduleDays(ctx context.Context, adminTelegramID int64, monthStart time.Time) ([]bot.ScheduleDay, error) {
	adminID, err := s.effectiveAdminIDByTelegram(ctx, adminTelegramID)
	if err != nil {
		return nil, err
	}
	from := time.Date(monthStart.In(s.loc).Year(), monthStart.In(s.loc).Month(), 1, 0, 0, 0, 0, s.loc)
	to := from.AddDate(0, 1, 0)
	from = maxTime(from, time.Now().In(s.loc))
	if !from.Before(to) {
		return []bot.ScheduleDay{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT date_trunc('day', start_at AT TIME ZONE $4)::date AS day,
       COUNT(*) AS slot_count
FROM schedule_slots
WHERE admin_user_id = $1
  AND start_at >= $2
  AND start_at < $3
GROUP BY day
ORDER BY day ASC;
`, adminID, from, to, s.loc.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []bot.ScheduleDay
	for rows.Next() {
		var (
			day   time.Time
			count int
		)
		if err := rows.Scan(&day, &count); err != nil {
			return nil, err
		}
		out = append(out, bot.ScheduleDay{
			Date:      time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, s.loc),
			SlotCount: count,
		})
	}
	return out, rows.Err()
}

func (s *appStore) ListScheduleWeekdays(ctx context.Context, adminTelegramID int64, monthStart time.Time) ([]bot.ScheduleWeekday, error) {
	days, err := s.ListScheduleDays(ctx, adminTelegramID, monthStart)
	if err != nil {
		return nil, err
	}
	counts := make(map[time.Weekday]int)
	for _, day := range days {
		counts[day.Date.Weekday()] += day.SlotCount
	}
	out := make([]bot.ScheduleWeekday, 0, len(counts))
	for _, weekday := range weeklyScheduleOrder {
		if count := counts[weekday]; count > 0 {
			out = append(out, bot.ScheduleWeekday{Weekday: weekday, SlotCount: count})
		}
	}
	return out, nil
}

func (s *appStore) BlockScheduleDate(ctx context.Context, adminTelegramID int64, date time.Time) (bot.BlockDateResult, error) {
	adminID, err := s.effectiveAdminIDByTelegram(ctx, adminTelegramID)
	if err != nil {
		return bot.BlockDateResult{}, err
	}
	day := dateOnlyLocal(date, s.loc)
	blocked, err := s.blockedDateSet(ctx, adminID)
	if err != nil {
		return bot.BlockDateResult{}, err
	}
	blocked[day.Format("2006-01-02")] = true
	if err := s.saveBlockedDateSet(ctx, adminID, blocked); err != nil {
		return bot.BlockDateResult{}, err
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE schedule_slots s
SET status = 'closed', updated_at = NOW()
WHERE s.admin_user_id = $1
  AND s.start_at >= $2
  AND s.start_at < $3
  AND s.status = 'open'
  AND NOT EXISTS (
      SELECT 1 FROM bookings b
      WHERE b.slot_id = s.id AND b.status = 'booked'
  );
`, adminID, day, day.AddDate(0, 0, 1))
	if err != nil {
		return bot.BlockDateResult{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return bot.BlockDateResult{}, err
	}
	_ = s.clearScheduleCacheForAdminID(ctx, adminID)
	return bot.BlockDateResult{Date: day, ClosedSlots: int(affected)}, nil
}

func (s *appStore) blockedDateSet(ctx context.Context, adminID int64) (map[string]bool, error) {
	raw, err := s.stringSetting(ctx, adminID, "blocked_dates", "")
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	if raw == "" {
		return out, nil
	}
	var dates []string
	if err := json.Unmarshal([]byte(raw), &dates); err != nil {
		return nil, err
	}
	for _, date := range dates {
		date = strings.TrimSpace(date)
		if date != "" {
			out[date] = true
		}
	}
	return out, nil
}

func (s *appStore) saveBlockedDateSet(ctx context.Context, adminID int64, dates map[string]bool) error {
	values := make([]string, 0, len(dates))
	for date, blocked := range dates {
		if blocked {
			values = append(values, date)
		}
	}
	sort.Strings(values)
	data, err := json.Marshal(values)
	if err != nil {
		return err
	}
	return s.repo.SetAdminSetting(ctx, adminID, "blocked_dates", string(data))
}

func (s *appStore) AddBookingByUsername(ctx context.Context, adminTelegramID int64, username string, start time.Time) (bot.BookingChangeResult, error) {
	adminID, err := s.effectiveAdminIDByTelegram(ctx, adminTelegramID)
	if err != nil {
		return bot.BookingChangeResult{}, err
	}
	clientID, err := s.ensureUserByUsername(ctx, username)
	if err != nil {
		return bot.BookingChangeResult{}, err
	}
	slotID, err := s.ensureSlot(ctx, adminID, start)
	if err != nil {
		return bot.BookingChangeResult{}, err
	}
	booking, err := s.repo.CreateBooking(ctx, domain.Booking{
		SlotID:        slotID,
		UserID:        &clientID,
		Status:        domain.BookingStatusBooked,
		TravelMinutes: 0,
		Note:          "created_by_admin",
	})
	if err != nil {
		return bot.BookingChangeResult{}, err
	}
	return s.bookingChangeByID(ctx, booking.ID, adminID)
}

func (s *appStore) AddBookingByPhone(ctx context.Context, adminTelegramID int64, phone string, start time.Time) (bot.BookingChangeResult, error) {
	adminID, err := s.effectiveAdminIDByTelegram(ctx, adminTelegramID)
	if err != nil {
		return bot.BookingChangeResult{}, err
	}
	clientID, err := s.ensureUserByPhone(ctx, phone)
	if err != nil {
		return bot.BookingChangeResult{}, err
	}
	slotID, err := s.ensureSlot(ctx, adminID, start)
	if err != nil {
		return bot.BookingChangeResult{}, err
	}
	booking, err := s.repo.CreateBooking(ctx, domain.Booking{
		SlotID:        slotID,
		UserID:        &clientID,
		Status:        domain.BookingStatusBooked,
		TravelMinutes: 0,
		Note:          "created_by_admin_phone",
	})
	if err != nil {
		return bot.BookingChangeResult{}, err
	}
	return s.bookingChangeByID(ctx, booking.ID, adminID)
}

func (s *appStore) AddBookingForContactByIndex(ctx context.Context, adminTelegramID int64, contactType, contact string, index int) (bot.BookingChangeResult, error) {
	adminID, err := s.effectiveAdminIDByTelegram(ctx, adminTelegramID)
	if err != nil {
		return bot.BookingChangeResult{}, err
	}
	var clientID int64
	switch contactType {
	case "phone":
		clientID, err = s.ensureUserByPhone(ctx, contact)
	case "telegram":
		clientID, err = s.ensureUserByUsername(ctx, contact)
	default:
		err = store.ErrInvalidArgument
	}
	if err != nil {
		return bot.BookingChangeResult{}, err
	}

	availability, err := s.loadAvailabilityForUser(ctx, adminTelegramID)
	if err != nil {
		return bot.BookingChangeResult{}, err
	}
	if index <= 0 || index > len(availability) {
		return bot.BookingChangeResult{}, store.ErrInvalidArgument
	}
	return s.bookAvailabilityForUserID(ctx, clientID, availability[index-1], adminID)
}

func (s *appStore) DeleteBookingByUsername(ctx context.Context, adminTelegramID int64, username string, start time.Time) (bot.BookingChangeResult, error) {
	bookingID, err := s.bookingIDByAdminUserStart(ctx, adminTelegramID, username, start)
	if err != nil {
		return bot.BookingChangeResult{}, err
	}
	return s.DeleteBookingByID(ctx, adminTelegramID, bookingID)
}

func (s *appStore) DeleteBookingByID(ctx context.Context, adminTelegramID int64, bookingID int64) (bot.BookingChangeResult, error) {
	adminID, err := s.adminIDFilterForBookingIDAction(ctx, adminTelegramID)
	if err != nil {
		return bot.BookingChangeResult{}, err
	}
	result, err := s.bookingChangeByID(ctx, bookingID, adminID)
	if err != nil {
		return bot.BookingChangeResult{}, err
	}
	if err := s.repo.DeleteBooking(ctx, bookingID, "cancelled_by_admin"); err != nil {
		return bot.BookingChangeResult{}, err
	}
	_, err = s.db.ExecContext(ctx, `
UPDATE bookings
SET status = 'cancelled', cancelled_at = NOW(), updated_at = NOW(), note = note || ';cancelled_by_admin'
WHERE status = 'blocked' AND note = $1;
`, coveredByBookingNote(bookingID))
	if err != nil {
		return bot.BookingChangeResult{}, err
	}
	return result, nil
}

func (s *appStore) adminIDFilterForBookingIDAction(ctx context.Context, telegramID int64) (int64, error) {
	actor, err := s.GetUserByTelegramID(ctx, telegramID)
	if err != nil {
		return 0, err
	}
	if actor.Role != bot.RoleSuperAdmin {
		return s.userIDByTelegram(ctx, telegramID)
	}
	view, err := s.GetSuperAdminView(ctx, telegramID)
	if err != nil || view.Role != bot.RoleAdmin || strings.TrimSpace(view.AdminUsername) == "" {
		return 0, nil
	}
	target, err := s.lookupUser(ctx, "username = $1", normalizeUsername(view.AdminUsername))
	if err != nil {
		return 0, err
	}
	if target.Role != domain.RoleAdmin && target.Role != domain.RoleSuperAdmin {
		return 0, store.ErrInvalidArgument
	}
	return target.ID, nil
}

func (s *appStore) RescheduleBookingByUsername(ctx context.Context, adminTelegramID int64, username string, fromStart, toStart time.Time) (bot.BookingChangeResult, error) {
	adminID, err := s.effectiveAdminIDByTelegram(ctx, adminTelegramID)
	if err != nil {
		return bot.BookingChangeResult{}, err
	}
	bookingID, err := s.bookingIDByAdminUserStart(ctx, adminTelegramID, username, fromStart)
	if err != nil {
		return bot.BookingChangeResult{}, err
	}
	result, err := s.bookingChangeByID(ctx, bookingID, adminID)
	if err != nil {
		return bot.BookingChangeResult{}, err
	}
	newSlotID, err := s.ensureSlot(ctx, adminID, toStart)
	if err != nil {
		return bot.BookingChangeResult{}, err
	}
	if err := s.repo.RescheduleBooking(ctx, bookingID, newSlotID); err != nil {
		return bot.BookingChangeResult{}, err
	}
	updated, err := s.bookingChangeByID(ctx, bookingID, adminID)
	if err != nil {
		return bot.BookingChangeResult{}, err
	}
	result.NewStartAt = updated.StartAt
	result.NewEndAt = updated.EndAt
	return result, nil
}

func (s *appStore) BlockSlot(ctx context.Context, adminTelegramID int64, start time.Time) error {
	adminID, err := s.effectiveAdminIDByTelegram(ctx, adminTelegramID)
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
	from = maxTime(from, time.Now().In(s.loc))
	if !from.Before(to) {
		if telegramID > 0 {
			_ = s.saveTimesForUser(ctx, telegramID, "last_free_slots", nil)
			_ = s.clearSettingForUser(ctx, telegramID, "last_availability_slots")
		}
		return []time.Time{}, nil
	}
	query := `
SELECT s.start_at
FROM schedule_slots s
JOIN users a ON a.id = s.admin_user_id
LEFT JOIN bookings b ON b.slot_id = s.id AND b.status IN ('booked', 'blocked')
WHERE s.status = 'open'
  AND s.start_at >= $1
  AND s.start_at < $2
`
	args := []any{from, to}
	if actor, err := s.GetUserByTelegramID(ctx, telegramID); err == nil {
		if actor.Role == bot.RoleAdmin {
			query += "  AND a.telegram_id = $3\n"
			args = append(args, telegramID)
		} else if actor.Role == bot.RoleSuperAdmin {
			if view, err := s.GetSuperAdminView(ctx, telegramID); err == nil && view.Role == bot.RoleAdmin && strings.TrimSpace(view.AdminUsername) != "" {
				query += "  AND a.username = $3\n"
				args = append(args, normalizeUsername(view.AdminUsername))
			}
		}
	}
	query += `
GROUP BY s.id
HAVING COUNT(b.id) < s.capacity
ORDER BY s.start_at ASC;
`
	rows, err := s.db.QueryContext(ctx, query, args...)
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
		_ = s.clearSettingForUser(ctx, telegramID, "last_availability_slots")
		if len(slots) == 0 {
			_ = s.notifyAdminsAboutMissingRequestedMonth(ctx, telegramID, from)
		}
	}
	return slots, nil
}

func (s *appStore) ListFreeSlotsForServices(ctx context.Context, telegramID int64, serviceIndexes []int, monthStart time.Time) ([]bot.AvailabilitySlot, error) {
	from := time.Date(monthStart.Year(), monthStart.Month(), 1, 0, 0, 0, 0, s.loc)
	to := from.AddDate(0, 1, 0)
	return s.listFreeSlotsForServices(ctx, telegramID, serviceIndexes, maxTime(from, time.Now().In(s.loc)), to, nil)
}

func (s *appStore) ListFreeSlotsForServicesRange(ctx context.Context, telegramID int64, serviceIndexes []int, from, to time.Time) ([]bot.AvailabilitySlot, error) {
	return s.listFreeSlotsForServices(ctx, telegramID, serviceIndexes, maxTime(from.In(s.loc), time.Now().In(s.loc)), to.In(s.loc), nil)
}

func (s *appStore) ListFreeSlotsForServicesDates(ctx context.Context, telegramID int64, serviceIndexes []int, dates []time.Time) ([]bot.AvailabilitySlot, error) {
	if len(dates) == 0 {
		return nil, store.ErrInvalidArgument
	}
	allowed := map[string]bool{}
	minDate := dateOnlyLocal(dates[0], s.loc)
	maxDate := minDate
	for _, date := range dates {
		day := dateOnlyLocal(date, s.loc)
		allowed[day.Format("2006-01-02")] = true
		if day.Before(minDate) {
			minDate = day
		}
		if day.After(maxDate) {
			maxDate = day
		}
	}
	return s.listFreeSlotsForServices(ctx, telegramID, serviceIndexes, maxTime(minDate, time.Now().In(s.loc)), maxDate.AddDate(0, 0, 1), allowed)
}

func (s *appStore) ListCachedAvailability(ctx context.Context, telegramID int64) ([]bot.AvailabilitySlot, error) {
	cache, err := s.loadAvailabilityForUser(ctx, telegramID)
	if err != nil {
		return nil, err
	}
	out := make([]bot.AvailabilitySlot, 0, len(cache))
	for _, item := range cache {
		start, err := time.Parse(time.RFC3339, item.Start)
		if err != nil {
			return nil, err
		}
		end, err := time.Parse(time.RFC3339, item.End)
		if err != nil {
			return nil, err
		}
		out = append(out, bot.AvailabilitySlot{
			StartAt:      start.In(s.loc),
			EndAt:        end.In(s.loc),
			ServiceNames: item.ServiceNames,
			DurationMin:  item.DurationMin,
		})
	}
	return out, nil
}

func (s *appStore) RequestMissingMonth(ctx context.Context, telegramID int64, monthStart time.Time) (bool, error) {
	from := time.Date(monthStart.In(s.loc).Year(), monthStart.In(s.loc).Month(), 1, 0, 0, 0, 0, s.loc)
	to := from.AddDate(0, 1, 0)
	hasAnySlots, err := s.hasAnyScheduleInRange(ctx, from, to)
	if err != nil {
		return false, err
	}
	if hasAnySlots {
		return false, nil
	}
	if err := s.notifyAdminsAboutMissingRequestedMonth(ctx, telegramID, from); err != nil {
		return false, err
	}
	return true, nil
}

func (s *appStore) AdminCalendar(ctx context.Context, telegramID int64, monthStart time.Time) ([]bot.CalendarDay, error) {
	actor, err := s.GetUserByTelegramID(ctx, telegramID)
	if err != nil {
		return nil, err
	}
	if !isBotAdmin(actor.Role) {
		return nil, store.ErrInvalidArgument
	}
	from := time.Date(monthStart.In(s.loc).Year(), monthStart.In(s.loc).Month(), 1, 0, 0, 0, 0, s.loc)
	to := from.AddDate(0, 1, 0)

	showAdminNames := actor.Role == bot.RoleSuperAdmin && !isSuperAdminViewingAdmin(ctx, s, telegramID)
	query := `
SELECT ` + calendarAdminSelect(showAdminNames) + ` AS admin_name,
       date_trunc('day', s.start_at AT TIME ZONE $1)::date AS day,
       COUNT(*) AS total_slots,
       COUNT(*) FILTER (
           WHERE s.status = 'open'
             AND NOT EXISTS (
                 SELECT 1 FROM bookings b
                 WHERE b.slot_id = s.id AND b.status IN ('booked', 'blocked')
             )
       ) AS open_slots,
       COUNT(*) FILTER (
           WHERE EXISTS (
               SELECT 1 FROM bookings b
               WHERE b.slot_id = s.id AND b.status = 'booked'
           )
       ) AS booked_slots,
       COUNT(*) FILTER (
           WHERE EXISTS (
               SELECT 1 FROM bookings b
               WHERE b.slot_id = s.id AND b.status = 'blocked'
           )
       ) AS blocked_slots,
       COUNT(*) FILTER (WHERE s.status = 'closed') AS closed_slots
FROM schedule_slots s
JOIN users a ON a.id = s.admin_user_id
WHERE s.start_at >= $2
  AND s.start_at < $3
`
	args := []any{s.loc.String(), from, to}
	if actor.Role == bot.RoleAdmin {
		query += "  AND a.telegram_id = $4\n"
		args = append(args, telegramID)
	} else if actor.Role == bot.RoleSuperAdmin {
		if view, err := s.GetSuperAdminView(ctx, telegramID); err == nil && view.Role == bot.RoleAdmin && strings.TrimSpace(view.AdminUsername) != "" {
			query += "  AND a.username = $4\n"
			args = append(args, normalizeUsername(view.AdminUsername))
		}
	}
	query += "GROUP BY admin_name, day ORDER BY admin_name ASC, day ASC;"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("admin calendar: %w", err)
	}
	defer rows.Close()

	var out []bot.CalendarDay
	for rows.Next() {
		var (
			adminName           string
			day                 time.Time
			total, open, booked int
			blocked, closed     int
		)
		if err := rows.Scan(&adminName, &day, &total, &open, &booked, &blocked, &closed); err != nil {
			return nil, err
		}
		out = append(out, bot.CalendarDay{
			AdminName:  adminName,
			Date:       time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, s.loc),
			OpenSlots:  open,
			Booked:     booked,
			Blocked:    blocked,
			Closed:     closed,
			TotalSlots: total,
		})
	}
	return out, rows.Err()
}

func calendarAdminSelect(showAdminNames bool) string {
	if showAdminNames {
		return "COALESCE(a.username, '')"
	}
	return "''"
}

func (s *appStore) AdminSchedule(ctx context.Context, telegramID int64, from, to time.Time) ([]bot.ScheduleGridSlot, error) {
	actor, err := s.GetUserByTelegramID(ctx, telegramID)
	if err != nil {
		return nil, err
	}
	if !isBotAdmin(actor.Role) {
		return nil, store.ErrInvalidArgument
	}
	from = dateOnlyLocal(from, s.loc)
	to = dateOnlyLocal(to, s.loc)
	if !to.After(from) {
		return nil, store.ErrInvalidArgument
	}

	showAdminNames := actor.Role == bot.RoleSuperAdmin && !isSuperAdminViewingAdmin(ctx, s, telegramID)
	query := `
SELECT ` + calendarAdminSelect(showAdminNames) + ` AS admin_name,
       s.start_at,
       s.end_at,
       s.status,
       s.capacity,
       COUNT(b.id) FILTER (WHERE b.status = 'booked') AS booked,
       COUNT(b.id) FILTER (WHERE b.status = 'blocked') AS blocked
FROM schedule_slots s
JOIN users a ON a.id = s.admin_user_id
LEFT JOIN bookings b ON b.slot_id = s.id AND b.status IN ('booked', 'blocked')
WHERE s.start_at >= $1
  AND s.start_at < $2
`
	args := []any{from, to}
	if actor.Role == bot.RoleAdmin {
		args = append(args, telegramID)
		query += fmt.Sprintf("  AND a.telegram_id = $%d\n", len(args))
	} else if actor.Role == bot.RoleSuperAdmin {
		if view, err := s.GetSuperAdminView(ctx, telegramID); err == nil && view.Role == bot.RoleAdmin && strings.TrimSpace(view.AdminUsername) != "" {
			args = append(args, normalizeUsername(view.AdminUsername))
			query += fmt.Sprintf("  AND a.username = $%d\n", len(args))
		}
	}
	query += `
GROUP BY admin_name, s.id, s.start_at, s.end_at, s.status, s.capacity
ORDER BY admin_name ASC, s.start_at ASC;
`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("admin schedule: %w", err)
	}
	defer rows.Close()

	var out []bot.ScheduleGridSlot
	for rows.Next() {
		var item bot.ScheduleGridSlot
		if err := rows.Scan(&item.AdminName, &item.StartAt, &item.EndAt, &item.Status, &item.Capacity, &item.Booked, &item.Blocked); err != nil {
			return nil, err
		}
		item.StartAt = item.StartAt.In(s.loc)
		item.EndAt = item.EndAt.In(s.loc)
		item.Available = item.Capacity - item.Booked - item.Blocked
		if item.Available < 0 {
			item.Available = 0
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *appStore) listFreeSlotsForServices(ctx context.Context, telegramID int64, serviceIndexes []int, from, to time.Time, allowedDates map[string]bool) ([]bot.AvailabilitySlot, error) {
	serviceIndexes = uniquePositiveInts(serviceIndexes)
	if len(serviceIndexes) == 0 {
		return nil, store.ErrInvalidArgument
	}
	if !from.Before(to) {
		if telegramID > 0 {
			_ = s.saveAvailabilityForUser(ctx, telegramID, nil)
		}
		return []bot.AvailabilitySlot{}, nil
	}
	serviceIDs, err := s.loadInt64sForUser(ctx, telegramID, "last_services")
	if err != nil {
		return nil, err
	}
	selectedIDs := make([]int64, 0, len(serviceIndexes))
	for _, index := range serviceIndexes {
		if index <= 0 || index > len(serviceIDs) {
			return nil, store.ErrInvalidArgument
		}
		selectedIDs = append(selectedIDs, serviceIDs[index-1])
	}
	services, err := s.servicesByIDs(ctx, selectedIDs)
	if err != nil {
		return nil, err
	}
	if len(services) != len(selectedIDs) {
		return nil, store.ErrNotFound
	}
	adminID := services[0].AdminUserID
	totalDuration := 0
	serviceNames := make([]string, 0, len(services))
	for _, service := range services {
		if service.AdminUserID != adminID {
			return nil, store.ErrInvalidArgument
		}
		totalDuration += service.DurationMin
		serviceNames = append(serviceNames, service.Name)
	}

	baseSlots, err := s.availableBaseSlots(ctx, adminID, from, to)
	if err != nil {
		return nil, err
	}

	adminName, err := s.usernameByID(ctx, adminID)
	if err != nil {
		adminName = ""
	}

	var out []bot.AvailabilitySlot
	var cache []availabilityCacheEntry
	needed := time.Duration(totalDuration) * time.Minute
	for i := range baseSlots {
		start := baseSlots[i].StartAt
		if len(allowedDates) > 0 && !allowedDates[start.In(s.loc).Format("2006-01-02")] {
			continue
		}
		current := start
		var covered []int64
		for j := i; j < len(baseSlots) && current.Sub(start) < needed; j++ {
			if !baseSlots[j].StartAt.Equal(current) {
				break
			}
			covered = append(covered, baseSlots[j].ID)
			current = baseSlots[j].EndAt
		}
		if current.Sub(start) < needed {
			continue
		}
		end := start.Add(needed)
		out = append(out, bot.AvailabilitySlot{
			StartAt:      start.In(s.loc),
			EndAt:        end.In(s.loc),
			AdminName:    adminName,
			ServiceNames: serviceNames,
			DurationMin:  totalDuration,
		})
		cache = append(cache, availabilityCacheEntry{
			Start:        start.In(s.loc).Format(time.RFC3339),
			End:          end.In(s.loc).Format(time.RFC3339),
			SlotIDs:      covered,
			ServiceIDs:   selectedIDs,
			ServiceNames: serviceNames,
			DurationMin:  totalDuration,
		})
	}
	_ = s.saveAvailabilityForUser(ctx, telegramID, cache)
	if telegramID > 0 && len(out) == 0 && isWholeMonthRange(from, to, s.loc) {
		_ = s.notifyAdminsAboutMissingRequestedMonth(ctx, telegramID, from)
	}
	return out, nil
}

func uniquePositiveInts(values []int) []int {
	seen := map[int]bool{}
	out := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func (s *appStore) ListAdminBookings(ctx context.Context, telegramID int64, from time.Time) ([]bot.BookingView, error) {
	return s.ListAdminBookingsRange(ctx, telegramID, from, time.Time{})
}

func (s *appStore) ListAdminBookingsRange(ctx context.Context, telegramID int64, from, to time.Time) ([]bot.BookingView, error) {
	actor, err := s.GetUserByTelegramID(ctx, telegramID)
	if err != nil {
		return nil, err
	}
	if !isBotAdmin(actor.Role) {
		return nil, store.ErrInvalidArgument
	}

	query := `
SELECT b.id,
       COALESCE(a.username, '') AS admin_name,
       CASE
           WHEN u.username LIKE 'phone_%' AND COALESCE(u.full_name, '') <> '' THEN u.full_name
           ELSE COALESCE(u.username, '')
       END AS client_name,
       s.start_at,
       s.end_at,
       b.status,
       b.note,
       COALESCE(svc.name, '') AS service_name
FROM bookings b
JOIN schedule_slots s ON s.id = b.slot_id
JOIN users a ON a.id = s.admin_user_id
LEFT JOIN users u ON u.id = b.user_id
LEFT JOIN admin_services svc ON svc.id = b.service_id
WHERE b.status = 'booked'
  AND b.user_id IS NOT NULL
  AND s.start_at >= $1
`
	args := []any{from.In(s.loc)}
	if !to.IsZero() {
		args = append(args, to.In(s.loc))
		query += fmt.Sprintf("  AND s.start_at < $%d\n", len(args))
	}
	if actor.Role == bot.RoleAdmin {
		args = append(args, telegramID)
		query += fmt.Sprintf("  AND a.telegram_id = $%d\n", len(args))
	} else if actor.Role == bot.RoleSuperAdmin {
		if view, err := s.GetSuperAdminView(ctx, telegramID); err == nil && view.Role == bot.RoleAdmin && strings.TrimSpace(view.AdminUsername) != "" {
			args = append(args, normalizeUsername(view.AdminUsername))
			query += fmt.Sprintf("  AND a.username = $%d\n", len(args))
		}
	}
	query += "ORDER BY s.start_at ASC, a.username ASC LIMIT 50;"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list admin bookings: %w", err)
	}
	defer rows.Close()

	var items []bot.BookingView
	for rows.Next() {
		var item bot.BookingView
		var note, serviceName string
		if err := rows.Scan(&item.ID, &item.AdminName, &item.Username, &item.StartAt, &item.EndAt, &item.Status, &note, &serviceName); err != nil {
			return nil, err
		}
		if actor.Role != bot.RoleSuperAdmin || isSuperAdminViewingAdmin(ctx, s, telegramID) {
			item.AdminName = ""
		}
		item.StartAt = item.StartAt.In(s.loc)
		item.EndAt = item.EndAt.In(s.loc)
		if duration := bookingDurationFromNote(note); duration > 0 {
			item.EndAt = item.StartAt.Add(time.Duration(duration) * time.Minute)
		}
		item.ServiceNames = bookingServiceNames(note, serviceName)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *appStore) listAdminBookingsByAdminIDRange(ctx context.Context, adminID int64, from, to time.Time) ([]bot.BookingView, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT b.id,
       CASE
           WHEN u.username LIKE 'phone_%' AND COALESCE(u.full_name, '') <> '' THEN u.full_name
           ELSE COALESCE(u.username, '')
       END AS client_name,
       s.start_at,
       s.end_at,
       b.status,
       b.note,
       COALESCE(svc.name, '') AS service_name
FROM bookings b
JOIN schedule_slots s ON s.id = b.slot_id
LEFT JOIN users u ON u.id = b.user_id
LEFT JOIN admin_services svc ON svc.id = b.service_id
WHERE b.status = 'booked'
  AND b.user_id IS NOT NULL
  AND s.admin_user_id = $1
  AND s.start_at >= $2
  AND s.start_at < $3
ORDER BY s.start_at ASC
LIMIT 50;
`, adminID, from.In(s.loc), to.In(s.loc))
	if err != nil {
		return nil, fmt.Errorf("list admin bookings by admin id: %w", err)
	}
	defer rows.Close()

	var items []bot.BookingView
	for rows.Next() {
		var item bot.BookingView
		var note, serviceName string
		if err := rows.Scan(&item.ID, &item.Username, &item.StartAt, &item.EndAt, &item.Status, &note, &serviceName); err != nil {
			return nil, err
		}
		item.StartAt = item.StartAt.In(s.loc)
		item.EndAt = item.EndAt.In(s.loc)
		if duration := bookingDurationFromNote(note); duration > 0 {
			item.EndAt = item.StartAt.Add(time.Duration(duration) * time.Minute)
		}
		item.ServiceNames = bookingServiceNames(note, serviceName)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *appStore) bookingChangeByID(ctx context.Context, bookingID, adminID int64) (bot.BookingChangeResult, error) {
	if bookingID <= 0 {
		return bot.BookingChangeResult{}, store.ErrInvalidArgument
	}
	query := `
SELECT a.telegram_id,
       COALESCE(al.value, 'ru') AS admin_language,
       CASE
           WHEN u.username LIKE 'phone_%' AND COALESCE(u.full_name, '') <> '' THEN u.full_name
           ELSE COALESCE(u.username, '')
       END AS client_name,
       s.start_at,
       s.end_at,
       b.note,
       COALESCE(svc.name, '') AS service_name
FROM bookings b
JOIN schedule_slots s ON s.id = b.slot_id
JOIN users a ON a.id = s.admin_user_id
LEFT JOIN users u ON u.id = b.user_id
LEFT JOIN admin_services svc ON svc.id = b.service_id
LEFT JOIN admin_settings al ON al.admin_user_id = a.id AND al.key = 'language'
WHERE b.id = $1
  AND b.status = 'booked'
`
	args := []any{bookingID}
	if adminID > 0 {
		args = append(args, adminID)
		query += fmt.Sprintf("  AND s.admin_user_id = $%d\n", len(args))
	}
	query += "LIMIT 1;"

	var (
		result      bot.BookingChangeResult
		adminChatID sql.NullInt64
		note        string
		serviceName string
	)
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&adminChatID,
		&result.AdminLanguage,
		&result.Username,
		&result.StartAt,
		&result.EndAt,
		&note,
		&serviceName,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return bot.BookingChangeResult{}, store.ErrNotFound
	}
	if err != nil {
		return bot.BookingChangeResult{}, err
	}
	if adminChatID.Valid {
		result.AdminChatID = adminChatID.Int64
	}
	result.StartAt = result.StartAt.In(s.loc)
	result.EndAt = result.EndAt.In(s.loc)
	if duration := bookingDurationFromNote(note); duration > 0 {
		result.EndAt = result.StartAt.Add(time.Duration(duration) * time.Minute)
	}
	result.ServiceNames = bookingServiceNames(note, serviceName)
	return result, nil
}

func (s *appStore) ListMyBookings(ctx context.Context, telegramID int64, from time.Time) ([]bot.BookingView, error) {
	userID, err := s.userIDByTelegram(ctx, telegramID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT b.id, COALESCE(a.username, ''), s.start_at, s.end_at, b.status, b.note
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
		var note string
		if err := rows.Scan(&item.ID, &item.AdminName, &item.StartAt, &item.EndAt, &item.Status, &note); err != nil {
			return nil, err
		}
		item.StartAt = item.StartAt.In(s.loc)
		item.EndAt = item.EndAt.In(s.loc)
		if duration := bookingDurationFromNote(note); duration > 0 {
			item.EndAt = item.StartAt.Add(time.Duration(duration) * time.Minute)
		}
		items = append(items, item)
		times = append(times, item.StartAt)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_ = s.saveTimesForUser(ctx, telegramID, "last_my_bookings", times)
	return items, nil
}

func (s *appStore) DeleteMyBookingByID(ctx context.Context, telegramID int64, bookingID int64) (bot.BookingChangeResult, error) {
	if bookingID <= 0 {
		return bot.BookingChangeResult{}, store.ErrInvalidArgument
	}
	userID, err := s.userIDByTelegram(ctx, telegramID)
	if err != nil {
		return bot.BookingChangeResult{}, err
	}
	var exists bool
	if err := s.db.QueryRowContext(ctx, `
SELECT EXISTS (
	SELECT 1
	FROM bookings b
	JOIN schedule_slots s ON s.id = b.slot_id
	WHERE b.id = $1
	  AND b.user_id = $2
	  AND b.status = 'booked'
	  AND s.start_at >= $3
);
`, bookingID, userID, time.Now().In(s.loc)).Scan(&exists); err != nil {
		return bot.BookingChangeResult{}, err
	}
	if !exists {
		return bot.BookingChangeResult{}, store.ErrNotFound
	}
	result, err := s.bookingChangeByID(ctx, bookingID, 0)
	if err != nil {
		return bot.BookingChangeResult{}, err
	}
	if err := s.repo.DeleteBooking(ctx, bookingID, "cancelled_by_user"); err != nil {
		return bot.BookingChangeResult{}, err
	}
	_, err = s.db.ExecContext(ctx, `
UPDATE bookings
SET status = 'cancelled', cancelled_at = NOW(), updated_at = NOW(), note = note || ';cancelled_by_user'
WHERE status = 'blocked' AND note = $1;
`, coveredByBookingNote(bookingID))
	if err != nil {
		return bot.BookingChangeResult{}, err
	}
	return result, nil
}

func (s *appStore) ListMoveTargetsForBooking(ctx context.Context, telegramID int64, bookingID int64, from, to time.Time) ([]bot.AvailabilitySlot, error) {
	if bookingID <= 0 || !to.After(from) {
		return nil, store.ErrInvalidArgument
	}
	userID, err := s.userIDByTelegram(ctx, telegramID)
	if err != nil {
		return nil, err
	}

	var (
		adminID     int64
		startAt     time.Time
		endAt       time.Time
		note        string
		serviceID   sql.NullInt64
		serviceName string
		adminName   string
	)
	err = s.db.QueryRowContext(ctx, `
SELECT s.admin_user_id, s.start_at, s.end_at, b.note, b.service_id, COALESCE(svc.name, ''), COALESCE(a.username, '')
FROM bookings b
JOIN schedule_slots s ON s.id = b.slot_id
JOIN users a ON a.id = s.admin_user_id
LEFT JOIN admin_services svc ON svc.id = b.service_id
WHERE b.id = $1
  AND b.user_id = $2
  AND b.status = 'booked'
  AND s.start_at >= $3
LIMIT 1;
`, bookingID, userID, time.Now().In(s.loc)).Scan(&adminID, &startAt, &endAt, &note, &serviceID, &serviceName, &adminName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	durationMin := bookingDurationFromNote(note)
	if durationMin <= 0 {
		durationMin = int(endAt.Sub(startAt) / time.Minute)
	}
	if durationMin <= 0 {
		return nil, store.ErrInvalidArgument
	}
	serviceNames := bookingServiceNames(note, serviceName)
	var serviceIDs []int64
	var parsed bookingServiceNote
	if err := json.Unmarshal([]byte(note), &parsed); err == nil && len(parsed.ServiceIDs) > 0 {
		serviceIDs = parsed.ServiceIDs
	} else if serviceID.Valid {
		serviceIDs = []int64{serviceID.Int64}
	}

	baseSlots, err := s.availableBaseSlots(ctx, adminID, maxTime(from.In(s.loc), time.Now().In(s.loc)), to.In(s.loc))
	if err != nil {
		return nil, err
	}
	needed := time.Duration(durationMin) * time.Minute
	var out []bot.AvailabilitySlot
	var cache []availabilityCacheEntry
	for i := range baseSlots {
		start := baseSlots[i].StartAt
		current := start
		var covered []int64
		for j := i; j < len(baseSlots) && current.Sub(start) < needed; j++ {
			if !baseSlots[j].StartAt.Equal(current) {
				break
			}
			covered = append(covered, baseSlots[j].ID)
			current = baseSlots[j].EndAt
		}
		if current.Sub(start) < needed {
			continue
		}
		end := start.Add(needed)
		out = append(out, bot.AvailabilitySlot{
			StartAt:      start.In(s.loc),
			EndAt:        end.In(s.loc),
			AdminName:    adminName,
			ServiceNames: serviceNames,
			DurationMin:  durationMin,
		})
		cache = append(cache, availabilityCacheEntry{
			Start:        start.In(s.loc).Format(time.RFC3339),
			End:          end.In(s.loc).Format(time.RFC3339),
			SlotIDs:      covered,
			ServiceIDs:   serviceIDs,
			ServiceNames: serviceNames,
			DurationMin:  durationMin,
		})
	}
	_ = s.saveAvailabilityForUserKey(ctx, telegramID, s.moveAvailabilityKey(bookingID), cache)
	return out, nil
}

func (s *appStore) MoveMyBookingByIDToIndex(ctx context.Context, telegramID int64, bookingID int64, slotIndex int) (bot.MoveResult, error) {
	availability, err := s.loadAvailabilityForUserKey(ctx, telegramID, s.moveAvailabilityKey(bookingID))
	if err != nil {
		return bot.MoveResult{}, err
	}
	if slotIndex <= 0 || slotIndex > len(availability) {
		return bot.MoveResult{}, store.ErrInvalidArgument
	}
	return s.moveBookingForUserIDToAvailability(ctx, telegramID, bookingID, availability[slotIndex-1])
}

func (s *appStore) BookForUser(ctx context.Context, telegramID int64, start time.Time) (bot.BookingChangeResult, error) {
	userID, err := s.userIDByTelegram(ctx, telegramID)
	if err != nil {
		return bot.BookingChangeResult{}, err
	}
	slotID, err := s.availableSlotIDByStart(ctx, start)
	if err != nil {
		return bot.BookingChangeResult{}, err
	}
	booking, err := s.repo.CreateBooking(ctx, domain.Booking{
		SlotID:        slotID,
		UserID:        &userID,
		Status:        domain.BookingStatusBooked,
		TravelMinutes: 0,
		Note:          "created_by_user",
	})
	if err != nil {
		return bot.BookingChangeResult{}, err
	}
	return s.bookingChangeByID(ctx, booking.ID, 0)
}

func (s *appStore) BookForUserByIndex(ctx context.Context, telegramID int64, index int) (bot.BookingChangeResult, error) {
	availability, err := s.loadAvailabilityForUser(ctx, telegramID)
	if err == nil && len(availability) > 0 {
		if index <= 0 || index > len(availability) {
			return bot.BookingChangeResult{}, store.ErrInvalidArgument
		}
		return s.bookAvailability(ctx, telegramID, availability[index-1])
	}

	slots, err := s.loadTimesForUser(ctx, telegramID, "last_free_slots")
	if err != nil {
		return bot.BookingChangeResult{}, err
	}
	if index <= 0 || index > len(slots) {
		return bot.BookingChangeResult{}, store.ErrInvalidArgument
	}
	start := slots[index-1]
	return s.BookForUser(ctx, telegramID, start)
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
	availability, availabilityErr := s.loadAvailabilityForUser(ctx, telegramID)
	if availabilityErr == nil && len(availability) > 0 {
		if bookingIndex <= 0 || bookingIndex > len(bookings) || slotIndex <= 0 || slotIndex > len(availability) {
			return bot.MoveResult{}, store.ErrInvalidArgument
		}
		return s.moveBookingForUserToAvailability(ctx, telegramID, bookings[bookingIndex-1], availability[slotIndex-1])
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
SELECT b.id, s.start_at, u.telegram_id, u.username, a.telegram_id,
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
			startAt       time.Time
			userChatID    sql.NullInt64
			username      sql.NullString
			adminChatID   sql.NullInt64
			userLanguage  string
			adminLanguage string
		)
		if err := rows.Scan(&bookingID, &startAt, &userChatID, &username, &adminChatID, &userLanguage, &adminLanguage); err != nil {
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
			if err := s.upsertReminder(ctx, bookingID, userChatID.Int64, "hour_before", "user", startAt.Add(-time.Hour), userReminderHourBefore(userLanguage, startAt)); err != nil {
				return err
			}
		}
		if adminChatID.Valid {
			if err := s.upsertReminder(ctx, bookingID, adminChatID.Int64, "day_before", "admin", startAt.Add(-24*time.Hour), adminReminderDayBefore(adminLanguage, userLabel, startAt)); err != nil {
				return err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := s.prepareAdminScheduleReminders(ctx, now); err != nil {
		return err
	}
	return s.prepareDailyAdminBookingSummaries(ctx, now)
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

func (s *appStore) upsertSystemReminder(ctx context.Context, dedupeKey string, chatID int64, kind, recipientRole string, sendAt time.Time, payload string) error {
	if strings.TrimSpace(dedupeKey) == "" || chatID <= 0 {
		return store.ErrInvalidArgument
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO reminders (booking_id, dedupe_key, chat_id, kind, recipient_role, send_at, channel, payload)
VALUES (NULL, $1, $2, $3, $4, $5, 'telegram', $6)
ON CONFLICT(dedupe_key, kind, recipient_role, chat_id) WHERE dedupe_key <> '' DO UPDATE SET
	send_at = EXCLUDED.send_at,
	payload = EXCLUDED.payload;
`, dedupeKey, chatID, kind, recipientRole, sendAt, payload)
	return err
}

func (s *appStore) prepareAdminScheduleReminders(ctx context.Context, now time.Time) error {
	now = now.In(s.loc)
	if now.Day() != 15 {
		return nil
	}

	nextMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, s.loc).AddDate(0, 1, 0)
	nextMonthEnd := nextMonth.AddDate(0, 1, 0)
	sendAt := time.Date(now.Year(), now.Month(), 15, 10, 0, 0, 0, s.loc)
	if now.After(sendAt) {
		sendAt = now
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT u.id, u.telegram_id, COALESCE(l.value, 'ru') AS language
FROM users u
LEFT JOIN admin_settings l ON l.admin_user_id = u.id AND l.key = 'language'
WHERE u.role IN ('admin', 'super_admin')
  AND u.telegram_id IS NOT NULL;
`)
	if err != nil {
		return fmt.Errorf("select admins for schedule reminders: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			adminID  int64
			chatID   int64
			language string
		)
		if err := rows.Scan(&adminID, &chatID, &language); err != nil {
			return err
		}
		hasSlots, err := s.adminHasScheduleInRange(ctx, adminID, nextMonth, nextMonthEnd)
		if err != nil {
			return err
		}
		if hasSlots {
			continue
		}
		month := nextMonth.Format("2006-01")
		dedupeKey := fmt.Sprintf("admin_month_missing:%d:%s", adminID, month)
		if err := s.upsertSystemReminder(ctx, dedupeKey, chatID, "admin_month_missing", "admin", sendAt, adminMonthMissingReminder(language, month)); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *appStore) prepareDailyAdminBookingSummaries(ctx context.Context, now time.Time) error {
	now = now.In(s.loc)
	day := dateOnlyLocal(now, s.loc)
	sendAt := time.Date(day.Year(), day.Month(), day.Day(), 8, 0, 0, 0, s.loc)
	if now.Before(sendAt) {
		return nil
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT u.id, u.telegram_id, COALESCE(l.value, 'ru') AS language
FROM users u
LEFT JOIN admin_settings l ON l.admin_user_id = u.id AND l.key = 'language'
WHERE u.role IN ('admin', 'super_admin')
  AND u.telegram_id IS NOT NULL;
`)
	if err != nil {
		return fmt.Errorf("select admins for daily booking summaries: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			adminID  int64
			chatID   int64
			language string
		)
		if err := rows.Scan(&adminID, &chatID, &language); err != nil {
			return err
		}
		items, err := s.listAdminBookingsByAdminIDRange(ctx, adminID, day, day.AddDate(0, 0, 1))
		if err != nil {
			return err
		}
		if len(items) == 0 {
			continue
		}
		dedupeKey := fmt.Sprintf("admin_daily_bookings:%d:%s", adminID, day.Format("2006-01-02"))
		payload := adminDailyBookingsReminder(language, day, items)
		if err := s.upsertSystemReminder(ctx, dedupeKey, chatID, "admin_daily_bookings", "admin", sendAt, payload); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *appStore) notifyAdminsAboutMissingRequestedMonth(ctx context.Context, requesterTelegramID int64, monthStart time.Time) error {
	from := time.Date(monthStart.In(s.loc).Year(), monthStart.In(s.loc).Month(), 1, 0, 0, 0, 0, s.loc)
	to := from.AddDate(0, 1, 0)
	hasAnySlots, err := s.hasAnyScheduleInRange(ctx, from, to)
	if err != nil {
		return err
	}
	if hasAnySlots {
		return nil
	}

	requesterLabel := "@client"
	if requester, err := s.lookupUser(ctx, "telegram_id = $1", requesterTelegramID); err == nil && requester.Username != "" {
		requesterLabel = "@" + requester.Username
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT u.id, u.telegram_id, COALESCE(l.value, 'ru') AS language
FROM users u
LEFT JOIN admin_settings l ON l.admin_user_id = u.id AND l.key = 'language'
WHERE u.role IN ('admin', 'super_admin')
  AND u.telegram_id IS NOT NULL;
`)
	if err != nil {
		return fmt.Errorf("select admins for missing month notice: %w", err)
	}
	defer rows.Close()

	month := from.Format("2006-01")
	for rows.Next() {
		var (
			adminID  int64
			chatID   int64
			language string
		)
		if err := rows.Scan(&adminID, &chatID, &language); err != nil {
			return err
		}
		dedupeKey := fmt.Sprintf("client_requested_missing_month:%d:%s", adminID, month)
		payload := adminMonthRequestedNotice(language, requesterLabel, month)
		if err := s.upsertSystemReminder(ctx, dedupeKey, chatID, "client_requested_missing_month", "admin", time.Now().In(s.loc), payload); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *appStore) adminHasScheduleInRange(ctx context.Context, adminID int64, from, to time.Time) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `
SELECT EXISTS (
	SELECT 1
	FROM schedule_slots
	WHERE admin_user_id = $1
	  AND start_at >= $2
	  AND start_at < $3
);
`, adminID, from, to).Scan(&exists)
	return exists, err
}

func (s *appStore) hasAnyScheduleInRange(ctx context.Context, from, to time.Time) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `
SELECT EXISTS (
	SELECT 1
	FROM schedule_slots
	WHERE start_at >= $1
	  AND start_at < $2
);
`, from, to).Scan(&exists)
	return exists, err
}

func isWholeMonthRange(from, to time.Time, loc *time.Location) bool {
	start := from.In(loc)
	monthStart := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, loc)
	return start.Equal(monthStart) && to.In(loc).Equal(monthStart.AddDate(0, 1, 0))
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
		Username:   u.Username,
		FirstName:  firstName,
		LastName:   lastName,
		Role:       bot.Role(u.Role),
		ActualRole: bot.Role(u.Role),
	}
	if u.TelegramID != nil {
		rec.TelegramID = *u.TelegramID
	}
	if lang, err := s.stringSetting(ctx, u.ID, "language", ""); err == nil && lang != "" {
		rec.Language = lang
		rec.LanguageSet = true
	}
	if rec.Language == "" {
		rec.Language = bot.LangRU
	}
	return rec, nil
}

func (s *appStore) effectiveAdminIDByTelegram(ctx context.Context, telegramID int64) (int64, error) {
	actor, err := s.GetUserByTelegramID(ctx, telegramID)
	if err != nil {
		return 0, err
	}
	if actor.ActualRole == bot.RoleSuperAdmin {
		if view, err := s.GetSuperAdminView(ctx, telegramID); err == nil && view.Role == bot.RoleAdmin && strings.TrimSpace(view.AdminUsername) != "" {
			target, err := s.lookupUser(ctx, "username = $1", normalizeUsername(view.AdminUsername))
			if err != nil {
				return 0, err
			}
			if target.Role != domain.RoleAdmin && target.Role != domain.RoleSuperAdmin {
				return 0, store.ErrInvalidArgument
			}
			return target.ID, nil
		}
	}
	return s.userIDByTelegram(ctx, telegramID)
}

func (s *appStore) userIDByTelegram(ctx context.Context, telegramID int64) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, "SELECT id FROM users WHERE telegram_id = $1", telegramID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, store.ErrNotFound
	}
	return id, err
}

func (s *appStore) userIDByUsername(ctx context.Context, username string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
SELECT id
FROM users
WHERE username = $1;
`, normalizeUsername(username)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, store.ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (s *appStore) targetAdminIDForServiceScope(ctx context.Context, telegramID int64) (int64, bool, error) {
	actor, err := s.GetUserByTelegramID(ctx, telegramID)
	if err != nil {
		return 0, false, err
	}
	if actor.Role == bot.RoleAdmin {
		id, err := s.userIDByTelegram(ctx, telegramID)
		return id, err == nil, err
	}
	if actor.Role == bot.RoleSuperAdmin {
		view, err := s.GetSuperAdminView(ctx, telegramID)
		if err == nil && view.Role == bot.RoleAdmin && strings.TrimSpace(view.AdminUsername) != "" {
			id, err := s.userIDByUsername(ctx, view.AdminUsername)
			return id, err == nil, err
		}
	}
	return 0, false, nil
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

func (s *appStore) ensureUserByPhone(ctx context.Context, phone string) (int64, error) {
	normalized := normalizePhoneNumber(phone)
	if normalized == "" {
		return 0, store.ErrInvalidArgument
	}
	username := "phone_" + phoneDigits(normalized)
	var id int64
	err := s.db.QueryRowContext(ctx, `
INSERT INTO users (username, full_name, role)
VALUES ($1, $2, 'user')
ON CONFLICT(username) DO UPDATE SET
	full_name = EXCLUDED.full_name,
	updated_at = NOW()
RETURNING id;
`, username, normalized).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("ensure user by phone: %w", err)
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
	adminID, err := s.effectiveAdminIDByTelegram(ctx, adminTelegramID)
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

func (s *appStore) categoryOrder(ctx context.Context, adminUserID int64) ([]string, error) {
	raw, err := s.stringSetting(ctx, adminUserID, "category_order", "")
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func sortServicesByCategoryOrder(services []bot.ServiceView, order []string) {
	rank := make(map[string]int, len(order))
	for i, category := range order {
		rank[category] = i
	}
	sort.SliceStable(services, func(i, j int) bool {
		left, leftOK := rank[services[i].Category]
		right, rightOK := rank[services[j].Category]
		if leftOK && rightOK && left != right {
			return left < right
		}
		if leftOK != rightOK {
			return leftOK
		}
		return false
	})
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

type baseSlot struct {
	ID      int64
	StartAt time.Time
	EndAt   time.Time
}

type availabilityCacheEntry struct {
	Start        string   `json:"start"`
	End          string   `json:"end"`
	SlotIDs      []int64  `json:"slot_ids"`
	ServiceIDs   []int64  `json:"service_ids"`
	ServiceNames []string `json:"service_names"`
	DurationMin  int      `json:"duration_min"`
}

type bookingServiceNote struct {
	ServiceIDs   []int64  `json:"service_ids"`
	ServiceNames []string `json:"service_names"`
	DurationMin  int      `json:"duration_min"`
}

func (s *appStore) saveInt64sForUser(ctx context.Context, telegramID int64, key string, values []int64) error {
	userID, err := s.userIDByTelegram(ctx, telegramID)
	if err != nil {
		return err
	}
	data, err := json.Marshal(values)
	if err != nil {
		return err
	}
	return s.repo.SetAdminSetting(ctx, userID, key, string(data))
}

func (s *appStore) loadInt64sForUser(ctx context.Context, telegramID int64, key string) ([]int64, error) {
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
	var out []int64
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *appStore) saveAvailabilityForUser(ctx context.Context, telegramID int64, values []availabilityCacheEntry) error {
	return s.saveAvailabilityForUserKey(ctx, telegramID, "last_availability_slots", values)
}

func (s *appStore) loadAvailabilityForUser(ctx context.Context, telegramID int64) ([]availabilityCacheEntry, error) {
	return s.loadAvailabilityForUserKey(ctx, telegramID, "last_availability_slots")
}

func (s *appStore) moveAvailabilityKey(bookingID int64) string {
	return fmt.Sprintf("last_move_availability:%d", bookingID)
}

func (s *appStore) saveAvailabilityForUserKey(ctx context.Context, telegramID int64, key string, values []availabilityCacheEntry) error {
	userID, err := s.userIDByTelegram(ctx, telegramID)
	if err != nil {
		return err
	}
	data, err := json.Marshal(values)
	if err != nil {
		return err
	}
	return s.repo.SetAdminSetting(ctx, userID, key, string(data))
}

func (s *appStore) loadAvailabilityForUserKey(ctx context.Context, telegramID int64, key string) ([]availabilityCacheEntry, error) {
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
	var out []availabilityCacheEntry
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *appStore) GetConversationState(ctx context.Context, telegramID int64) (bot.ConversationState, error) {
	userID, err := s.userIDByTelegram(ctx, telegramID)
	if err != nil {
		return bot.ConversationState{}, err
	}
	raw, err := s.stringSetting(ctx, userID, "conversation_state", "")
	if err != nil {
		return bot.ConversationState{}, err
	}
	if raw == "" {
		return bot.ConversationState{}, store.ErrNotFound
	}
	var state bot.ConversationState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return bot.ConversationState{}, err
	}
	return state, nil
}

func (s *appStore) SetConversationState(ctx context.Context, telegramID int64, state bot.ConversationState) error {
	userID, err := s.userIDByTelegram(ctx, telegramID)
	if err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return s.repo.SetAdminSetting(ctx, userID, "conversation_state", string(data))
}

func (s *appStore) ClearConversationState(ctx context.Context, telegramID int64) error {
	return s.clearSettingForUser(ctx, telegramID, "conversation_state")
}

func (s *appStore) clearSettingForUser(ctx context.Context, telegramID int64, key string) error {
	userID, err := s.userIDByTelegram(ctx, telegramID)
	if err != nil {
		return err
	}
	return s.clearSettingForAdminID(ctx, userID, key)
}

func (s *appStore) clearSettingForAdminID(ctx context.Context, adminID int64, key string) error {
	_, err := s.db.ExecContext(ctx, `
DELETE FROM admin_settings
WHERE admin_user_id = $1 AND key = $2;
`, adminID, key)
	return err
}

func (s *appStore) clearScheduleCacheForAdminID(ctx context.Context, adminID int64) error {
	if adminID <= 0 {
		return store.ErrInvalidArgument
	}
	_, err := s.db.ExecContext(ctx, `
DELETE FROM admin_settings
WHERE key IN ('last_free_slots', 'last_availability_slots')
   OR key LIKE 'last_move_availability:%';
`)
	return err
}

func (s *appStore) servicesByIDs(ctx context.Context, ids []int64) ([]domain.AdminService, error) {
	if len(ids) == 0 {
		return nil, store.ErrInvalidArgument
	}
	placeholders := make([]string, 0, len(ids))
	args := make([]any, 0, len(ids))
	for i, id := range ids {
		if id <= 0 {
			return nil, store.ErrInvalidArgument
		}
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, admin_user_id, category, subcategory, name, description, duration_min, price_cents, is_active, created_at, updated_at
FROM admin_services
WHERE is_active = TRUE AND id IN (`+strings.Join(placeholders, ",")+`)
ORDER BY array_position(ARRAY[`+strings.Join(placeholders, ",")+`]::bigint[], id);
`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.AdminService
	for rows.Next() {
		var service domain.AdminService
		if err := rows.Scan(&service.ID, &service.AdminUserID, &service.Category, &service.Subcategory, &service.Name, &service.Description, &service.DurationMin, &service.PriceCents, &service.IsActive, &service.CreatedAt, &service.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, service)
	}
	return out, rows.Err()
}

func (s *appStore) availableBaseSlots(ctx context.Context, adminID int64, from, to time.Time) ([]baseSlot, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT s.id, s.start_at, s.end_at
FROM schedule_slots s
LEFT JOIN bookings b ON b.slot_id = s.id AND b.status IN ('booked', 'blocked')
WHERE s.admin_user_id = $1
  AND s.status = 'open'
  AND s.start_at >= $2
  AND s.start_at < $3
GROUP BY s.id
HAVING COUNT(b.id) < s.capacity
ORDER BY s.start_at ASC;
`, adminID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []baseSlot
	for rows.Next() {
		var slot baseSlot
		if err := rows.Scan(&slot.ID, &slot.StartAt, &slot.EndAt); err != nil {
			return nil, err
		}
		slot.StartAt = slot.StartAt.In(s.loc)
		slot.EndAt = slot.EndAt.In(s.loc)
		out = append(out, slot)
	}
	return out, rows.Err()
}

func (s *appStore) usernameByID(ctx context.Context, userID int64) (string, error) {
	var username string
	err := s.db.QueryRowContext(ctx, `SELECT username FROM users WHERE id = $1`, userID).Scan(&username)
	return username, err
}

func (s *appStore) bookAvailability(ctx context.Context, telegramID int64, entry availabilityCacheEntry) (bot.BookingChangeResult, error) {
	if len(entry.SlotIDs) == 0 {
		return bot.BookingChangeResult{}, store.ErrInvalidArgument
	}
	userID, err := s.userIDByTelegram(ctx, telegramID)
	if err != nil {
		return bot.BookingChangeResult{}, err
	}
	return s.bookAvailabilityForUserID(ctx, userID, entry, 0)
}

func (s *appStore) bookAvailabilityForUserID(ctx context.Context, userID int64, entry availabilityCacheEntry, adminID int64) (bot.BookingChangeResult, error) {
	if userID <= 0 || len(entry.SlotIDs) == 0 {
		return bot.BookingChangeResult{}, store.ErrInvalidArgument
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return bot.BookingChangeResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	for _, slotID := range entry.SlotIDs {
		var capacity int
		err := tx.QueryRowContext(ctx, `
SELECT capacity
FROM schedule_slots
WHERE id = $1 AND status = 'open'
FOR UPDATE;
`, slotID).Scan(&capacity)
		if errors.Is(err, sql.ErrNoRows) {
			return bot.BookingChangeResult{}, store.ErrSlotUnavailable
		}
		if err != nil {
			return bot.BookingChangeResult{}, err
		}
		var booked int
		if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM bookings
WHERE slot_id = $1 AND status IN ('booked', 'blocked');
`, slotID).Scan(&booked); err != nil {
			return bot.BookingChangeResult{}, err
		}
		if booked >= capacity {
			return bot.BookingChangeResult{}, store.ErrSlotUnavailable
		}
	}

	var serviceID any
	if len(entry.ServiceIDs) > 0 {
		serviceID = entry.ServiceIDs[0]
	}
	note, err := json.Marshal(bookingServiceNote{
		ServiceIDs:   entry.ServiceIDs,
		ServiceNames: entry.ServiceNames,
		DurationMin:  entry.DurationMin,
	})
	if err != nil {
		return bot.BookingChangeResult{}, err
	}

	var bookingID int64
	if err := tx.QueryRowContext(ctx, `
INSERT INTO bookings (slot_id, user_id, service_id, status, travel_minutes, note)
VALUES ($1, $2, $3, 'booked', $4, $5)
RETURNING id;
`, entry.SlotIDs[0], userID, serviceID, 0, string(note)).Scan(&bookingID); err != nil {
		return bot.BookingChangeResult{}, err
	}
	for _, slotID := range entry.SlotIDs[1:] {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO bookings (slot_id, user_id, service_id, status, travel_minutes, note)
VALUES ($1, NULL, NULL, 'blocked', $2, $3);
`, slotID, 0, coveredByBookingNote(bookingID)); err != nil {
			return bot.BookingChangeResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return bot.BookingChangeResult{}, err
	}
	return s.bookingChangeByID(ctx, bookingID, adminID)
}

func (s *appStore) moveBookingForUserToAvailability(ctx context.Context, telegramID int64, fromStart time.Time, entry availabilityCacheEntry) (bot.MoveResult, error) {
	if len(entry.SlotIDs) == 0 {
		return bot.MoveResult{}, store.ErrInvalidArgument
	}
	userID, err := s.userIDByTelegram(ctx, telegramID)
	if err != nil {
		return bot.MoveResult{}, err
	}
	toStart, err := time.Parse(time.RFC3339, entry.Start)
	if err != nil {
		return bot.MoveResult{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return bot.MoveResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		oldBookingID   int64
		adminID        int64
		adminChatID    sql.NullInt64
		clientUsername string
	)
	err = tx.QueryRowContext(ctx, `
SELECT b.id, s.admin_user_id, a.telegram_id, u.username
FROM bookings b
JOIN schedule_slots s ON s.id = b.slot_id
JOIN users u ON u.id = b.user_id
JOIN users a ON a.id = s.admin_user_id
WHERE b.user_id = $1
  AND s.start_at = $2
  AND b.status = 'booked'
FOR UPDATE;
`, userID, fromStart.In(s.loc)).Scan(&oldBookingID, &adminID, &adminChatID, &clientUsername)
	if errors.Is(err, sql.ErrNoRows) {
		return bot.MoveResult{}, store.ErrNotFound
	}
	if err != nil {
		return bot.MoveResult{}, err
	}

	for _, slotID := range entry.SlotIDs {
		var capacity int
		err := tx.QueryRowContext(ctx, `
SELECT capacity
FROM schedule_slots
WHERE id = $1 AND admin_user_id = $2 AND status = 'open'
FOR UPDATE;
`, slotID, adminID).Scan(&capacity)
		if errors.Is(err, sql.ErrNoRows) {
			return bot.MoveResult{}, store.ErrSlotUnavailable
		}
		if err != nil {
			return bot.MoveResult{}, err
		}
		var booked int
		if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM bookings
WHERE slot_id = $1 AND status IN ('booked', 'blocked');
`, slotID).Scan(&booked); err != nil {
			return bot.MoveResult{}, err
		}
		if booked >= capacity {
			return bot.MoveResult{}, store.ErrSlotUnavailable
		}
	}

	if _, err := tx.ExecContext(ctx, `
UPDATE bookings
SET status = 'cancelled', cancelled_at = NOW(), updated_at = NOW(), note = note || ';moved_by_user'
WHERE id = $1;
`, oldBookingID); err != nil {
		return bot.MoveResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE bookings
SET status = 'cancelled', cancelled_at = NOW(), updated_at = NOW(), note = note || ';moved_by_user'
WHERE status = 'blocked' AND note = $1;
`, coveredByBookingNote(oldBookingID)); err != nil {
		return bot.MoveResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE reminders
SET sent_at = NOW()
WHERE booking_id = $1 AND sent_at IS NULL;
`, oldBookingID); err != nil {
		return bot.MoveResult{}, err
	}

	var serviceID any
	if len(entry.ServiceIDs) > 0 {
		serviceID = entry.ServiceIDs[0]
	}
	note, err := json.Marshal(bookingServiceNote{
		ServiceIDs:   entry.ServiceIDs,
		ServiceNames: entry.ServiceNames,
		DurationMin:  entry.DurationMin,
	})
	if err != nil {
		return bot.MoveResult{}, err
	}
	var newBookingID int64
	if err := tx.QueryRowContext(ctx, `
INSERT INTO bookings (slot_id, user_id, service_id, status, travel_minutes, note)
VALUES ($1, $2, $3, 'booked', $4, $5)
RETURNING id;
`, entry.SlotIDs[0], userID, serviceID, 0, string(note)).Scan(&newBookingID); err != nil {
		return bot.MoveResult{}, err
	}
	for _, slotID := range entry.SlotIDs[1:] {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO bookings (slot_id, user_id, service_id, status, travel_minutes, note)
VALUES ($1, NULL, NULL, 'blocked', $2, $3);
`, slotID, 0, coveredByBookingNote(newBookingID)); err != nil {
			return bot.MoveResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
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

func (s *appStore) moveBookingForUserIDToAvailability(ctx context.Context, telegramID int64, bookingID int64, entry availabilityCacheEntry) (bot.MoveResult, error) {
	if bookingID <= 0 || len(entry.SlotIDs) == 0 {
		return bot.MoveResult{}, store.ErrInvalidArgument
	}
	userID, err := s.userIDByTelegram(ctx, telegramID)
	if err != nil {
		return bot.MoveResult{}, err
	}
	toStart, err := time.Parse(time.RFC3339, entry.Start)
	if err != nil {
		return bot.MoveResult{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return bot.MoveResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		adminID        int64
		adminChatID    sql.NullInt64
		clientUsername string
		fromStart      time.Time
	)
	err = tx.QueryRowContext(ctx, `
SELECT s.admin_user_id, a.telegram_id, u.username, s.start_at
FROM bookings b
JOIN schedule_slots s ON s.id = b.slot_id
JOIN users u ON u.id = b.user_id
JOIN users a ON a.id = s.admin_user_id
WHERE b.id = $1
  AND b.user_id = $2
  AND b.status = 'booked'
FOR UPDATE;
`, bookingID, userID).Scan(&adminID, &adminChatID, &clientUsername, &fromStart)
	if errors.Is(err, sql.ErrNoRows) {
		return bot.MoveResult{}, store.ErrNotFound
	}
	if err != nil {
		return bot.MoveResult{}, err
	}

	for _, slotID := range entry.SlotIDs {
		var capacity int
		err := tx.QueryRowContext(ctx, `
SELECT capacity
FROM schedule_slots
WHERE id = $1 AND admin_user_id = $2 AND status = 'open'
FOR UPDATE;
`, slotID, adminID).Scan(&capacity)
		if errors.Is(err, sql.ErrNoRows) {
			return bot.MoveResult{}, store.ErrSlotUnavailable
		}
		if err != nil {
			return bot.MoveResult{}, err
		}
		var booked int
		if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM bookings
WHERE slot_id = $1 AND status IN ('booked', 'blocked');
`, slotID).Scan(&booked); err != nil {
			return bot.MoveResult{}, err
		}
		if booked >= capacity {
			return bot.MoveResult{}, store.ErrSlotUnavailable
		}
	}

	if _, err := tx.ExecContext(ctx, `
UPDATE bookings
SET status = 'cancelled', cancelled_at = NOW(), updated_at = NOW(), note = note || ';moved_by_user'
WHERE id = $1;
`, bookingID); err != nil {
		return bot.MoveResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE bookings
SET status = 'cancelled', cancelled_at = NOW(), updated_at = NOW(), note = note || ';moved_by_user'
WHERE status = 'blocked' AND note = $1;
`, coveredByBookingNote(bookingID)); err != nil {
		return bot.MoveResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE reminders
SET sent_at = NOW()
WHERE booking_id = $1 AND sent_at IS NULL;
`, bookingID); err != nil {
		return bot.MoveResult{}, err
	}

	var serviceID any
	if len(entry.ServiceIDs) > 0 {
		serviceID = entry.ServiceIDs[0]
	}
	note, err := json.Marshal(bookingServiceNote{
		ServiceIDs:   entry.ServiceIDs,
		ServiceNames: entry.ServiceNames,
		DurationMin:  entry.DurationMin,
	})
	if err != nil {
		return bot.MoveResult{}, err
	}
	var newBookingID int64
	if err := tx.QueryRowContext(ctx, `
INSERT INTO bookings (slot_id, user_id, service_id, status, travel_minutes, note)
VALUES ($1, $2, $3, 'booked', $4, $5)
RETURNING id;
`, entry.SlotIDs[0], userID, serviceID, 0, string(note)).Scan(&newBookingID); err != nil {
		return bot.MoveResult{}, err
	}
	for _, slotID := range entry.SlotIDs[1:] {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO bookings (slot_id, user_id, service_id, status, travel_minutes, note)
VALUES ($1, NULL, NULL, 'blocked', $2, $3);
`, slotID, 0, coveredByBookingNote(newBookingID)); err != nil {
			return bot.MoveResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
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

func coveredByBookingNote(bookingID int64) string {
	return fmt.Sprintf("covered_by_booking:%d", bookingID)
}

func dateOnlyLocal(value time.Time, loc *time.Location) time.Time {
	value = value.In(loc)
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, loc)
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func bookingDurationFromNote(note string) int {
	var parsed bookingServiceNote
	if err := json.Unmarshal([]byte(note), &parsed); err != nil {
		return 0
	}
	return parsed.DurationMin
}

func bookingServiceNames(note, fallback string) []string {
	var parsed bookingServiceNote
	if err := json.Unmarshal([]byte(note), &parsed); err == nil && len(parsed.ServiceNames) > 0 {
		return parsed.ServiceNames
	}
	fallback = strings.TrimSpace(fallback)
	if fallback == "" {
		return nil
	}
	return []string{fallback}
}

func normalizeUsername(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "@")
	return strings.ToLower(value)
}

func normalizePhoneNumber(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var digits strings.Builder
	for i, r := range value {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
			continue
		}
		if r == '+' && i == 0 {
			continue
		}
		if r == ' ' || r == '-' || r == '(' || r == ')' {
			continue
		}
		return ""
	}
	raw := digits.String()
	if len(raw) < 5 {
		return ""
	}
	if strings.HasPrefix(value, "+") {
		return "+" + raw
	}
	return raw
}

func phoneDigits(value string) string {
	var out strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func validBotRole(role bot.Role) bool {
	return role == bot.RoleUser || role == bot.RoleAdmin || role == bot.RoleSuperAdmin
}

func isBotAdmin(role bot.Role) bool {
	return role == bot.RoleAdmin || role == bot.RoleSuperAdmin
}

func isSuperAdminViewingAdmin(ctx context.Context, s *appStore, telegramID int64) bool {
	view, err := s.GetSuperAdminView(ctx, telegramID)
	return err == nil && view.Role == bot.RoleAdmin && strings.TrimSpace(view.AdminUsername) != ""
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

func parseServicePath(raw string) (string, string, string) {
	parts := splitServicePath(raw)
	switch len(parts) {
	case 0:
		return "", "", ""
	case 1:
		return "", "", parts[0]
	case 2:
		return parts[0], "", parts[1]
	default:
		return parts[0], parts[1], strings.Join(parts[2:], " > ")
	}
}

func splitServicePath(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var sep string
	switch {
	case strings.Contains(raw, ">"):
		sep = ">"
	case strings.Contains(raw, "|"):
		sep = "|"
	default:
		return []string{raw}
	}
	parts := strings.Split(raw, sep)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func userReminderDayBefore(language string, startAt time.Time) string {
	if language == bot.LangEN {
		return fmt.Sprintf("Reminder: you have a booking on %s.", startAt.Format("02.01.2006 15:04"))
	}
	return fmt.Sprintf("Напоминание: у вас запись %s.", startAt.Format("02.01.2006 15:04"))
}

func userReminderHourBefore(language string, startAt time.Time) string {
	if language == bot.LangEN {
		return fmt.Sprintf("Reminder: your booking starts in 1 hour, at %s.", startAt.Format("15:04"))
	}
	return fmt.Sprintf("Напоминание: запись начнется через 1 час, в %s.", startAt.Format("15:04"))
}

func adminReminderDayBefore(language, userLabel string, startAt time.Time) string {
	if language == bot.LangEN {
		return fmt.Sprintf("Reminder: tomorrow client %s has a booking at %s.", userLabel, startAt.Format("02.01.2006 15:04"))
	}
	return fmt.Sprintf("Напоминание: завтра запись у клиента %s в %s.", userLabel, startAt.Format("02.01.2006 15:04"))
}

func adminDailyBookingsReminder(language string, day time.Time, items []bot.BookingView) string {
	var sb strings.Builder
	if language == bot.LangEN {
		sb.WriteString(fmt.Sprintf("Today's bookings, %s:\n", day.Format("02.01.2006")))
	} else {
		sb.WriteString(fmt.Sprintf("Записи на сегодня, %s:\n", day.Format("02.01.2006")))
	}
	for i, item := range items {
		sb.WriteString(fmt.Sprintf("%d. %s", i+1, item.StartAt.Format("15:04")))
		if !item.EndAt.IsZero() {
			sb.WriteString("-")
			sb.WriteString(item.EndAt.Format("15:04"))
		}
		sb.WriteString(" - ")
		if strings.TrimSpace(item.Username) == "" {
			if language == bot.LangEN {
				sb.WriteString("client")
			} else {
				sb.WriteString("клиент")
			}
		} else {
			sb.WriteString(formatReminderClientContact(item.Username))
		}
		if len(item.ServiceNames) > 0 {
			sb.WriteString(" - ")
			sb.WriteString(strings.Join(item.ServiceNames, ", "))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func formatReminderClientContact(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	first := value[0]
	if first == '+' || first >= '0' && first <= '9' {
		return value
	}
	return "@" + value
}

func adminMonthMissingReminder(language, month string) string {
	if language == bot.LangEN {
		return fmt.Sprintf("Schedule for %s is not filled yet. Clients will not be able to book that month. Use /generate %s or /generate %s 2 to fill two months ahead.", month, month, month)
	}
	return fmt.Sprintf("Расписание на %s еще не заполнено. Клиенты не смогут записаться на этот месяц. Используйте /generate %s или /generate %s 2, чтобы заполнить два месяца вперед.", month, month, month)
}

func adminMonthRequestedNotice(language, clientLabel, month string) string {
	if language == bot.LangEN {
		return fmt.Sprintf("Client %s requested booking for %s, but that month has no schedule. Generate it with /generate %s.", clientLabel, month, month)
	}
	return fmt.Sprintf("Клиент %s запросил запись на %s, но на этот месяц нет расписания. Создайте его командой /generate %s.", clientLabel, month, month)
}
