package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"time-table-bot/internal/nlu"
	"time-table-bot/internal/telegram"
)

var naturalScheduleDatePattern = regexp.MustCompile(`\b(?:\d{4}-\d{2}-\d{2}|\d{1,2}\.\d{1,2}(?:\.\d{2,4})?)\b`)

var schedulePlanWeekdayOrder = []time.Weekday{
	time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday,
}

func (b *Bot) handleAdminNaturalSchedule(ctx context.Context, chatID int64, user UserRecord, text string) (bool, error) {
	if !isAdmin(user.Role) {
		return false, nil
	}
	if looksLikeSchedulePlanRequest(text) {
		return true, b.startAdminSchedulePlan(ctx, chatID, user, text, nil)
	}
	start, ok := parseAdminNaturalScheduleWeek(text, time.Now())
	if !ok {
		return false, nil
	}
	return true, b.handleWeek(ctx, chatID, user, []string{"/week", start.Format("2006-01-02")})
}

func looksLikeSchedulePlanRequest(text string) bool {
	normalized := normalizeMatchText(text)
	action := containsAnyPhrase(normalized, []string{
		"сделай", "создай", "заполни", "сформируй", "поставь", "нагенер", "скопируй", "make", "create", "fill", "generate", "copy",
	})
	copyLike := containsAnyPhrase(normalized, []string{"как в этом", "как в текущ", "same as"})
	if !action || !copyLike && !containsAnyPhrase(normalized, []string{"расписан", "график", "рабоч", "schedule", "timetable"}) {
		return false
	}
	return true
}

func (b *Bot) startAdminSchedulePlan(ctx context.Context, chatID int64, user UserRecord, text string, current *SchedulePlanDraft) error {
	parser, ok := b.adminBookingParser.(nlu.AdminSchedulePlanParser)
	if !ok {
		return b.sendText(ctx, chatID, tr(user.Language, "schedule_plan_unavailable"))
	}
	currentJSON := ""
	if current != nil {
		if raw, err := json.Marshal(current); err == nil {
			currentJSON = string(raw)
		}
	}
	now := time.Now().In(time.Local)
	intent, err := parser.ParseAdminSchedulePlan(ctx, nlu.AdminSchedulePlanRequest{
		Text: text, Language: user.Language, Now: now, Timezone: now.Location().String(), CurrentPlan: currentJSON,
	})
	if err != nil {
		b.logger.Printf("natural schedule plan: parser failed admin=%d: %v", user.TelegramID, err)
		return b.sendText(ctx, chatID, tr(user.Language, "schedule_plan_parse_failed"))
	}
	if !intent.IsSchedulePlan || intent.Confidence < 0.55 {
		return b.sendText(ctx, chatID, tr(user.Language, "schedule_plan_parse_failed"))
	}
	plan, err := b.materializeSchedulePlan(ctx, user, intent, now)
	if err != nil {
		b.logger.Printf("natural schedule plan: invalid admin=%d: %v", user.TelegramID, err)
		return b.sendText(ctx, chatID, tr(user.Language, "schedule_plan_invalid", err.Error()))
	}
	state := ConversationState{Step: conversationStepSchedulePlan, SchedulePlan: plan}
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	return b.showSchedulePlanPreview(ctx, chatID, user, state)
}

func (b *Bot) materializeSchedulePlan(ctx context.Context, user UserRecord, intent nlu.AdminSchedulePlanIntent, now time.Time) (SchedulePlanDraft, error) {
	target, err := time.ParseInLocation(monthLayout, intent.TargetMonth, now.Location())
	if err != nil {
		return SchedulePlanDraft{}, fmt.Errorf("не указан месяц")
	}
	target = time.Date(target.Year(), target.Month(), 1, 0, 0, 0, 0, now.Location())
	currentMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	if target.Before(currentMonth) || target.After(currentMonth.AddDate(1, 0, 0)) {
		return SchedulePlanDraft{}, fmt.Errorf("месяц должен быть текущим или одним из следующих 12")
	}

	rules := map[time.Weekday]SchedulePlanRule{}
	duration := intent.SlotDurationMin
	copyFrom := ""
	if strings.TrimSpace(intent.CopyFromMonth) != "" {
		source, parseErr := time.ParseInLocation(monthLayout, intent.CopyFromMonth, now.Location())
		if parseErr != nil {
			return SchedulePlanDraft{}, fmt.Errorf("не удалось определить месяц для копирования")
		}
		source = time.Date(source.Year(), source.Month(), 1, 0, 0, 0, 0, now.Location())
		slots, listErr := b.store.AdminSchedule(ctx, user.TelegramID, source, source.AddDate(0, 1, 0))
		if listErr != nil {
			return SchedulePlanDraft{}, listErr
		}
		copiedRules, copiedDuration := inferSchedulePlanRules(slots)
		if len(copiedRules) == 0 {
			return SchedulePlanDraft{}, fmt.Errorf("в месяце для копирования нет рабочего расписания")
		}
		for weekday, rule := range copiedRules {
			rules[weekday] = rule
		}
		if duration <= 0 {
			duration = copiedDuration
		}
		copyFrom = source.Format(monthLayout)
	}
	for _, input := range intent.Rules {
		if len(input.Weekdays) == 0 || !validSchedulePlanRange(input.Start, input.End) {
			return SchedulePlanDraft{}, fmt.Errorf("проверьте дни недели и рабочее время")
		}
		for _, iso := range input.Weekdays {
			weekday, ok := isoWeekday(iso)
			if !ok {
				return SchedulePlanDraft{}, fmt.Errorf("не удалось определить день недели")
			}
			rules[weekday] = SchedulePlanRule{Weekdays: []int{iso}, Start: input.Start, End: input.End}
		}
	}
	if len(rules) == 0 && len(intent.ExtraDays) == 0 {
		return SchedulePlanDraft{}, fmt.Errorf("не указаны рабочие дни")
	}

	days := map[string]SchedulePlanDay{}
	for day := target; day.Before(target.AddDate(0, 1, 0)); day = day.AddDate(0, 0, 1) {
		if rule, ok := rules[day.Weekday()]; ok {
			days[day.Format("2006-01-02")] = SchedulePlanDay{Date: day.Format("2006-01-02"), Start: rule.Start, End: rule.End}
		}
	}
	fallback, hasFallback := mostCommonSchedulePlanRule(rules)
	for _, extra := range intent.ExtraDays {
		date, parseErr := time.ParseInLocation("2006-01-02", extra.Date, now.Location())
		if parseErr != nil || date.Year() != target.Year() || date.Month() != target.Month() {
			return SchedulePlanDraft{}, fmt.Errorf("дополнительный рабочий день должен быть в выбранном месяце")
		}
		if extra.Weekday > 0 {
			claimed, ok := isoWeekday(extra.Weekday)
			if !ok || claimed != date.Weekday() {
				return SchedulePlanDraft{}, fmt.Errorf("%s приходится на %s, проверьте дату", date.Format("02.01.2006"), weekdayShort(user.Language, date.Weekday()))
			}
		}
		start, end := strings.TrimSpace(extra.Start), strings.TrimSpace(extra.End)
		if start == "" && end == "" {
			if rule, ok := rules[date.Weekday()]; ok {
				start, end = rule.Start, rule.End
			} else if hasFallback {
				start, end = fallback.Start, fallback.End
			}
		}
		if !validSchedulePlanRange(start, end) {
			return SchedulePlanDraft{}, fmt.Errorf("не указано время для %s", date.Format("02.01.2006"))
		}
		key := date.Format("2006-01-02")
		days[key] = SchedulePlanDay{Date: key, Start: start, End: end, Extra: true}
	}
	closed := make([]string, 0, len(intent.ClosedDates))
	for _, raw := range intent.ClosedDates {
		date, parseErr := time.ParseInLocation("2006-01-02", raw, now.Location())
		if parseErr != nil || date.Year() != target.Year() || date.Month() != target.Month() {
			return SchedulePlanDraft{}, fmt.Errorf("нерабочая дата должна быть в выбранном месяце")
		}
		key := date.Format("2006-01-02")
		delete(days, key)
		closed = append(closed, key)
	}
	materialized := make([]SchedulePlanDay, 0, len(days))
	for _, day := range days {
		materialized = append(materialized, day)
	}
	sort.Slice(materialized, func(i, j int) bool { return materialized[i].Date < materialized[j].Date })
	if len(materialized) == 0 {
		return SchedulePlanDraft{}, fmt.Errorf("после исключений не осталось рабочих дней")
	}
	return SchedulePlanDraft{
		TargetMonth: target.Format(monthLayout), CopyFromMonth: copyFrom, Rules: groupedSchedulePlanRules(rules),
		Days: materialized, ClosedDates: closed, SlotDurationMin: duration,
	}, nil
}

func validSchedulePlanRange(start, end string) bool {
	from, err1 := parseClock(start)
	to, err2 := parseClock(end)
	return err1 == nil && err2 == nil && to > from
}

func isoWeekday(value int) (time.Weekday, bool) {
	if value < 1 || value > 7 {
		return 0, false
	}
	if value == 7 {
		return time.Sunday, true
	}
	return time.Weekday(value), true
}

func weekdayISO(value time.Weekday) int {
	if value == time.Sunday {
		return 7
	}
	return int(value)
}

func inferSchedulePlanRules(slots []ScheduleGridSlot) (map[time.Weekday]SchedulePlanRule, int) {
	type dayWindow struct {
		date     time.Time
		startMin int
		endMin   int
	}
	byDate := map[string]dayWindow{}
	durations := map[int]int{}
	for _, slot := range slots {
		local := slot.StartAt.In(time.Local)
		key := local.Format("2006-01-02")
		startMin := local.Hour()*60 + local.Minute()
		end := slot.EndAt.In(time.Local)
		endMin := end.Hour()*60 + end.Minute()
		window, ok := byDate[key]
		if !ok || startMin < window.startMin {
			window.startMin = startMin
		}
		if !ok || endMin > window.endMin {
			window.endMin = endMin
		}
		window.date = local
		byDate[key] = window
		minutes := int(slot.EndAt.Sub(slot.StartAt) / time.Minute)
		if minutes > 0 {
			durations[minutes]++
		}
	}
	type signature struct{ start, end int }
	counts := map[time.Weekday]map[signature]int{}
	for _, window := range byDate {
		weekday := window.date.Weekday()
		if counts[weekday] == nil {
			counts[weekday] = map[signature]int{}
		}
		counts[weekday][signature{window.startMin, window.endMin}]++
	}
	rules := map[time.Weekday]SchedulePlanRule{}
	for weekday, options := range counts {
		best, bestCount := signature{}, -1
		for option, count := range options {
			if count > bestCount || count == bestCount && option.start < best.start {
				best, bestCount = option, count
			}
		}
		rules[weekday] = SchedulePlanRule{Weekdays: []int{weekdayISO(weekday)}, Start: clockFromMinutes(best.start), End: clockFromMinutes(best.end)}
	}
	bestDuration, bestCount := 0, -1
	for minutes, count := range durations {
		if count > bestCount || count == bestCount && minutes < bestDuration {
			bestDuration, bestCount = minutes, count
		}
	}
	return rules, bestDuration
}

func clockFromMinutes(value int) string {
	return fmt.Sprintf("%02d:%02d", value/60, value%60)
}

func mostCommonSchedulePlanRule(rules map[time.Weekday]SchedulePlanRule) (SchedulePlanRule, bool) {
	type key struct{ start, end string }
	counts := map[key]int{}
	for _, rule := range rules {
		counts[key{rule.Start, rule.End}]++
	}
	best, bestCount := key{}, 0
	for candidate, count := range counts {
		if count > bestCount {
			best, bestCount = candidate, count
		}
	}
	return SchedulePlanRule{Start: best.start, End: best.end}, bestCount > 0
}

func groupedSchedulePlanRules(rules map[time.Weekday]SchedulePlanRule) []SchedulePlanRule {
	grouped := map[string]*SchedulePlanRule{}
	for _, weekday := range schedulePlanWeekdayOrder {
		rule, ok := rules[weekday]
		if !ok {
			continue
		}
		key := rule.Start + "-" + rule.End
		if grouped[key] == nil {
			grouped[key] = &SchedulePlanRule{Start: rule.Start, End: rule.End}
		}
		grouped[key].Weekdays = append(grouped[key].Weekdays, weekdayISO(weekday))
	}
	out := make([]SchedulePlanRule, 0, len(grouped))
	for _, rule := range grouped {
		out = append(out, *rule)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Weekdays[0] < out[j].Weekdays[0] })
	return out
}

func (b *Bot) showSchedulePlanPreview(ctx context.Context, chatID int64, user UserRecord, state ConversationState) error {
	image, err := renderSchedulePlanMonthImage(user.Language, state.SchedulePlan)
	if err != nil {
		b.logger.Printf("schedule plan image failed admin=%d: %v", user.TelegramID, err)
		return b.sendTextWithKeyboard(ctx, chatID, formatSchedulePlanPreview(user.Language, state.SchedulePlan), schedulePlanKeyboard(user.Language))
	}
	return b.sendPhoto(ctx, chatID, image, formatSchedulePlanPreview(user.Language, state.SchedulePlan), schedulePlanKeyboard(user.Language))
}

func (b *Bot) conversationSchedulePlanConfirm(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	switch normalizeChoice(text) {
	case "yes":
		return b.applySchedulePlan(ctx, chatID, user, state)
	case "no":
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "schedule_plan_cancelled"), keyboardForUser(user))
	case "edit":
		state.Step = conversationStepSchedulePlanEdit
		if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
			return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
		}
		return b.sendText(ctx, chatID, tr(user.Language, "schedule_plan_edit_ask"))
	default:
		return b.showSchedulePlanPreview(ctx, chatID, user, state)
	}
}

func (b *Bot) conversationSchedulePlanEdit(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	return b.startAdminSchedulePlan(ctx, chatID, user, text, &state.SchedulePlan)
}

func (b *Bot) handleSchedulePlanCallback(ctx context.Context, cb *telegram.CallbackQuery) error {
	user, err := b.userFromCallback(ctx, cb)
	if err != nil {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(LangRU, "register_failed"))
	}
	state, err := b.store.GetConversationState(ctx, user.TelegramID)
	if err != nil || state.Step != conversationStepSchedulePlan {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(user.Language, "schedule_plan_expired"))
	}
	return b.conversationSchedulePlanConfirm(ctx, cb.Message.Chat.ID, user, state, strings.TrimPrefix(cb.Data, "scheduleplan:"))
}

func (b *Bot) applySchedulePlan(ctx context.Context, chatID int64, user UserRecord, state ConversationState) error {
	created, skipped := 0, 0
	for _, day := range state.SchedulePlan.Days {
		date, err := time.ParseInLocation("2006-01-02", day.Date, time.Local)
		if err != nil {
			return b.sendText(ctx, chatID, tr(user.Language, "schedule_plan_apply_failed"))
		}
		start, err1 := parseClock(day.Start)
		end, err2 := parseClock(day.End)
		if err1 != nil || err2 != nil || end <= start {
			return b.sendText(ctx, chatID, tr(user.Language, "schedule_plan_apply_failed"))
		}
		result, err := b.store.GenerateSchedule(ctx, user.TelegramID, GenerateScheduleRequest{
			Date: date, DayStart: start, DayEnd: end, DurationMin: state.SchedulePlan.SlotDurationMin,
		})
		if err != nil {
			b.logger.Printf("schedule plan apply failed admin=%d date=%s: %v", user.TelegramID, day.Date, err)
			return b.sendText(ctx, chatID, tr(user.Language, "schedule_plan_apply_failed"))
		}
		created += result.Created
		skipped += result.Skipped
	}
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "schedule_plan_applied", created, skipped), keyboardForUser(user))
}

func formatSchedulePlanPreview(lang string, plan SchedulePlanDraft) string {
	var sb strings.Builder
	sb.WriteString(tr(lang, "schedule_plan_preview", plan.TargetMonth))
	for _, rule := range plan.Rules {
		labels := make([]string, 0, len(rule.Weekdays))
		for _, iso := range rule.Weekdays {
			if weekday, ok := isoWeekday(iso); ok {
				labels = append(labels, weekdayShort(lang, weekday))
			}
		}
		if len(labels) > 0 {
			sb.WriteString("\n")
			sb.WriteString(strings.Join(labels, ", "))
			sb.WriteString(": ")
			sb.WriteString(rule.Start)
			sb.WriteString("-")
			sb.WriteString(rule.End)
		}
	}
	extras := make([]SchedulePlanDay, 0)
	for _, day := range plan.Days {
		if day.Extra {
			extras = append(extras, day)
		}
	}
	if len(extras) > 0 {
		sb.WriteString("\n")
		sb.WriteString(tr(lang, "schedule_plan_extra_days"))
		for _, day := range extras {
			date, _ := time.Parse("2006-01-02", day.Date)
			sb.WriteString(" ")
			sb.WriteString(date.Format("02.01"))
			sb.WriteString(" ")
			sb.WriteString(day.Start)
			sb.WriteString("-")
			sb.WriteString(day.End)
		}
	}
	if len(plan.ClosedDates) > 0 {
		sb.WriteString("\n")
		sb.WriteString(tr(lang, "schedule_plan_closed_days", strings.Join(plan.ClosedDates, ", ")))
	}
	if plan.CopyFromMonth != "" {
		sb.WriteString("\n")
		sb.WriteString(tr(lang, "schedule_plan_copied", plan.CopyFromMonth))
	}
	sb.WriteString("\n\n")
	sb.WriteString(tr(lang, "schedule_plan_working_days", len(plan.Days)))
	sb.WriteString("\n")
	sb.WriteString(tr(lang, "schedule_plan_existing_kept"))
	return sb.String()
}

func parseAdminNaturalScheduleWeek(text string, now time.Time) (time.Time, bool) {
	normalized := normalizeMatchText(text)
	if normalized == "" || !containsAnyPhrase(normalized, []string{"график", "расписан", "schedule", "timetable"}) {
		return time.Time{}, false
	}
	if !containsAnyPhrase(normalized, []string{
		"покаж", "пришл", "отправ", "дай", "за недел", "на недел", "текущ", "следующ", "прошл", "предыдущ",
		"show", "send", "this week", "next week", "last week", "current week",
	}) {
		return time.Time{}, false
	}

	now = dateOnly(now)
	if strings.Contains(normalized, "следующ") || strings.Contains(normalized, "next week") {
		return weekStart(now.AddDate(0, 0, 7)), true
	}
	if strings.Contains(normalized, "прошл") || strings.Contains(normalized, "предыдущ") || strings.Contains(normalized, "last week") {
		return weekStart(now.AddDate(0, 0, -7)), true
	}
	if rawDate := naturalScheduleDatePattern.FindString(text); rawDate != "" {
		if parsed, err := parseUserDate(rawDate, now); err == nil {
			return weekStart(parsed), true
		}
	}
	return weekStart(now), true
}

func containsAnyPhrase(value string, phrases []string) bool {
	for _, phrase := range phrases {
		if strings.Contains(value, phrase) {
			return true
		}
	}
	return false
}
