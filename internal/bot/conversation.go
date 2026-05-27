package bot

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"time-table-bot/internal/store"
	"time-table-bot/internal/telegram"
)

func (b *Bot) handleConversation(ctx context.Context, chatID int64, user UserRecord, text string) (bool, error) {
	state, err := b.store.GetConversationState(ctx, user.TelegramID)
	if errors.Is(err, store.ErrNotFound) || state.Step == "" {
		return false, nil
	}
	if err != nil {
		return true, b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}

	switch state.Step {
	case conversationStepLanguage:
		return true, b.conversationLanguage(ctx, chatID, user, text)
	case conversationStepService:
		return true, b.conversationService(ctx, chatID, user, state, text)
	case conversationStepMore:
		return true, b.conversationMore(ctx, chatID, user, state, text)
	case conversationStepTimeChoice:
		return true, b.conversationTimeChoice(ctx, chatID, user, state, text)
	case conversationStepDates:
		return true, b.conversationDates(ctx, chatID, user, state, text)
	case conversationStepSlot:
		return true, b.conversationSlot(ctx, chatID, user, text)
	default:
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return false, nil
	}
}

func (b *Bot) conversationLanguage(ctx context.Context, chatID int64, user UserRecord, text string) error {
	lang, ok := parseLanguageChoice(text)
	if !ok {
		return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "choose_language"), languageKeyboard())
	}
	if err := b.store.SetUserLanguage(ctx, user.TelegramID, lang); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "lang_failed"))
	}
	user.Language = lang
	return b.askService(ctx, chatID, user, nil)
}

func (b *Bot) conversationService(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	index, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || index <= 0 {
		return b.askService(ctx, chatID, user, state.ServiceIndexes)
	}
	services, err := b.store.ListServices(ctx, user.TelegramID)
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "services_list_failed"))
	}
	if index > len(services) {
		return b.askService(ctx, chatID, user, state.ServiceIndexes)
	}
	state.ServiceIndexes = append(state.ServiceIndexes, index)
	state.Step = conversationStepMore
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "ask_more_services"), yesNoKeyboard(user.Language))
}

func (b *Bot) conversationMore(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	if isYes(user.Language, text) {
		return b.askService(ctx, chatID, user, state.ServiceIndexes)
	}
	if isNo(user.Language, text) {
		state.Step = conversationStepTimeChoice
		if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
			return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
		}
		return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "ask_time_choice"), timeChoiceKeyboard(user.Language))
	}
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "ask_more_services"), yesNoKeyboard(user.Language))
}

func (b *Bot) conversationTimeChoice(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	normalized := normalizeChoice(text)
	if normalized == "nearest" {
		now := time.Now()
		slots, err := b.store.ListFreeSlotsForServicesRange(ctx, user.TelegramID, state.ServiceIndexes, now, now.AddDate(0, 0, 7))
		if err != nil {
			return b.sendText(ctx, chatID, tr(user.Language, "free_failed"))
		}
		return b.showInteractiveSlots(ctx, chatID, user, state, slots)
	}
	if normalized == "dates" {
		state.Step = conversationStepDates
		if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
			return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
		}
		return b.sendText(ctx, chatID, tr(user.Language, "ask_dates"))
	}
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "ask_time_choice"), timeChoiceKeyboard(user.Language))
}

func (b *Bot) conversationDates(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	dates, err := parseDateList(text, time.Now())
	if err != nil || len(dates) == 0 {
		return b.sendText(ctx, chatID, tr(user.Language, "bad_dates"))
	}
	slots, err := b.store.ListFreeSlotsForServicesDates(ctx, user.TelegramID, state.ServiceIndexes, dates)
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "free_failed"))
	}
	return b.showInteractiveSlots(ctx, chatID, user, state, slots)
}

func (b *Bot) conversationSlot(ctx context.Context, chatID int64, user UserRecord, text string) error {
	index, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || index <= 0 {
		return b.sendText(ctx, chatID, tr(user.Language, "choose_slot_number"))
	}
	travel := user.TravelMin
	if travel <= 0 {
		travel = 30
	}
	start, err := b.store.BookForUserByIndex(ctx, user.TelegramID, index, travel)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalidArgument) {
			return b.sendText(ctx, chatID, tr(user.Language, "book_need_schedule"))
		}
		return b.sendText(ctx, chatID, tr(user.Language, "book_failed"))
	}
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "book_ok", start.Format(dateTimeLayout), travel), keyboardForRole(user.Role))
}

func (b *Bot) askService(ctx context.Context, chatID int64, user UserRecord, selected []int) error {
	intro, _ := b.store.MasterIntro(ctx, user.TelegramID)
	services, err := b.store.ListServices(ctx, user.TelegramID)
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "services_list_failed"))
	}
	if len(services) == 0 {
		return b.sendText(ctx, chatID, tr(user.Language, "services_empty"))
	}
	state := ConversationState{Step: conversationStepService, ServiceIndexes: selected}
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	var sb strings.Builder
	if strings.TrimSpace(intro) != "" {
		sb.WriteString(intro)
		sb.WriteString("\n\n")
	}
	if len(selected) > 0 {
		sb.WriteString(tr(user.Language, "selected_services", formatIndexes(selected)))
		sb.WriteString("\n\n")
	}
	sb.WriteString(formatServices(user.Language, services))
	sb.WriteString("\n")
	sb.WriteString(tr(user.Language, "ask_service"))
	return b.sendTextWithKeyboard(ctx, chatID, sb.String(), numberKeyboard(len(services)))
}

func (b *Bot) showInteractiveSlots(ctx context.Context, chatID int64, user UserRecord, state ConversationState, slots []AvailabilitySlot) error {
	if len(slots) == 0 {
		state.Step = conversationStepTimeChoice
		_ = b.store.SetConversationState(ctx, user.TelegramID, state)
		return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "free_empty_try_other"), timeChoiceKeyboard(user.Language))
	}
	state.Step = conversationStepSlot
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}

	var sb strings.Builder
	sb.WriteString(tr(user.Language, "choose_slot_number"))
	sb.WriteString("\n")
	limit := len(slots)
	if limit > 20 {
		limit = 20
	}
	for i := 0; i < limit; i++ {
		sb.WriteString(strconv.Itoa(i + 1))
		sb.WriteString(". ")
		sb.WriteString(slots[i].StartAt.Format(dateTimeLayout))
		sb.WriteString(" - ")
		sb.WriteString(slots[i].EndAt.Format("15:04"))
		if slots[i].AdminName != "" {
			sb.WriteString(" (@")
			sb.WriteString(slots[i].AdminName)
			sb.WriteString(")")
		}
		sb.WriteString("\n")
	}
	if len(slots) > limit {
		sb.WriteString(tr(user.Language, "free_more", len(slots)-limit))
	}
	return b.sendTextWithKeyboard(ctx, chatID, sb.String(), numberKeyboard(limit))
}

func formatServices(lang string, services []ServiceView) string {
	var sb strings.Builder
	sb.WriteString(tr(lang, "services_header"))
	for i, service := range services {
		sb.WriteString(strconv.Itoa(i + 1))
		sb.WriteString(". ")
		sb.WriteString(service.Name)
		sb.WriteString(" - ")
		sb.WriteString(strconv.Itoa(service.DurationMin))
		sb.WriteString(" ")
		sb.WriteString(tr(lang, "minutes_short"))
		if service.AdminName != "" {
			sb.WriteString(" (@")
			sb.WriteString(service.AdminName)
			sb.WriteString(")")
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func parseLanguageChoice(text string) (string, bool) {
	switch normalizeChoice(text) {
	case "ru", "russian":
		return LangRU, true
	case "en", "english":
		return LangEN, true
	default:
		return "", false
	}
}

func normalizeChoice(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	switch text {
	case "русский", "ru", "рус", "russian":
		return "ru"
	case "english", "английский", "en", "eng":
		return "en"
	case "да", "yes", "y", "д":
		return "yes"
	case "нет", "no", "n", "н":
		return "no"
	case "ближайшее время", "ближайшее", "nearest", "soon":
		return "nearest"
	case "конкретные даты", "даты", "dates", "date":
		return "dates"
	default:
		return text
	}
}

func isYes(lang, text string) bool {
	return normalizeChoice(text) == "yes"
}

func isNo(lang, text string) bool {
	return normalizeChoice(text) == "no"
}

func parseDateList(text string, now time.Time) ([]time.Time, error) {
	parts := strings.Split(text, ",")
	out := make([]time.Time, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		parsed, err := parseUserDate(part, now)
		if err != nil {
			return nil, err
		}
		out = append(out, parsed)
	}
	return out, nil
}

func parseUserDate(text string, now time.Time) (time.Time, error) {
	layouts := []string{"2006-01-02", "02.01.2006"}
	for _, layout := range layouts {
		if parsed, err := time.ParseInLocation(layout, text, time.Local); err == nil {
			return dateOnly(parsed), nil
		}
	}
	parsed, err := time.ParseInLocation("02.01", text, time.Local)
	if err == nil {
		return time.Date(now.Year(), parsed.Month(), parsed.Day(), 0, 0, 0, 0, time.Local), nil
	}
	return time.Time{}, fmt.Errorf("bad date")
}

func dateOnly(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, value.Location())
}

func formatIndexes(values []int) string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, strconv.Itoa(value))
	}
	return strings.Join(out, ", ")
}

func languageKeyboard() *telegram.ReplyMarkup {
	return &telegram.ReplyMarkup{
		ResizeKeyboard: true,
		Keyboard: [][]telegram.KeyboardButton{
			{{Text: "Русский"}, {Text: "English"}},
		},
	}
}

func yesNoKeyboard(lang string) *telegram.ReplyMarkup {
	return &telegram.ReplyMarkup{
		ResizeKeyboard: true,
		Keyboard: [][]telegram.KeyboardButton{
			{{Text: tr(lang, "yes")}, {Text: tr(lang, "no")}},
		},
	}
}

func timeChoiceKeyboard(lang string) *telegram.ReplyMarkup {
	return &telegram.ReplyMarkup{
		ResizeKeyboard: true,
		Keyboard: [][]telegram.KeyboardButton{
			{{Text: tr(lang, "nearest_time")}, {Text: tr(lang, "specific_dates")}},
		},
	}
}

func numberKeyboard(count int) *telegram.ReplyMarkup {
	if count <= 0 {
		return nil
	}
	if count > 12 {
		count = 12
	}
	rows := make([][]telegram.KeyboardButton, 0, (count+2)/3)
	for i := 1; i <= count; i += 3 {
		var row []telegram.KeyboardButton
		for j := i; j < i+3 && j <= count; j++ {
			row = append(row, telegram.KeyboardButton{Text: strconv.Itoa(j)})
		}
		rows = append(rows, row)
	}
	return &telegram.ReplyMarkup{ResizeKeyboard: true, Keyboard: rows}
}
