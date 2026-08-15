package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"time-table-bot/internal/bot"
	"time-table-bot/internal/domain"
	"time-table-bot/internal/store"
)

var (
	serviceMoneyRE         = regexp.MustCompile(`(?i)(\d{1,6}(?:[.,]\d{1,2})?)\s*(?:€|eur|евро)`)
	serviceTrailingMoneyRE = regexp.MustCompile(`(?:^|\s)(\d{2,4}(?:[.,]\d{1,2})?)\s*$`)
)

func (s *appStore) AddFinanceEntry(ctx context.Context, adminTelegramID int64, entry bot.FinanceEntryInput) error {
	adminID, ok, err := s.targetAdminIDForServiceScope(ctx, adminTelegramID)
	if err != nil {
		return err
	}
	if !ok || entry.AmountCents <= 0 || entry.OccurredAt.IsZero() {
		return store.ErrInvalidArgument
	}
	entry.Kind = strings.ToLower(strings.TrimSpace(entry.Kind))
	if entry.Kind != "income" && entry.Kind != "expense" {
		return store.ErrInvalidArgument
	}
	entry.Currency = strings.ToUpper(strings.TrimSpace(entry.Currency))
	if entry.Currency == "" {
		entry.Currency = "EUR"
	}
	if entry.Currency != "EUR" {
		return store.ErrInvalidArgument
	}
	entry.Source = strings.TrimSpace(entry.Source)
	if entry.Source == "" {
		entry.Source = "text"
	}
	var bookingID any
	if entry.BookingID > 0 {
		bookingID = entry.BookingID
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO admin_finance_entries
    (admin_user_id, booking_id, kind, category, amount_cents, currency, occurred_at, description, source)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT(admin_user_id, booking_id) WHERE booking_id IS NOT NULL AND source = 'booking_override'
DO UPDATE SET
    kind = EXCLUDED.kind,
    category = EXCLUDED.category,
    amount_cents = EXCLUDED.amount_cents,
    currency = EXCLUDED.currency,
    occurred_at = EXCLUDED.occurred_at,
    description = EXCLUDED.description,
    updated_at = NOW();
`, adminID, bookingID, entry.Kind, strings.TrimSpace(entry.Category), entry.AmountCents,
		entry.Currency, entry.OccurredAt.In(s.loc), strings.TrimSpace(entry.Description), entry.Source)
	if err != nil {
		return fmt.Errorf("add finance entry: %w", err)
	}
	return nil
}

func (s *appStore) FinanceReport(ctx context.Context, adminTelegramID int64, from, to time.Time, period string) (bot.FinanceReport, error) {
	adminID, ok, err := s.targetAdminIDForServiceScope(ctx, adminTelegramID)
	if err != nil {
		return bot.FinanceReport{}, err
	}
	if !ok || from.IsZero() || !to.After(from) {
		return bot.FinanceReport{}, store.ErrInvalidArgument
	}
	from, to = from.In(s.loc), to.In(s.loc)
	report := bot.FinanceReport{
		From: from, To: to, Currency: "EUR", ExpenseCategories: map[string]int64{},
		Buckets: financeBuckets(from, to, period),
	}
	overrides, err := s.loadFinanceEntries(ctx, adminID, from, to, &report)
	if err != nil {
		return bot.FinanceReport{}, err
	}
	services, err := s.allAdminServicesByID(ctx, adminID)
	if err != nil {
		return bot.FinanceReport{}, err
	}
	earnedTo := to
	if now := time.Now().In(s.loc); now.Before(earnedTo) {
		earnedTo = now
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT b.id, sl.start_at,
       CASE
           WHEN (u.username LIKE 'phone_%' OR u.username LIKE 'name_%') AND COALESCE(u.full_name, '') <> '' THEN u.full_name
           ELSE COALESCE(u.username, '')
       END,
       b.note, b.service_id, COALESCE(svc.name, '')
FROM bookings b
JOIN schedule_slots sl ON sl.id = b.slot_id
LEFT JOIN users u ON u.id = b.user_id
LEFT JOIN admin_services svc ON svc.id = b.service_id
WHERE sl.admin_user_id = $1
  AND b.status = 'booked'
  AND b.user_id IS NOT NULL
  AND sl.start_at >= $2
  AND sl.start_at < $3
ORDER BY sl.start_at;
`, adminID, from, earnedTo)
	if err != nil {
		return bot.FinanceReport{}, fmt.Errorf("finance bookings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			bookingID, serviceID   int64
			startAt                time.Time
			client, note, fallback string
			serviceIDNull          sql.NullInt64
		)
		if err := rows.Scan(&bookingID, &startAt, &client, &note, &serviceIDNull, &fallback); err != nil {
			return bot.FinanceReport{}, err
		}
		startAt = startAt.In(s.loc)
		if override, exists := overrides[bookingID]; exists {
			report.BookingIncomeCents += override.AmountCents
			addFinanceBucket(report.Buckets, override.OccurredAt, override.AmountCents, 0)
			continue
		}
		var parsed bookingServiceNote
		_ = json.Unmarshal([]byte(note), &parsed)
		serviceIDs := parsed.ServiceIDs
		if len(serviceIDs) == 0 && serviceIDNull.Valid {
			serviceID = serviceIDNull.Int64
			serviceIDs = []int64{serviceID}
		}
		serviceNames := parsed.ServiceNames
		if len(serviceNames) == 0 && strings.TrimSpace(fallback) != "" {
			serviceNames = []string{fallback}
		}
		amount, reason := bookingFinanceAmount(serviceIDs, services)
		if reason != "" {
			report.Unresolved = append(report.Unresolved, bot.FinanceUnresolved{
				BookingID: bookingID, StartAt: startAt, Client: client,
				ServiceNames: serviceNames, Reason: reason,
			})
			continue
		}
		report.BookingIncomeCents += amount
		addFinanceBucket(report.Buckets, startAt, amount, 0)
	}
	if err := rows.Err(); err != nil {
		return bot.FinanceReport{}, err
	}
	return report, nil
}

type storedFinanceEntry struct {
	BookingID   int64
	AmountCents int64
	OccurredAt  time.Time
}

func (s *appStore) loadFinanceEntries(ctx context.Context, adminID int64, from, to time.Time, report *bot.FinanceReport) (map[int64]storedFinanceEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT COALESCE(booking_id, 0), kind, category, amount_cents, currency, occurred_at, source
FROM admin_finance_entries
WHERE admin_user_id = $1 AND occurred_at >= $2 AND occurred_at < $3
ORDER BY occurred_at;
`, adminID, from, to)
	if err != nil {
		return nil, fmt.Errorf("finance entries: %w", err)
	}
	defer rows.Close()
	overrides := map[int64]storedFinanceEntry{}
	for rows.Next() {
		var bookingID, amount int64
		var kind, category, currency, source string
		var occurredAt time.Time
		if err := rows.Scan(&bookingID, &kind, &category, &amount, &currency, &occurredAt, &source); err != nil {
			return nil, err
		}
		occurredAt = occurredAt.In(s.loc)
		if source == "booking_override" && bookingID > 0 {
			overrides[bookingID] = storedFinanceEntry{BookingID: bookingID, AmountCents: amount, OccurredAt: occurredAt}
			continue
		}
		if currency != "EUR" {
			continue
		}
		if kind == "income" {
			report.ManualIncomeCents += amount
			addFinanceBucket(report.Buckets, occurredAt, amount, 0)
		} else if kind == "expense" {
			report.ExpenseCents += amount
			category = strings.TrimSpace(category)
			if category == "" {
				category = "other"
			}
			report.ExpenseCategories[category] += amount
			addFinanceBucket(report.Buckets, occurredAt, 0, amount)
		}
	}
	return overrides, rows.Err()
}

func (s *appStore) allAdminServicesByID(ctx context.Context, adminID int64) (map[int64]domain.AdminService, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, admin_user_id, category, subcategory, name, description, duration_min, price_cents, is_active, created_at, updated_at
FROM admin_services WHERE admin_user_id = $1;
`, adminID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]domain.AdminService{}
	for rows.Next() {
		var service domain.AdminService
		if err := rows.Scan(&service.ID, &service.AdminUserID, &service.Category, &service.Subcategory,
			&service.Name, &service.Description, &service.DurationMin, &service.PriceCents,
			&service.IsActive, &service.CreatedAt, &service.UpdatedAt); err != nil {
			return nil, err
		}
		out[service.ID] = service
	}
	return out, rows.Err()
}

func bookingFinanceAmount(serviceIDs []int64, services map[int64]domain.AdminService) (int64, string) {
	if len(serviceIDs) == 0 {
		return 0, "service_missing"
	}
	var total int64
	seen := map[int64]bool{}
	for _, id := range serviceIDs {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		service, ok := services[id]
		if !ok {
			return 0, "service_missing"
		}
		amount, reason := serviceFinancePrice(service)
		if reason != "" {
			return 0, reason
		}
		total += amount
	}
	if total <= 0 {
		return 0, "price_missing"
	}
	return total, ""
}

func serviceFinancePrice(service domain.AdminService) (int64, string) {
	if service.PriceCents > 0 {
		return service.PriceCents, ""
	}
	fullName := strings.ToLower(strings.Join([]string{service.Category, service.Subcategory, service.Name}, " "))
	for _, marker := range []string{"курс", "абонемент", "package", "bundle"} {
		if strings.Contains(fullName, marker) {
			return 0, "package_price"
		}
	}
	for _, value := range []string{service.Description, service.Name} {
		matches := serviceMoneyRE.FindAllStringSubmatch(value, -1)
		if len(matches) == 1 {
			amount, ok := decimalMoneyToCents(matches[0][1])
			if ok {
				return amount, ""
			}
		}
		if len(matches) > 1 {
			return 0, "price_ambiguous"
		}
	}
	if match := serviceTrailingMoneyRE.FindStringSubmatch(strings.TrimSpace(service.Description)); len(match) == 2 {
		if amount, ok := decimalMoneyToCents(match[1]); ok {
			return amount, ""
		}
	}
	if match := serviceTrailingMoneyRE.FindStringSubmatch(strings.TrimSpace(service.Name)); len(match) == 2 {
		if amount, ok := decimalMoneyToCents(match[1]); ok {
			return amount, ""
		}
	}
	return 0, "price_missing"
}

func decimalMoneyToCents(value string) (int64, bool) {
	value = strings.ReplaceAll(strings.TrimSpace(value), ",", ".")
	parts := strings.Split(value, ".")
	if len(parts) > 2 || len(parts) == 0 {
		return 0, false
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole < 0 {
		return 0, false
	}
	cents := int64(0)
	if len(parts) == 2 {
		fraction := parts[1]
		if len(fraction) == 1 {
			fraction += "0"
		}
		if len(fraction) != 2 {
			return 0, false
		}
		cents, err = strconv.ParseInt(fraction, 10, 64)
		if err != nil {
			return 0, false
		}
	}
	amount := whole*100 + cents
	return amount, amount > 0
}

func financeBuckets(from, to time.Time, period string) []bot.FinanceBucket {
	out := make([]bot.FinanceBucket, 0, 31)
	if period == "month" || to.Sub(from) <= 32*24*time.Hour {
		for day := from; day.Before(to); day = day.AddDate(0, 0, 1) {
			out = append(out, bot.FinanceBucket{StartAt: day, Label: day.Format("02")})
		}
		return out
	}
	for month := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, from.Location()); month.Before(to); month = month.AddDate(0, 1, 0) {
		out = append(out, bot.FinanceBucket{StartAt: month, Label: month.Format("01.06")})
	}
	return out
}

func addFinanceBucket(buckets []bot.FinanceBucket, at time.Time, income, expense int64) {
	for i := range buckets {
		start := buckets[i].StartAt
		end := start.AddDate(0, 0, 1)
		if len(buckets) <= 12 && start.Day() == 1 && (i+1 == len(buckets) || buckets[i+1].StartAt.Month() != start.Month()) {
			end = start.AddDate(0, 1, 0)
		}
		if !at.Before(start) && at.Before(end) {
			buckets[i].IncomeCents += income
			buckets[i].ExpenseCents += expense
			return
		}
	}
}

func (s *appStore) prepareMonthlyFinanceReminders(ctx context.Context, now time.Time) error {
	now = now.In(s.loc)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, s.loc)
	monthEndDay := monthStart.AddDate(0, 1, -1)
	if now.Day() < monthEndDay.Day()-2 {
		return nil
	}
	sendAt := time.Date(monthEndDay.Year(), monthEndDay.Month(), monthEndDay.Day(), 18, 0, 0, 0, s.loc)
	if now.After(sendAt) {
		sendAt = now
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT u.telegram_id, COALESCE(lang.value, 'ru')
FROM users u
LEFT JOIN admin_settings lang ON lang.admin_user_id = u.id AND lang.key = 'language'
WHERE u.role IN ('admin', 'super_admin') AND u.telegram_id IS NOT NULL;
`)
	if err != nil {
		return fmt.Errorf("finance reminder admins: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var chatID int64
		var language string
		if err := rows.Scan(&chatID, &language); err != nil {
			return err
		}
		payload := "Пора закрыть месяц. Откройте «Финансы» → «Этот месяц», уточните записи без цены и добавьте аренду и расходники."
		if language == bot.LangEN {
			payload = "It is time to close the month. Open Finance → This month, clarify bookings without a price, and add rent and supplies."
		}
		key := fmt.Sprintf("finance-close:%s", monthStart.Format("2006-01"))
		if err := s.upsertSystemReminder(ctx, key, chatID, "finance_month_close", "admin", sendAt, payload); err != nil {
			return err
		}
	}
	return rows.Err()
}
