package bot

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"time-table-bot/internal/nlu"
	"time-table-bot/internal/store"
)

const (
	adminBookingMinConfidence = 0.55
	adminBookingMaxOptions    = 7
)

func (b *Bot) handleAdminNaturalBooking(ctx context.Context, chatID int64, user UserRecord, text string) (bool, error) {
	if b.adminBookingParser == nil || !isAdmin(user.Role) {
		return false, nil
	}
	text = strings.TrimSpace(text)
	if text == "" || strings.HasPrefix(text, "/") || utf8.RuneCountInString(text) > 500 {
		return false, nil
	}
	services, err := b.store.ListServices(ctx, user.TelegramID)
	if err != nil || len(services) == 0 {
		return false, nil
	}
	if !looksLikeAdminBookingCandidate(text, services) {
		return false, nil
	}

	now := time.Now().In(time.Local)
	intent, err := b.adminBookingParser.ParseAdminBookingIntent(ctx, nlu.AdminBookingIntentRequest{
		Text:     text,
		Language: user.Language,
		Now:      now,
		Timezone: now.Location().String(),
		Services: nluServices(services),
	})
	if err != nil {
		b.logger.Printf("admin natural booking: parser failed admin=%d: %v", user.TelegramID, err)
		return true, b.sendText(ctx, chatID, tr(user.Language, "admin_booking_parse_failed"))
	}
	if !intent.IsCreateBooking || intent.Confidence < adminBookingMinConfidence {
		return false, nil
	}

	contactType, contact := normalizeAdminBookingContact(intent.ContactType, intent.Contact)
	if contact == "" {
		return true, b.beginConversation(ctx, chatID, user, ConversationState{Step: conversationStepAppointKind}, "admin_booking_need_contact", contactTypeKeyboard(user.Language))
	}
	state := ConversationState{
		ContactType: contactType,
		Username:    contact,
	}
	state.ServiceIndexes = resolveNaturalBookingServices(nlu.BookingIntent{
		ServiceIndexes: intent.ServiceIndexes,
		ServiceQueries: intent.ServiceQueries,
		DurationMin:    intent.DurationMin,
	}, text, services)
	if !adminBookingDurationMatches(state.ServiceIndexes, intent.DurationMin, services) {
		state.ServiceIndexes = resolveNaturalBookingServices(nlu.BookingIntent{
			ServiceQueries: intent.ServiceQueries,
			DurationMin:    intent.DurationMin,
		}, text, services)
		if !adminBookingDurationMatches(state.ServiceIndexes, intent.DurationMin, services) {
			state.ServiceIndexes = nil
		}
	}
	if len(state.ServiceIndexes) == 0 {
		if err := b.sendText(ctx, chatID, tr(user.Language, "admin_booking_need_service")); err != nil {
			return true, err
		}
		return true, b.askAdminAppointmentServices(ctx, chatID, user, state)
	}

	requested, err := parseAdminBookingStart(intent.StartAt, now.Location())
	if err != nil || requested.Before(now) {
		state.Step = conversationStepTimeChoice
		if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
			return true, b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
		}
		return true, b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "admin_booking_need_time"), timeChoiceKeyboard(user.Language))
	}

	dayStart := time.Date(requested.Year(), requested.Month(), requested.Day(), 0, 0, 0, 0, requested.Location())
	slots, err := b.store.ListFreeSlotsForServicesRange(ctx, user.TelegramID, state.ServiceIndexes, dayStart, dayStart.AddDate(0, 0, 7))
	if err != nil {
		b.logger.Printf("admin natural booking: list slots failed admin=%d services=%v: %v", user.TelegramID, state.ServiceIndexes, err)
		if errors.Is(err, store.ErrInvalidArgument) || errors.Is(err, store.ErrNotFound) {
			return true, b.sendText(ctx, chatID, tr(user.Language, "admin_booking_need_service"))
		}
		return true, b.sendText(ctx, chatID, tr(user.Language, "free_failed"))
	}
	for index, slot := range slots {
		if slot.StartAt.Equal(requested) {
			return true, b.beginBookingConfirmation(ctx, chatID, user, state, index+1)
		}
	}
	if len(slots) == 0 {
		return true, b.sendText(ctx, chatID, tr(user.Language, "free_empty_try_other"))
	}
	return true, b.showAdminBookingAlternatives(ctx, chatID, user, state, slots, requested)
}

func adminBookingDurationMatches(indexes []int, requestedMinutes int, services []ServiceView) bool {
	if len(indexes) == 0 || requestedMinutes <= 0 {
		return true
	}
	total := 0
	for _, index := range indexes {
		if index <= 0 || index > len(services) || services[index-1].DurationMin <= 0 {
			return false
		}
		total += services[index-1].DurationMin
	}
	return total == requestedMinutes
}

func looksLikeAdminBookingCandidate(text string, services []ServiceView) bool {
	normalized := normalizeMatchText(text)
	for _, phrase := range []string{
		"запиши", "запишите", "записать клиента", "создай запись", "поставь клиента",
		"book client", "book @", "create booking", "make appointment",
	} {
		if strings.Contains(normalized, normalizeMatchText(phrase)) {
			return true
		}
	}
	return looksLikeNaturalBookingCandidate(text, services)
}

func normalizeAdminBookingContact(contactType, contact string) (string, string) {
	contact = strings.TrimSpace(contact)
	switch strings.ToLower(strings.TrimSpace(contactType)) {
	case "telegram":
		return "telegram", normalizeUsername(contact)
	case "phone":
		return "phone", normalizePhone(contact)
	}
	if strings.HasPrefix(contact, "@") {
		return "telegram", normalizeUsername(contact)
	}
	if phone := normalizePhone(contact); phone != "" {
		return "phone", phone
	}
	return "", ""
}

func parseAdminBookingStart(value string, loc *time.Location) (time.Time, error) {
	value = strings.TrimSpace(value)
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.In(loc), nil
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", dateTimeLayout} {
		if parsed, err := time.ParseInLocation(layout, value, loc); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, errors.New("invalid admin booking start")
}

func (b *Bot) showAdminBookingAlternatives(ctx context.Context, chatID int64, user UserRecord, state ConversationState, slots []AvailabilitySlot, requested time.Time) error {
	type candidate struct {
		index    int
		distance time.Duration
	}
	candidates := make([]candidate, 0, len(slots))
	for index, slot := range slots {
		distance := slot.StartAt.Sub(requested)
		if distance < 0 {
			distance = -distance
		}
		candidates = append(candidates, candidate{index: index, distance: distance})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].distance == candidates[j].distance {
			return slots[candidates[i].index].StartAt.Before(slots[candidates[j].index].StartAt)
		}
		return candidates[i].distance < candidates[j].distance
	})
	if len(candidates) > adminBookingMaxOptions {
		candidates = candidates[:adminBookingMaxOptions]
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return slots[candidates[i].index].StartAt.Before(slots[candidates[j].index].StartAt)
	})
	selected := make([]AvailabilitySlot, 0, len(candidates))
	state.VisibleSlotIndexes = make([]int, 0, len(candidates))
	for _, item := range candidates {
		selected = append(selected, slots[item.index])
		state.VisibleSlotIndexes = append(state.VisibleSlotIndexes, item.index+1)
	}
	state.Step = conversationStepSlot
	state.PendingSlotIndex = 0
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	text := tr(user.Language, "admin_booking_alternatives") + "\n" + formatAvailabilitySlots(user.Language, selected, len(selected))
	return b.sendTextWithKeyboard(ctx, chatID, text, numberKeyboardWithPrefixLang(len(selected), "slot", user.Language))
}
