package bot

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"time-table-bot/internal/nlu"
	"time-table-bot/internal/telegram"
)

const (
	financeMinConfidence = 0.5
	financeMaxTextRunes  = 10000
)

var (
	financeAmountRE = regexp.MustCompile(`(?i)(\d{1,7}(?:[.,]\d{1,2})?)\s*(?:€|eur|евро)?`)
	financeYearRE   = regexp.MustCompile(`\b(20\d{2})\b`)
)

type evaluatedFinanceEntry struct {
	Draft      FinanceEntryDraft
	OccurredAt time.Time
	Ready      bool
	Issue      string
}

func looksLikeFinanceInput(text, source, forcedKind string) bool {
	if forcedKind == "income" || forcedKind == "expense" {
		return true
	}
	normalized := normalizeMatchText(text)
	strongMarkers := []string{
		"расход", "трата", "потрат", "аренда", "расходник", "закуп", "чек", "итого", "к оплате",
		"доход", "заработ", "выручка", "expense", "income", "revenue", "rent", "supplies", "total",
	}
	for _, marker := range strongMarkers {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	for _, marker := range []string{"купила", "купил", "получила", "получил оплат"} {
		if strings.Contains(normalized, marker) && (financeAmountRE.MatchString(text) || containsAnyPhrase(normalized, []string{"евро", "eur"})) {
			return true
		}
	}
	return source == "image" && financeAmountRE.MatchString(text) &&
		(strings.Contains(normalized, "оплат") || strings.Contains(normalized, "total") || strings.Contains(normalized, "итого"))
}

func (b *Bot) handleAdminFinanceInput(ctx context.Context, chatID int64, user UserRecord, text, source, forcedKind string) (bool, error) {
	parser, ok := b.adminBookingParser.(nlu.AdminFinanceIntentParser)
	if !ok || !isAdmin(user.Role) || !looksLikeFinanceInput(text, source, forcedKind) {
		return false, nil
	}
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > financeMaxTextRunes {
		return true, b.sendText(ctx, chatID, tr(user.Language, "finance_parse_failed"))
	}
	if err := b.sendText(ctx, chatID, tr(user.Language, "finance_processing")); err != nil {
		return true, err
	}
	now := time.Now().In(time.Local)
	intent, err := parser.ParseAdminFinanceIntent(ctx, nlu.AdminFinanceIntentRequest{
		Text: text, Language: user.Language, Now: now, Timezone: now.Location().String(),
		Source: source, ForcedKind: forcedKind,
	})
	if err != nil {
		b.logger.Printf("finance: parser failed admin=%d: %v", user.TelegramID, err)
		return true, b.sendText(ctx, chatID, tr(user.Language, "finance_parse_failed"))
	}
	if !intent.IsFinance || intent.Confidence < financeMinConfidence || len(intent.Entries) == 0 {
		return true, b.sendText(ctx, chatID, tr(user.Language, "finance_no_entries"))
	}
	entries := make([]FinanceEntryDraft, 0, len(intent.Entries))
	for _, entry := range intent.Entries {
		kind := entry.Kind
		if forcedKind != "" {
			kind = forcedKind
		}
		entries = append(entries, FinanceEntryDraft{
			Kind: kind, Category: entry.Category, AmountCents: entry.AmountCents,
			Currency: entry.Currency, OccurredAt: entry.OccurredAt,
			Description: entry.Description, Source: source, Confidence: entry.Confidence,
		})
	}
	state, _ := b.store.GetConversationState(ctx, user.TelegramID)
	state.Step = conversationStepFinanceConfirm
	state.FinanceEntries = entries
	state.FinanceForcedKind = ""
	return true, b.showFinanceEntryPreview(ctx, chatID, user, state)
}

func (b *Bot) showFinanceEntryPreview(ctx context.Context, chatID int64, user UserRecord, state ConversationState) error {
	items := evaluateFinanceEntries(user.Language, state.FinanceEntries, time.Now().In(time.Local))
	state.Step = conversationStepFinanceConfirm
	state.FinanceEntries = make([]FinanceEntryDraft, 0, len(items))
	ready := 0
	for _, item := range items {
		item.Draft.OccurredAt = item.OccurredAt.Format(time.RFC3339)
		state.FinanceEntries = append(state.FinanceEntries, item.Draft)
		if item.Ready {
			ready++
		}
	}
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	return b.sendTextWithKeyboard(ctx, chatID, formatFinanceEntryPreview(user.Language, items), financeEntryKeyboard(user.Language, ready > 0))
}

func evaluateFinanceEntries(lang string, drafts []FinanceEntryDraft, now time.Time) []evaluatedFinanceEntry {
	out := make([]evaluatedFinanceEntry, 0, len(drafts))
	for _, draft := range drafts {
		draft.Kind = strings.ToLower(strings.TrimSpace(draft.Kind))
		draft.Category = strings.TrimSpace(draft.Category)
		draft.Currency = strings.ToUpper(strings.TrimSpace(draft.Currency))
		if draft.Currency == "" {
			draft.Currency = "EUR"
		}
		draft.Description = strings.TrimSpace(draft.Description)
		item := evaluatedFinanceEntry{Draft: draft, OccurredAt: now}
		if parsed, err := time.Parse(time.RFC3339, draft.OccurredAt); err == nil {
			item.OccurredAt = parsed.In(now.Location())
		}
		switch {
		case draft.Kind != "income" && draft.Kind != "expense":
			item.Issue = tr(lang, "finance_issue_kind")
		case draft.AmountCents <= 0:
			item.Issue = tr(lang, "finance_issue_amount")
		case draft.Currency != "EUR":
			item.Issue = tr(lang, "finance_issue_currency")
		case draft.Confidence > 0 && draft.Confidence < financeMinConfidence:
			item.Issue = tr(lang, "finance_issue_uncertain")
		default:
			item.Ready = true
		}
		out = append(out, item)
	}
	return out
}

func (b *Bot) confirmFinanceEntries(ctx context.Context, chatID int64, user UserRecord, state ConversationState) error {
	items := evaluateFinanceEntries(user.Language, state.FinanceEntries, time.Now().In(time.Local))
	added, skipped := 0, 0
	for _, item := range items {
		if !item.Ready {
			skipped++
			continue
		}
		if err := b.store.AddFinanceEntry(ctx, user.TelegramID, FinanceEntryInput{
			BookingID: item.Draft.BookingID, Kind: item.Draft.Kind, Category: item.Draft.Category,
			AmountCents: item.Draft.AmountCents, Currency: item.Draft.Currency,
			OccurredAt: item.OccurredAt, Description: item.Draft.Description, Source: item.Draft.Source,
		}); err != nil {
			b.logger.Printf("finance: save failed admin=%d: %v", user.TelegramID, err)
			skipped++
			continue
		}
		added++
	}
	period := state.FinanceReportPeriod
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	if err := b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "finance_saved", added, skipped), financeMenuKeyboard(user.Language)); err != nil {
		return err
	}
	if period != "" && added > 0 {
		return b.sendFinanceReport(ctx, chatID, user, period)
	}
	return nil
}

func (b *Bot) handleFinanceEntryCallback(ctx context.Context, cb *telegram.CallbackQuery) error {
	user, err := b.userFromCallback(ctx, cb)
	if err != nil {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(LangRU, "register_failed"))
	}
	if !isAdmin(user.Role) {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(user.Language, "admin_only"))
	}
	state, err := b.store.GetConversationState(ctx, user.TelegramID)
	if err != nil || state.Step != conversationStepFinanceConfirm {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(user.Language, "finance_expired"))
	}
	if strings.TrimPrefix(cb.Data, "financeentry:") == "yes" {
		return b.confirmFinanceEntries(ctx, cb.Message.Chat.ID, user, state)
	}
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	return b.sendTextWithKeyboard(ctx, cb.Message.Chat.ID, tr(user.Language, "finance_cancelled"), financeMenuKeyboard(user.Language))
}

func (b *Bot) beginFinanceInput(ctx context.Context, chatID int64, user UserRecord, kind, period string) error {
	if !isAdmin(user.Role) {
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	state := ConversationState{Step: conversationStepFinanceInput, FinanceForcedKind: kind, FinanceReportPeriod: period}
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	key := "finance_ask_income"
	if kind == "expense" {
		key = "finance_ask_expense"
	}
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, key), financeInputKeyboard(user.Language))
}

func (b *Bot) conversationFinanceInput(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	handled, err := b.handleAdminFinanceInput(ctx, chatID, user, text, "text", state.FinanceForcedKind)
	if handled {
		if err == nil {
			latest, getErr := b.store.GetConversationState(ctx, user.TelegramID)
			if getErr == nil {
				latest.FinanceReportPeriod = state.FinanceReportPeriod
				_ = b.store.SetConversationState(ctx, user.TelegramID, latest)
			}
		}
		return err
	}
	return b.sendText(ctx, chatID, tr(user.Language, "finance_parse_failed"))
}

func (b *Bot) conversationFinanceResolve(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	amount, ok := parseFinanceAmount(text)
	if !ok {
		return b.sendText(ctx, chatID, tr(user.Language, "finance_resolve_ask"))
	}
	report, err := b.financeReportForPeriod(ctx, user, state.FinanceReportPeriod)
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "finance_report_failed"))
	}
	var unresolved *FinanceUnresolved
	for i := range report.Unresolved {
		if report.Unresolved[i].BookingID == state.FinanceResolveBooking {
			unresolved = &report.Unresolved[i]
			break
		}
	}
	if unresolved == nil {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "finance_resolve_missing"))
	}
	description := strings.Join(unresolved.ServiceNames, ", ")
	state.Step = conversationStepFinanceConfirm
	state.FinanceEntries = []FinanceEntryDraft{{
		BookingID: unresolved.BookingID, Kind: "income", Category: "services",
		AmountCents: amount, Currency: "EUR", OccurredAt: unresolved.StartAt.Format(time.RFC3339),
		Description: description, Source: "booking_override", Confidence: 1,
	}}
	return b.showFinanceEntryPreview(ctx, chatID, user, state)
}

func (b *Bot) handleAdminFinanceReportRequest(ctx context.Context, chatID int64, user UserRecord, text string) (bool, error) {
	if !isAdmin(user.Role) {
		return false, nil
	}
	period, ok := financeReportPeriodFromText(text, time.Now().In(time.Local))
	if !ok {
		return false, nil
	}
	return true, b.sendFinanceReport(ctx, chatID, user, period)
}

func (b *Bot) handleAdminFinanceChartRequest(ctx context.Context, chatID int64, user UserRecord, text string) (bool, error) {
	if !isAdmin(user.Role) {
		return false, nil
	}
	period, ok := financeChartPeriodFromText(text, time.Now().In(time.Local))
	if !ok {
		return false, nil
	}
	return true, b.sendFinanceChart(ctx, chatID, user, period)
}

func financeChartPeriodFromText(text string, now time.Time) (string, bool) {
	normalized := normalizeMatchText(text)
	if !containsAnyPhrase(normalized, []string{"график", "chart"}) ||
		!containsAnyPhrase(normalized, []string{"финанс", "доход", "расход", "заработ", "выруч", "income", "expense", "revenue"}) {
		return "", false
	}
	period, ok := financeReportPeriodFromText("отчет "+text, now)
	if !ok {
		period = "month"
	}
	return period, true
}

func (b *Bot) sendFinanceReport(ctx context.Context, chatID int64, user UserRecord, period string) error {
	if !isAdmin(user.Role) {
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	report, err := b.financeReportForPeriod(ctx, user, period)
	if err != nil {
		b.logger.Printf("finance report failed admin=%d period=%s: %v", user.TelegramID, period, err)
		return b.sendText(ctx, chatID, tr(user.Language, "finance_report_failed"))
	}
	return b.sendTextWithKeyboard(ctx, chatID, formatFinanceReport(user.Language, report), financeReportKeyboard(user.Language, report.Unresolved, period))
}

func (b *Bot) sendFinanceChart(ctx context.Context, chatID int64, user UserRecord, period string) error {
	if !isAdmin(user.Role) {
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	report, err := b.financeReportForPeriod(ctx, user, period)
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "finance_report_failed"))
	}
	image, err := renderFinanceChart(user.Language, report)
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "finance_chart_failed"))
	}
	return b.sendPhoto(ctx, chatID, image, financeChartCaption(user.Language, report), financeReportKeyboard(user.Language, report.Unresolved, period))
}

func (b *Bot) financeReportForPeriod(ctx context.Context, user UserRecord, period string) (FinanceReport, error) {
	from, to, bucketPeriod, ok := financeRangeForPeriod(period, time.Now().In(time.Local))
	if !ok {
		return FinanceReport{}, fmt.Errorf("invalid finance period %q", period)
	}
	return b.store.FinanceReport(ctx, user.TelegramID, from, to, bucketPeriod)
}

func (b *Bot) handleFinanceCallback(ctx context.Context, cb *telegram.CallbackQuery) error {
	user, err := b.userFromCallback(ctx, cb)
	if err != nil {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(LangRU, "register_failed"))
	}
	if !isAdmin(user.Role) {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(user.Language, "admin_only"))
	}
	data := strings.TrimPrefix(cb.Data, "finance:")
	switch {
	case strings.HasPrefix(data, "report:"):
		return b.sendFinanceReport(ctx, cb.Message.Chat.ID, user, strings.TrimPrefix(data, "report:"))
	case strings.HasPrefix(data, "chart:"):
		return b.sendFinanceChart(ctx, cb.Message.Chat.ID, user, strings.TrimPrefix(data, "chart:"))
	case strings.HasPrefix(data, "addincome:"):
		return b.beginFinanceInput(ctx, cb.Message.Chat.ID, user, "income", strings.TrimPrefix(data, "addincome:"))
	case strings.HasPrefix(data, "addexpense:"):
		return b.beginFinanceInput(ctx, cb.Message.Chat.ID, user, "expense", strings.TrimPrefix(data, "addexpense:"))
	case strings.HasPrefix(data, "resolve:"):
		parts := strings.SplitN(strings.TrimPrefix(data, "resolve:"), ":", 2)
		if len(parts) != 2 {
			return b.sendText(ctx, cb.Message.Chat.ID, tr(user.Language, "finance_resolve_missing"))
		}
		bookingID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || bookingID <= 0 {
			return b.sendText(ctx, cb.Message.Chat.ID, tr(user.Language, "finance_resolve_missing"))
		}
		state := ConversationState{Step: conversationStepFinanceResolve, FinanceResolveBooking: bookingID, FinanceReportPeriod: parts[1]}
		if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
			return b.sendText(ctx, cb.Message.Chat.ID, tr(user.Language, "conversation_failed"))
		}
		return b.sendText(ctx, cb.Message.Chat.ID, tr(user.Language, "finance_resolve_ask"))
	default:
		return b.sendTextWithKeyboard(ctx, cb.Message.Chat.ID, tr(user.Language, "menu_finance_text"), financeMenuKeyboard(user.Language))
	}
}

func formatFinanceEntryPreview(lang string, items []evaluatedFinanceEntry) string {
	var sb strings.Builder
	sb.WriteString(tr(lang, "finance_preview"))
	for _, item := range items {
		prefix := "+ "
		if !item.Ready {
			prefix = "! "
		}
		kind := tr(lang, "finance_kind_"+item.Draft.Kind)
		line := fmt.Sprintf("\n%s%s — %s — %s", prefix, item.OccurredAt.Format("02.01.2006"), kind, formatMoney(item.Draft.AmountCents, item.Draft.Currency))
		if item.Draft.Category != "" {
			line += " — " + item.Draft.Category
		}
		if item.Draft.Description != "" {
			line += " — " + item.Draft.Description
		}
		if item.Issue != "" {
			line += ": " + item.Issue
		}
		if utf8.RuneCountInString(sb.String()+line) > 3600 {
			sb.WriteString("\n…")
			break
		}
		sb.WriteString(line)
	}
	sb.WriteString("\n\n")
	sb.WriteString(tr(lang, "finance_preview_footer"))
	return sb.String()
}

func formatFinanceReport(lang string, report FinanceReport) string {
	income := report.BookingIncomeCents + report.ManualIncomeCents
	net := income - report.ExpenseCents
	var sb strings.Builder
	sb.WriteString(tr(lang, "finance_report_header", report.From.Format("02.01.2006"), report.To.Add(-time.Nanosecond).Format("02.01.2006")))
	sb.WriteString("\n")
	sb.WriteString(tr(lang, "finance_report_booking_income", formatMoney(report.BookingIncomeCents, report.Currency)))
	sb.WriteString("\n")
	sb.WriteString(tr(lang, "finance_report_manual_income", formatMoney(report.ManualIncomeCents, report.Currency)))
	sb.WriteString("\n")
	sb.WriteString(tr(lang, "finance_report_income", formatMoney(income, report.Currency)))
	sb.WriteString("\n")
	sb.WriteString(tr(lang, "finance_report_expenses", formatMoney(report.ExpenseCents, report.Currency)))
	if len(report.ExpenseCategories) > 0 {
		categories := make([]string, 0, len(report.ExpenseCategories))
		for category := range report.ExpenseCategories {
			categories = append(categories, category)
		}
		sort.Strings(categories)
		for _, category := range categories {
			sb.WriteString(fmt.Sprintf("\n  • %s: %s", financeCategoryLabel(lang, category), formatMoney(report.ExpenseCategories[category], report.Currency)))
		}
	}
	sb.WriteString("\n")
	sb.WriteString(tr(lang, "finance_report_net", formatMoney(net, report.Currency)))
	if len(report.Unresolved) > 0 {
		sb.WriteString("\n\n")
		sb.WriteString(tr(lang, "finance_report_unresolved", len(report.Unresolved)))
		for i, item := range report.Unresolved {
			if i >= 12 {
				sb.WriteString("\n…")
				break
			}
			client := strings.TrimSpace(item.Client)
			if client == "" {
				client = tr(lang, "schedule_import_unknown_client")
			}
			sb.WriteString(fmt.Sprintf("\n! %s %s — %s: %s", item.StartAt.Format("02.01"), client, strings.Join(item.ServiceNames, ", "), financeReasonLabel(lang, item.Reason)))
		}
	} else {
		sb.WriteString("\n\n")
		sb.WriteString(tr(lang, "finance_report_complete"))
	}
	return sb.String()
}

func formatMoney(cents int64, currency string) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	return fmt.Sprintf("%s%d.%02d %s", sign, cents/100, cents%100, currency)
}

func financeCategoryLabel(lang, category string) string {
	key := "finance_category_" + strings.ToLower(strings.TrimSpace(category))
	value := tr(lang, key)
	if value == "" || value == key {
		return category
	}
	return value
}

func financeReasonLabel(lang, reason string) string {
	key := "finance_reason_" + reason
	value := tr(lang, key)
	if value == "" || value == key {
		return tr(lang, "finance_reason_price_missing")
	}
	return value
}

func parseFinanceAmount(text string) (int64, bool) {
	match := financeAmountRE.FindStringSubmatch(strings.TrimSpace(text))
	if len(match) != 2 {
		return 0, false
	}
	value := strings.ReplaceAll(match[1], ",", ".")
	parts := strings.Split(value, ".")
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole <= 0 || len(parts) > 2 {
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
	return whole*100 + cents, true
}

func financeReportPeriodFromText(text string, now time.Time) (string, bool) {
	normalized := normalizeMatchText(text)
	reportMarker := false
	for _, marker := range []string{"посчитай", "отчет", "отчёт", "сколько заработ", "финанс", "итоги", "report", "calculate"} {
		if strings.Contains(normalized, marker) {
			reportMarker = true
			break
		}
	}
	if !reportMarker {
		return "", false
	}
	year := 0
	if match := financeYearRE.FindStringSubmatch(normalized); len(match) == 2 {
		year, _ = strconv.Atoi(match[1])
	}
	months := []struct {
		month time.Month
		names []string
	}{
		{time.January, []string{"январ", "january", "jan"}}, {time.February, []string{"феврал", "february", "feb"}},
		{time.March, []string{"март", "march"}}, {time.April, []string{"апрел", "april"}},
		{time.May, []string{"май", "мая", "may"}}, {time.June, []string{"июн", "june"}},
		{time.July, []string{"июл", "july"}}, {time.August, []string{"август", "august", "aug"}},
		{time.September, []string{"сентябр", "september", "sep"}}, {time.October, []string{"октябр", "october", "oct"}},
		{time.November, []string{"ноябр", "november", "nov"}}, {time.December, []string{"декабр", "december", "dec"}},
	}
	for _, item := range months {
		for _, name := range item.names {
			if strings.Contains(normalized, name) {
				if year == 0 {
					year = now.Year()
					if item.month > now.Month() {
						year--
					}
				}
				return fmt.Sprintf("%04d-%02d", year, item.month), true
			}
		}
	}
	if strings.Contains(normalized, "квартал") || strings.Contains(normalized, "quarter") {
		quarter := (int(now.Month())-1)/3 + 1
		for i, marker := range []string{"перв", "втор", "трет", "четвер", "1 кварт", "2 кварт", "3 кварт", "4 кварт", "q1", "q2", "q3", "q4"} {
			if strings.Contains(normalized, marker) {
				quarter = i%4 + 1
				break
			}
		}
		if year == 0 {
			year = now.Year()
		}
		return fmt.Sprintf("%04d-Q%d", year, quarter), true
	}
	if strings.Contains(normalized, "год") || strings.Contains(normalized, "year") {
		if year == 0 {
			year = now.Year()
		}
		return strconv.Itoa(year), true
	}
	if strings.Contains(normalized, "месяц") || strings.Contains(normalized, "month") {
		return "month", true
	}
	return "", false
}

func financeRangeForPeriod(period string, now time.Time) (time.Time, time.Time, string, bool) {
	loc := now.Location()
	switch period {
	case "month":
		from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
		return from, from.AddDate(0, 1, 0), "month", true
	case "quarter":
		month := time.Month((int(now.Month())-1)/3*3 + 1)
		from := time.Date(now.Year(), month, 1, 0, 0, 0, 0, loc)
		return from, from.AddDate(0, 3, 0), "quarter", true
	case "year":
		from := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, loc)
		return from, from.AddDate(1, 0, 0), "year", true
	}
	if parsed, err := time.Parse("2006-01", period); err == nil {
		from := time.Date(parsed.Year(), parsed.Month(), 1, 0, 0, 0, 0, loc)
		return from, from.AddDate(0, 1, 0), "month", true
	}
	if match := regexp.MustCompile(`^(20\d{2})-Q([1-4])$`).FindStringSubmatch(period); len(match) == 3 {
		year, _ := strconv.Atoi(match[1])
		quarter, _ := strconv.Atoi(match[2])
		from := time.Date(year, time.Month((quarter-1)*3+1), 1, 0, 0, 0, 0, loc)
		return from, from.AddDate(0, 3, 0), "quarter", true
	}
	if year, err := strconv.Atoi(period); err == nil && year >= 2000 && year <= 2100 {
		from := time.Date(year, 1, 1, 0, 0, 0, 0, loc)
		return from, from.AddDate(1, 0, 0), "year", true
	}
	return time.Time{}, time.Time{}, "", false
}
