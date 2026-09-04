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
	if !strings.HasPrefix(strings.TrimSpace(intent.Contact), "@") && normalizePhone(intent.Contact) == "" {
		aliases, aliasErr := b.store.ListContactAliases(ctx, user.TelegramID)
		if aliasErr != nil {
			b.logger.Printf("admin natural booking: list contact aliases failed admin=%d: %v", user.TelegramID, aliasErr)
		} else if alias, ok := resolveContactAlias(strings.TrimSpace(intent.Contact)+" "+text, aliases); ok {
			contactType = alias.ContactType
			contact = alias.Contact
			b.logger.Printf("admin natural booking: resolved contact alias admin=%d alias=%q type=%s", user.TelegramID, alias.Alias, alias.ContactType)
		}
	}
	state := ConversationState{
		BookingDraft: "admin",
		ContactType:  contactType,
		Username:     contact,
		FromDateTime: intent.StartAt,
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
	return true, b.continueAdminBookingDraft(ctx, chatID, user, state)
}

func (b *Bot) continueAdminBookingDraft(ctx context.Context, chatID int64, user UserRecord, state ConversationState) error {
	state.BookingDraft = "admin"
	if strings.TrimSpace(state.Username) == "" || state.ContactType == "" {
		state.Step = conversationStepAppointKind
		return b.beginConversation(ctx, chatID, user, state, "admin_booking_need_contact", contactTypeKeyboard(user.Language))
	}
	if len(state.ServiceIndexes) == 0 {
		if err := b.sendText(ctx, chatID, tr(user.Language, "admin_booking_need_service")); err != nil {
			return err
		}
		return b.askAdminAppointmentServices(ctx, chatID, user, state)
	}

	now := time.Now().In(time.Local)
	requested, err := parseAdminBookingStart(state.FromDateTime, now.Location())
	if err != nil || requested.Before(now) {
		state.Step = conversationStepAdminBookingTime
		if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
			return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
		}
		return b.sendText(ctx, chatID, tr(user.Language, "admin_booking_time_correction"))
	}

	dayStart := time.Date(requested.Year(), requested.Month(), requested.Day(), 0, 0, 0, 0, requested.Location())
	slots, err := b.store.ListFreeSlotsForServicesRange(ctx, user.TelegramID, state.ServiceIndexes, dayStart, dayStart.AddDate(0, 0, 7))
	if err != nil {
		b.logger.Printf("admin natural booking: list slots failed admin=%d services=%v: %v", user.TelegramID, state.ServiceIndexes, err)
		if errors.Is(err, store.ErrInvalidArgument) || errors.Is(err, store.ErrNotFound) {
			state.ServiceIndexes = nil
			return b.continueAdminBookingDraft(ctx, chatID, user, state)
		}
		return b.sendText(ctx, chatID, tr(user.Language, "free_failed"))
	}
	for index, slot := range slots {
		if slot.StartAt.Equal(requested) {
			return b.beginBookingConfirmation(ctx, chatID, user, state, index+1)
		}
	}
	unavailableReason := b.adminBookingUnavailableReason(ctx, user, state, requested)
	if len(slots) == 0 {
		state.Step = conversationStepAdminBookingTime
		if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
			return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
		}
		return b.sendText(ctx, chatID, unavailableReason+"\n"+tr(user.Language, "admin_booking_time_correction"))
	}
	return b.showAdminBookingAlternatives(ctx, chatID, user, state, slots, requested, unavailableReason)
}

func (b *Bot) adminBookingUnavailableReason(ctx context.Context, user UserRecord, state ConversationState, requested time.Time) string {
	services, err := b.store.ListServices(ctx, user.TelegramID)
	if err != nil {
		return tr(user.Language, "admin_booking_alternatives")
	}
	durationMin := 0
	seen := make(map[int]bool, len(state.ServiceIndexes))
	for _, index := range state.ServiceIndexes {
		if index <= 0 || index > len(services) || seen[index] {
			continue
		}
		seen[index] = true
		durationMin += services[index-1].DurationMin
	}
	if durationMin <= 0 {
		return tr(user.Language, "admin_booking_alternatives")
	}
	return tr(
		user.Language,
		"admin_booking_unavailable_reason",
		requested.Format("02.01.2006"),
		requested.Format("15:04"),
		durationMin,
	)
}

func (b *Bot) correctAdminBookingTime(ctx context.Context, user UserRecord, state ConversationState, text string) (ConversationState, bool) {
	if parsed, err := parseDateTimeInput(text); err == nil {
		state.FromDateTime = parsed.Format(time.RFC3339)
		return state, true
	}
	if b.adminBookingParser == nil {
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
	intent, err := b.adminBookingParser.ParseAdminBookingIntent(ctx, nlu.AdminBookingIntentRequest{
		Text: "Уточнение времени для записи клиента " + formatClientContact(state.Username) +
			" на " + strings.Join(selectedNames, ", ") + ": " + text,
		Language: user.Language,
		Now:      now,
		Timezone: now.Location().String(),
		Services: nluServices(services),
	})
	if err != nil {
		return state, false
	}
	if _, err := parseAdminBookingStart(intent.StartAt, now.Location()); err != nil {
		return state, false
	}
	state.FromDateTime = intent.StartAt
	return state, true
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
	case "name":
		return "name", strings.TrimSpace(contact)
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

func (b *Bot) showAdminBookingAlternatives(ctx context.Context, chatID int64, user UserRecord, state ConversationState, slots []AvailabilitySlot, requested time.Time, unavailableReason string) error {
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
	if strings.TrimSpace(unavailableReason) == "" {
		unavailableReason = tr(user.Language, "admin_booking_alternatives")
	}
	text := unavailableReason + "\n" + tr(user.Language, "admin_booking_alternatives") + "\n" + formatAvailabilitySlots(user.Language, selected, len(selected))
	return b.sendTextWithKeyboard(ctx, chatID, text, numberKeyboardWithPrefixLang(len(selected), "slot", user.Language))
}
