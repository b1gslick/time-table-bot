package bot

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"time-table-bot/internal/nlu"
	"time-table-bot/internal/store"
)

const naturalBookingMinConfidence = 0.45

func (b *Bot) handleNaturalBooking(ctx context.Context, chatID int64, user UserRecord, text string) (bool, error) {
	if b.bookingParser == nil || user.Role != RoleUser {
		return false, nil
	}
	text = strings.TrimSpace(text)
	if text == "" || strings.HasPrefix(text, "/") || utf8.RuneCountInString(text) > 500 {
		return false, nil
	}

	services, err := b.store.ListServices(ctx, user.TelegramID)
	if err != nil {
		b.logger.Printf("natural booking: list services failed user=%d: %v", user.TelegramID, err)
		return false, nil
	}
	if len(services) == 0 {
		return false, nil
	}
	if !looksLikeNaturalBookingCandidate(text, services) {
		return false, nil
	}

	now := time.Now().In(time.Local)
	intent, err := b.bookingParser.ParseBookingIntent(ctx, nlu.BookingIntentRequest{
		Text:     text,
		Language: user.Language,
		Now:      now,
		Timezone: now.Location().String(),
		Services: nluServices(services),
	})
	if err != nil {
		b.logger.Printf("natural booking: parser failed user=%d: %v", user.TelegramID, err)
		return false, nil
	}
	if !intent.IsBooking || intent.Confidence < naturalBookingMinConfidence {
		return false, nil
	}

	serviceIndexes := resolveNaturalBookingServices(intent, text, services)
	state := resetSlotBrowserState(ConversationState{
		BookingDraft: "client",
		DateFrom:     intent.DateFrom,
		DateTo:       intent.DateTo,
	})
	state.SlotPeriod = normalizeSlotPeriod(intent.Period)
	if len(serviceIndexes) == 0 {
		b.logger.Printf("natural booking: no service match user=%d text=%q intent=%+v", user.TelegramID, text, intent)
		if err := b.sendText(ctx, chatID, tr(user.Language, "natural_booking_choose_service")); err != nil {
			return true, err
		}
		return true, b.askCategoryWithState(ctx, chatID, user, state)
	}

	state.ServiceIndexes = serviceIndexes
	from, to := naturalBookingRange(intent, now)
	slots, err := b.store.ListFreeSlotsForServicesRange(ctx, user.TelegramID, serviceIndexes, from, to)
	if err != nil {
		b.logger.Printf("natural booking: list slots failed user=%d services=%v from=%s to=%s: %v", user.TelegramID, serviceIndexes, from.Format(time.RFC3339), to.Format(time.RFC3339), err)
		if errors.Is(err, store.ErrInvalidArgument) || errors.Is(err, store.ErrNotFound) {
			if err := b.sendText(ctx, chatID, tr(user.Language, "natural_booking_choose_service")); err != nil {
				return true, err
			}
			return true, b.askCategory(ctx, chatID, user, nil)
		}
		return true, b.sendText(ctx, chatID, tr(user.Language, "free_failed"))
	}
	b.logger.Printf("natural booking: user=%d services=%v from=%s to=%s period=%s slots=%d", user.TelegramID, serviceIndexes, from.Format(time.RFC3339), to.Format(time.RFC3339), state.SlotPeriod, len(slots))
	return true, b.showInteractiveSlots(ctx, chatID, user, state, slots)
}

func (b *Bot) continueClientBookingDraft(ctx context.Context, chatID int64, user UserRecord, state ConversationState) error {
	if len(state.ServiceIndexes) == 0 {
		return b.askCategoryWithState(ctx, chatID, user, state)
	}
	now := time.Now().In(time.Local)
	from, to := naturalBookingRange(nlu.BookingIntent{
		DateFrom: state.DateFrom,
		DateTo:   state.DateTo,
		Period:   state.SlotPeriod,
	}, now)
	period := normalizeSlotPeriod(state.SlotPeriod)
	state = resetSlotBrowserState(state)
	state.BookingDraft = "client"
	state.SlotPeriod = period
	slots, err := b.store.ListFreeSlotsForServicesRange(ctx, user.TelegramID, state.ServiceIndexes, from, to)
	if err != nil {
		b.logger.Printf("client booking draft: list slots failed user=%d services=%v: %v", user.TelegramID, state.ServiceIndexes, err)
		return b.sendText(ctx, chatID, tr(user.Language, "free_failed"))
	}
	return b.showInteractiveSlots(ctx, chatID, user, state, slots)
}

func (b *Bot) correctClientBookingTime(ctx context.Context, user UserRecord, state ConversationState, text string) (ConversationState, bool) {
	if b.bookingParser == nil {
		return state, false
	}
	services, err := b.store.ListServices(ctx, user.TelegramID)
	if err != nil {
		return state, false
	}
	selectedNames := make([]string, 0, len(state.ServiceIndexes))
	for _, index := range state.ServiceIndexes {
		if index > 0 && index <= len(services) {
			selectedNames = append(selectedNames, services[index-1].Name)
		}
	}
	now := time.Now().In(time.Local)
	intent, err := b.bookingParser.ParseBookingIntent(ctx, nlu.BookingIntentRequest{
		Text:     "Уточнение даты для записи на " + strings.Join(selectedNames, ", ") + ": " + text,
		Language: user.Language,
		Now:      now,
		Timezone: now.Location().String(),
		Services: nluServices(services),
	})
	if err != nil || intent.DateFrom == "" && intent.DateTo == "" {
		return state, false
	}
	state.DateFrom = intent.DateFrom
	state.DateTo = intent.DateTo
	state.SlotPeriod = normalizeSlotPeriod(intent.Period)
	return state, true
}

func nluServices(services []ServiceView) []nlu.Service {
	out := make([]nlu.Service, 0, len(services))
	for i, service := range services {
		out = append(out, nlu.Service{
			Index:       i + 1,
			Name:        service.Name,
			Category:    service.Category,
			Subcategory: service.Subcategory,
			Description: service.Description,
			AdminName:   service.AdminName,
			DurationMin: service.DurationMin,
		})
	}
	return out
}

func looksLikeNaturalBookingCandidate(text string, services []ServiceView) bool {
	normalized := normalizeMatchText(text)
	if normalized == "" {
		return false
	}
	keywords := []string{
		"хочу", "можно", "запис", "запись", "свобод", "окно", "время", "когда",
		"сегодня", "завтра", "послезавтра", "недел", "вечер", "утро", "днем",
		"час", "минут", "полтора", "полчаса", "после обеда", "до обеда",
		"book", "booking", "appointment", "slot", "available", "free", "tomorrow",
		"today", "evening", "morning", "afternoon", "week", "hour", "minute",
	}
	for _, keyword := range keywords {
		if strings.Contains(normalized, normalizeMatchText(keyword)) {
			return true
		}
	}
	for _, service := range services {
		if serviceMatchScore(text, service, 0) >= 5 {
			return true
		}
	}
	return false
}

func resolveNaturalBookingServices(intent nlu.BookingIntent, rawText string, services []ServiceView) []int {
	selected := make([]int, 0, len(intent.ServiceIndexes)+len(intent.ServiceQueries))
	add := func(index int) {
		if index <= 0 || index > len(services) || intInSlice(selected, index) {
			return
		}
		selected = append(selected, index)
	}
	for _, index := range intent.ServiceIndexes {
		add(index)
	}
	for _, query := range intent.ServiceQueries {
		index, score := bestServiceMatch(query, intent.DurationMin, services)
		if score >= 3 {
			add(index)
		}
	}
	if len(selected) == 0 {
		index, score := bestServiceMatch(rawText, intent.DurationMin, services)
		if score >= 7 {
			add(index)
		}
	}
	return selected
}

func bestServiceMatch(query string, durationMin int, services []ServiceView) (int, int) {
	bestIndex := 0
	bestScore := 0
	for i, service := range services {
		score := serviceMatchScore(query, service, durationMin)
		if score > bestScore {
			bestIndex = i + 1
			bestScore = score
		}
	}
	return bestIndex, bestScore
}

func serviceMatchScore(query string, service ServiceView, durationMin int) int {
	queryNorm := normalizeMatchText(query)
	if queryNorm == "" {
		return 0
	}
	fields := []string{service.Name, service.Category, service.Subcategory, service.Description}
	score := 0
	for _, field := range fields {
		fieldNorm := normalizeMatchText(field)
		if fieldNorm == "" {
			continue
		}
		if strings.Contains(fieldNorm, queryNorm) || strings.Contains(queryNorm, fieldNorm) {
			score += 8
		}
		score += tokenOverlapScore(matchTokens(queryNorm), matchTokens(fieldNorm))
	}
	if durationMin > 0 && service.DurationMin > 0 {
		diff := int(math.Abs(float64(service.DurationMin - durationMin)))
		switch {
		case diff == 0:
			score += 3
		case diff <= 15:
			score += 1
		}
	}
	return score
}

func tokenOverlapScore(left, right []string) int {
	score := 0
	for _, l := range left {
		if len([]rune(l)) < 3 {
			continue
		}
		for _, r := range right {
			if len([]rune(r)) < 3 {
				continue
			}
			if l == r {
				score += 4
				continue
			}
			if commonPrefixRuneLen(l, r) >= 4 {
				score += 3
			}
		}
	}
	return score
}

func matchTokens(value string) []string {
	parts := strings.Fields(value)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || isMatchStopword(part) {
			continue
		}
		out = append(out, part)
	}
	return out
}

func normalizeMatchText(value string) string {
	value = strings.ToLower(strings.ReplaceAll(value, "ё", "е"))
	var sb strings.Builder
	lastSpace := true
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(r)
			lastSpace = false
			continue
		}
		if !lastSpace {
			sb.WriteByte(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(sb.String())
}

func isMatchStopword(value string) bool {
	switch value {
	case "на", "в", "во", "и", "или", "к", "ко", "по", "за", "для", "мне", "меня", "если", "есть",
		"a", "an", "the", "to", "for", "me", "if", "is", "are":
		return true
	default:
		return false
	}
}

func commonPrefixRuneLen(left, right string) int {
	lr := []rune(left)
	rr := []rune(right)
	limit := len(lr)
	if len(rr) < limit {
		limit = len(rr)
	}
	count := 0
	for count < limit && lr[count] == rr[count] {
		count++
	}
	return count
}

func naturalBookingRange(intent nlu.BookingIntent, now time.Time) (time.Time, time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
	loc := now.Location()
	from := now
	to := now.AddDate(0, 0, 7)

	if parsed, ok := parseIntentDate(intent.DateFrom, loc); ok {
		from = parsed
		if sameLocalDate(from, now) || from.Before(now) {
			from = now
		}
		if parsedTo, ok := parseIntentDate(intent.DateTo, loc); ok {
			to = parsedTo
		} else {
			to = dateOnly(from).AddDate(0, 0, 1)
		}
	} else if parsedTo, ok := parseIntentDate(intent.DateTo, loc); ok {
		to = parsedTo
	}

	if !to.After(from) {
		to = dateOnly(from).AddDate(0, 0, 1)
	}
	maxTo := now.AddDate(0, 0, 45)
	if to.After(maxTo) {
		to = maxTo
	}
	return from.In(loc), to.In(loc)
}

func parseIntentDate(value string, loc *time.Location) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	parsed, err := time.ParseInLocation("2006-01-02", value, loc)
	if err != nil {
		return time.Time{}, false
	}
	return dateOnly(parsed), true
}

func sameLocalDate(left, right time.Time) bool {
	left = left.In(right.Location())
	return left.Year() == right.Year() && left.Month() == right.Month() && left.Day() == right.Day()
}

func normalizeSlotPeriod(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "morning", "day", "evening":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "all"
	}
}
