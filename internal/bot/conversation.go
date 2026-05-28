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
	case conversationStepCategory:
		return true, b.conversationCategory(ctx, chatID, user, state, text)
	case conversationStepSubcategory:
		return true, b.conversationSubcategory(ctx, chatID, user, state, text)
	case conversationStepService:
		return true, b.conversationService(ctx, chatID, user, state, text)
	case conversationStepMore:
		return true, b.conversationMore(ctx, chatID, user, state, text)
	case conversationStepTimeChoice:
		return true, b.conversationTimeChoice(ctx, chatID, user, state, text)
	case conversationStepDates:
		return true, b.conversationDates(ctx, chatID, user, state, text)
	case conversationStepSlot:
		return true, b.conversationSlot(ctx, chatID, user, state, text)
	case conversationStepAddSvcCat:
		return true, b.conversationAddServiceCategory(ctx, chatID, user, state, text)
	case conversationStepAddSvcSub:
		return true, b.conversationAddServiceSubcategory(ctx, chatID, user, state, text)
	case conversationStepAddSvcName:
		return true, b.conversationAddServiceName(ctx, chatID, user, state, text)
	case conversationStepAddSvcDur:
		return true, b.conversationAddServiceDuration(ctx, chatID, user, state, text)
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
	return b.askCategory(ctx, chatID, user, nil)
}

func (b *Bot) conversationCategory(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	index, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || index <= 0 {
		return b.askCategory(ctx, chatID, user, state.ServiceIndexes)
	}
	services, err := b.store.ListServices(ctx, user.TelegramID)
	if err != nil {
		b.logger.Printf("conversation category: list services failed user=%d: %v", user.TelegramID, err)
		return b.sendText(ctx, chatID, tr(user.Language, "services_list_failed"))
	}
	categories := serviceCategories(services)
	if index > len(categories) {
		return b.askCategory(ctx, chatID, user, state.ServiceIndexes)
	}
	state.Category = categories[index-1]
	return b.askSubcategory(ctx, chatID, user, state)
}

func (b *Bot) conversationSubcategory(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	index, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || index <= 0 {
		return b.askSubcategory(ctx, chatID, user, state)
	}
	services, err := b.store.ListServices(ctx, user.TelegramID)
	if err != nil {
		b.logger.Printf("conversation subcategory: list services failed user=%d category=%q: %v", user.TelegramID, state.Category, err)
		return b.sendText(ctx, chatID, tr(user.Language, "services_list_failed"))
	}
	subcategories := serviceSubcategories(services, state.Category)
	if index > len(subcategories) {
		return b.askSubcategory(ctx, chatID, user, state)
	}
	state.Subcategory = subcategories[index-1]
	return b.askService(ctx, chatID, user, state)
}

func (b *Bot) conversationService(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	index, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || index <= 0 {
		return b.askService(ctx, chatID, user, state)
	}
	services, err := b.store.ListServices(ctx, user.TelegramID)
	if err != nil {
		b.logger.Printf("conversation service: list services failed user=%d category=%q subcategory=%q: %v", user.TelegramID, state.Category, state.Subcategory, err)
		return b.sendText(ctx, chatID, tr(user.Language, "services_list_failed"))
	}
	visibleIndexes := state.VisibleServiceIndexes
	if len(visibleIndexes) == 0 {
		visibleIndexes = serviceIndexesForPath(services, state.Category, state.Subcategory)
	}
	if index > len(visibleIndexes) {
		return b.askService(ctx, chatID, user, state)
	}
	globalIndex := visibleIndexes[index-1]
	state.ServiceIndexes = append(state.ServiceIndexes, globalIndex)
	state.VisibleServiceIndexes = nil
	state.Category = ""
	state.Subcategory = ""
	state.Step = conversationStepMore
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		b.logger.Printf("conversation service: save state failed user=%d: %v", user.TelegramID, err)
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "ask_more_services"), yesNoKeyboard(user.Language))
}

func (b *Bot) conversationMore(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	if isYes(user.Language, text) {
		return b.askCategory(ctx, chatID, user, state.ServiceIndexes)
	}
	if isNo(user.Language, text) {
		state.Step = conversationStepTimeChoice
		if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
			b.logger.Printf("conversation more: save state failed user=%d: %v", user.TelegramID, err)
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
			b.logger.Printf("conversation nearest: list slots failed user=%d services=%v: %v", user.TelegramID, state.ServiceIndexes, err)
			return b.sendText(ctx, chatID, tr(user.Language, "free_failed"))
		}
		b.logger.Printf("conversation nearest: user=%d services=%v slots=%d", user.TelegramID, state.ServiceIndexes, len(slots))
		return b.showInteractiveSlots(ctx, chatID, user, state, slots)
	}
	if normalized == "dates" {
		state.Step = conversationStepDates
		if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
			b.logger.Printf("conversation dates choice: save state failed user=%d: %v", user.TelegramID, err)
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
		b.logger.Printf("conversation dates: list slots failed user=%d services=%v dates=%v: %v", user.TelegramID, state.ServiceIndexes, dates, err)
		return b.sendText(ctx, chatID, tr(user.Language, "free_failed"))
	}
	b.logger.Printf("conversation dates: user=%d services=%v dates=%d slots=%d", user.TelegramID, state.ServiceIndexes, len(dates), len(slots))
	return b.showInteractiveSlots(ctx, chatID, user, state, slots)
}

func (b *Bot) conversationSlot(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	index, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || index <= 0 {
		return b.sendText(ctx, chatID, tr(user.Language, "choose_slot_number"))
	}
	if len(state.VisibleSlotIndexes) > 0 {
		if index > len(state.VisibleSlotIndexes) {
			return b.sendText(ctx, chatID, tr(user.Language, "choose_slot_number"))
		}
		index = state.VisibleSlotIndexes[index-1]
	}
	start, err := b.store.BookForUserByIndex(ctx, user.TelegramID, index)
	if err != nil {
		b.logger.Printf("conversation slot: book failed user=%d index=%d: %v", user.TelegramID, index, err)
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalidArgument) {
			return b.sendText(ctx, chatID, tr(user.Language, "book_need_schedule"))
		}
		return b.sendText(ctx, chatID, tr(user.Language, "book_failed"))
	}
	b.logger.Printf("conversation slot: booked user=%d index=%d start=%s", user.TelegramID, index, start.Format(time.RFC3339))
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "book_ok", start.Format(dateTimeLayout)), keyboardForRole(user.Role))
}

func (b *Bot) conversationAddServiceCategory(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	state.Category = normalizeOptionalText(text)
	state.Step = conversationStepAddSvcSub
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	return b.sendText(ctx, chatID, tr(user.Language, "service_add_ask_subcategory"))
}

func (b *Bot) conversationAddServiceSubcategory(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	state.Subcategory = normalizeOptionalText(text)
	state.Step = conversationStepAddSvcName
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	return b.sendText(ctx, chatID, tr(user.Language, "service_add_ask_name"))
}

func (b *Bot) conversationAddServiceName(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	name := strings.TrimSpace(text)
	if name == "" || name == "-" {
		return b.sendText(ctx, chatID, tr(user.Language, "service_add_ask_name"))
	}
	state.ServiceName = name
	state.Step = conversationStepAddSvcDur
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	return b.sendText(ctx, chatID, tr(user.Language, "service_add_ask_duration"))
}

func (b *Bot) conversationAddServiceDuration(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	duration, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || duration <= 0 {
		return b.sendText(ctx, chatID, tr(user.Language, "duration_bad"))
	}
	path := servicePathFromState(state)
	if err := b.store.AddService(ctx, user.TelegramID, path, duration, ""); err != nil {
		b.logger.Printf("interactive service add failed admin=%d path=%q duration=%d: %v", user.TelegramID, path, duration, err)
		return b.sendText(ctx, chatID, tr(user.Language, "service_add_failed"))
	}
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	b.logger.Printf("interactive service added admin=%d path=%q duration=%d", user.TelegramID, path, duration)
	return b.sendText(ctx, chatID, tr(user.Language, "service_add_ok", path, duration))
}

func (b *Bot) askCategory(ctx context.Context, chatID int64, user UserRecord, selected []int) error {
	intro, _ := b.store.MasterIntro(ctx, user.TelegramID)
	services, err := b.store.ListServices(ctx, user.TelegramID)
	if err != nil {
		b.logger.Printf("ask category: list services failed user=%d: %v", user.TelegramID, err)
		return b.sendText(ctx, chatID, tr(user.Language, "services_list_failed"))
	}
	if len(services) == 0 {
		return b.sendText(ctx, chatID, tr(user.Language, "services_empty"))
	}
	categories := serviceCategories(services)
	state := ConversationState{Step: conversationStepCategory, ServiceIndexes: selected}
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		b.logger.Printf("ask category: save state failed user=%d: %v", user.TelegramID, err)
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
	sb.WriteString(formatCategories(user.Language, categories))
	sb.WriteString("\n")
	sb.WriteString(tr(user.Language, "ask_category"))
	return b.sendTextWithKeyboard(ctx, chatID, sb.String(), numberKeyboardWithPrefix(len(categories), "cat"))
}

func (b *Bot) askSubcategory(ctx context.Context, chatID int64, user UserRecord, state ConversationState) error {
	services, err := b.store.ListServices(ctx, user.TelegramID)
	if err != nil {
		b.logger.Printf("ask subcategory: list services failed user=%d category=%q: %v", user.TelegramID, state.Category, err)
		return b.sendText(ctx, chatID, tr(user.Language, "services_list_failed"))
	}
	subcategories := serviceSubcategories(services, state.Category)
	if len(subcategories) == 0 {
		return b.askService(ctx, chatID, user, state)
	}
	if len(subcategories) == 1 && subcategories[0] == "" {
		state.Subcategory = ""
		return b.askService(ctx, chatID, user, state)
	}
	state.Step = conversationStepSubcategory
	state.Subcategory = ""
	state.VisibleServiceIndexes = nil
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		b.logger.Printf("ask subcategory: save state failed user=%d: %v", user.TelegramID, err)
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	var sb strings.Builder
	sb.WriteString(tr(user.Language, "selected_category", displayCategory(user.Language, state.Category)))
	sb.WriteString("\n")
	sb.WriteString(formatSubcategories(user.Language, subcategories))
	sb.WriteString("\n")
	sb.WriteString(tr(user.Language, "ask_subcategory"))
	return b.sendTextWithKeyboard(ctx, chatID, sb.String(), numberKeyboardWithPrefix(len(subcategories), "sub"))
}

func (b *Bot) askService(ctx context.Context, chatID int64, user UserRecord, state ConversationState) error {
	services, err := b.store.ListServices(ctx, user.TelegramID)
	if err != nil {
		b.logger.Printf("ask service: list services failed user=%d category=%q subcategory=%q: %v", user.TelegramID, state.Category, state.Subcategory, err)
		return b.sendText(ctx, chatID, tr(user.Language, "services_list_failed"))
	}
	visibleIndexes := serviceIndexesForPath(services, state.Category, state.Subcategory)
	if len(visibleIndexes) == 0 {
		return b.askCategory(ctx, chatID, user, state.ServiceIndexes)
	}
	state.Step = conversationStepService
	state.VisibleServiceIndexes = visibleIndexes
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		b.logger.Printf("ask service: save state failed user=%d: %v", user.TelegramID, err)
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	visibleServices := make([]ServiceView, 0, len(visibleIndexes))
	for _, globalIndex := range visibleIndexes {
		visibleServices = append(visibleServices, services[globalIndex-1])
	}
	var sb strings.Builder
	sb.WriteString(tr(user.Language, "selected_category", displayCategory(user.Language, state.Category)))
	if state.Subcategory != "" {
		sb.WriteString("\n")
		sb.WriteString(tr(user.Language, "selected_subcategory", state.Subcategory))
	}
	sb.WriteString("\n")
	sb.WriteString(formatServicesList(user.Language, visibleServices, false))
	sb.WriteString("\n")
	sb.WriteString(tr(user.Language, "ask_service"))
	return b.sendTextWithKeyboard(ctx, chatID, sb.String(), numberKeyboardWithPrefix(len(visibleServices), "svc"))
}

func (b *Bot) showInteractiveSlots(ctx context.Context, chatID int64, user UserRecord, state ConversationState, slots []AvailabilitySlot) error {
	if len(slots) == 0 {
		state.Step = conversationStepTimeChoice
		_ = b.store.SetConversationState(ctx, user.TelegramID, state)
		return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "free_empty_try_other"), timeChoiceKeyboard(user.Language))
	}
	state.Step = conversationStepSlot
	if state.SlotPeriod == "" {
		state.SlotPeriod = firstSlotPeriod(slots)
	}
	state.SlotDay = chooseSlotDayForPeriod(slots, state.SlotDay, state.SlotPeriod)
	text, kb, nextState := renderSlotBrowser(user.Language, state, slots)
	if err := b.store.SetConversationState(ctx, user.TelegramID, nextState); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	return b.sendTextWithKeyboard(ctx, chatID, text, kb)
}

func formatServices(lang string, services []ServiceView) string {
	return formatServicesList(lang, services, true)
}

func formatServicesList(lang string, services []ServiceView, includePath bool) string {
	var sb strings.Builder
	sb.WriteString(tr(lang, "services_header"))
	lastCategory := "\x00"
	lastSubcategory := "\x00"
	for i, service := range services {
		if includePath {
			if service.Category != lastCategory {
				lastCategory = service.Category
				lastSubcategory = "\x00"
				sb.WriteString("\n")
				sb.WriteString(displayCategory(lang, service.Category))
				sb.WriteString("\n")
			}
			if service.Subcategory != lastSubcategory {
				lastSubcategory = service.Subcategory
				if service.Subcategory != "" {
					sb.WriteString("  ")
					sb.WriteString(service.Subcategory)
					sb.WriteString("\n")
				}
			}
		}
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

func normalizeOptionalText(text string) string {
	text = strings.TrimSpace(text)
	if text == "-" || strings.EqualFold(text, "нет") || strings.EqualFold(text, "no") {
		return ""
	}
	return text
}

func servicePathFromState(state ConversationState) string {
	var parts []string
	if state.Category != "" {
		parts = append(parts, state.Category)
	}
	if state.Subcategory != "" {
		parts = append(parts, state.Subcategory)
	}
	parts = append(parts, state.ServiceName)
	return strings.Join(parts, " > ")
}

func formatCategories(lang string, categories []string) string {
	var sb strings.Builder
	sb.WriteString(tr(lang, "categories_header"))
	for i, category := range categories {
		sb.WriteString(strconv.Itoa(i + 1))
		sb.WriteString(". ")
		sb.WriteString(displayCategory(lang, category))
		sb.WriteString("\n")
	}
	return sb.String()
}

func formatSubcategories(lang string, subcategories []string) string {
	var sb strings.Builder
	sb.WriteString(tr(lang, "subcategories_header"))
	for i, subcategory := range subcategories {
		sb.WriteString(strconv.Itoa(i + 1))
		sb.WriteString(". ")
		if subcategory == "" {
			sb.WriteString(tr(lang, "without_subcategory"))
		} else {
			sb.WriteString(subcategory)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func formatAvailabilitySlots(lang string, slots []AvailabilitySlot, limit int) string {
	limit = minInt(limit, len(slots))
	var sb strings.Builder
	currentDay := ""
	for i := 0; i < limit; i++ {
		day := dayHeader(lang, slots[i].StartAt)
		if day != currentDay {
			currentDay = day
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(day)
			sb.WriteString("\n")
		}
		sb.WriteString(strconv.Itoa(i + 1))
		sb.WriteString(". ")
		sb.WriteString(slots[i].StartAt.Format("15:04"))
		sb.WriteString("-")
		sb.WriteString(slots[i].EndAt.Format("15:04"))
		if slots[i].AdminName != "" {
			sb.WriteString(" @")
			sb.WriteString(slots[i].AdminName)
		}
		sb.WriteString("\n")
	}
	if len(slots) > limit {
		sb.WriteString("\n")
		sb.WriteString(tr(lang, "slots_more_hint", len(slots)-limit))
	}
	return sb.String()
}

func renderSlotBrowser(lang string, state ConversationState, slots []AvailabilitySlot) (string, *telegram.ReplyMarkup, ConversationState) {
	state.Step = conversationStepSlot
	if state.SlotPeriod == "" {
		state.SlotPeriod = firstSlotPeriod(slots)
	}
	if state.SlotDay == "" {
		state.SlotDay = chooseSlotDayForPeriod(slots, "", state.SlotPeriod)
	}
	visible := visibleSlotsForDayPeriod(slots, state.SlotDay, state.SlotPeriod)
	if len(visible) == 0 {
		state.SlotDay = chooseSlotDayForPeriod(slots, state.SlotDay, state.SlotPeriod)
		visible = visibleSlotsForDayPeriod(slots, state.SlotDay, state.SlotPeriod)
	}
	state.VisibleSlotIndexes = make([]int, 0, len(visible))

	var sb strings.Builder
	sb.WriteString(tr(lang, "choose_slot_number"))
	sb.WriteString("\n")
	if state.SlotDay != "" {
		if day, err := time.Parse("2006-01-02", state.SlotDay); err == nil {
			sb.WriteString(dayHeader(lang, day))
			sb.WriteString(" - ")
			sb.WriteString(tr(lang, "slot_period_"+state.SlotPeriod))
			sb.WriteString("\n")
		}
	}
	if len(visible) == 0 {
		sb.WriteString(tr(lang, "slot_day_empty"))
	} else {
		for i, item := range visible {
			state.VisibleSlotIndexes = append(state.VisibleSlotIndexes, item.index+1)
			slot := slots[item.index]
			sb.WriteString(strconv.Itoa(i + 1))
			sb.WriteString(". ")
			sb.WriteString(slot.StartAt.Format("15:04"))
			sb.WriteString("-")
			sb.WriteString(slot.EndAt.Format("15:04"))
			if slot.AdminName != "" {
				sb.WriteString(" @")
				sb.WriteString(slot.AdminName)
			}
			sb.WriteString("\n")
		}
	}
	return sb.String(), slotBrowserKeyboard(lang, len(visible)), state
}

type indexedSlot struct {
	index int
}

func visibleSlotsForDayPeriod(slots []AvailabilitySlot, day, period string) []indexedSlot {
	var out []indexedSlot
	for i, slot := range slots {
		if slot.StartAt.Format("2006-01-02") != day {
			continue
		}
		if !slotMatchesPeriod(slot.StartAt, period) {
			continue
		}
		out = append(out, indexedSlot{index: i})
	}
	return out
}

func chooseSlotDayForPeriod(slots []AvailabilitySlot, currentDay, period string) string {
	days := slotDaysForPeriod(slots, period)
	if len(days) == 0 {
		return ""
	}
	if currentDay == "" {
		return days[0]
	}
	for _, day := range days {
		if day >= currentDay {
			return day
		}
	}
	return days[0]
}

func moveSlotDay(slots []AvailabilitySlot, currentDay, period, direction string) string {
	days := slotDaysForPeriod(slots, period)
	if len(days) == 0 {
		return ""
	}
	idx := 0
	for i, day := range days {
		if day == currentDay {
			idx = i
			break
		}
	}
	switch direction {
	case "prev":
		if idx > 0 {
			idx--
		}
	case "next":
		if idx < len(days)-1 {
			idx++
		}
	}
	return days[idx]
}

func slotDaysForPeriod(slots []AvailabilitySlot, period string) []string {
	seen := map[string]bool{}
	var days []string
	for _, slot := range slots {
		if !slotMatchesPeriod(slot.StartAt, period) {
			continue
		}
		day := slot.StartAt.Format("2006-01-02")
		if !seen[day] {
			seen[day] = true
			days = append(days, day)
		}
	}
	return days
}

func slotMatchesPeriod(value time.Time, period string) bool {
	hour := value.Hour()
	switch period {
	case "morning":
		return hour < 12
	case "day":
		return hour >= 12 && hour < 17
	case "evening":
		return hour >= 17
	default:
		return true
	}
}

func firstSlotPeriod(slots []AvailabilitySlot) string {
	if len(slots) == 0 {
		return "all"
	}
	hour := slots[0].StartAt.Hour()
	switch {
	case hour < 12:
		return "morning"
	case hour < 17:
		return "day"
	default:
		return "evening"
	}
}

func slotBrowserKeyboard(lang string, count int) *telegram.ReplyMarkup {
	var rows [][]telegram.InlineKeyboardButton
	for i := 1; i <= count; i += 3 {
		var row []telegram.InlineKeyboardButton
		for j := i; j < i+3 && j <= count; j++ {
			value := strconv.Itoa(j)
			row = append(row, telegram.InlineKeyboardButton{Text: value, CallbackData: "slot:" + value})
		}
		rows = append(rows, row)
	}
	rows = append(rows, []telegram.InlineKeyboardButton{
		{Text: tr(lang, "slot_period_all"), CallbackData: "slotperiod:all"},
		{Text: tr(lang, "slot_period_morning"), CallbackData: "slotperiod:morning"},
	})
	rows = append(rows, []telegram.InlineKeyboardButton{
		{Text: tr(lang, "slot_period_day"), CallbackData: "slotperiod:day"},
		{Text: tr(lang, "slot_period_evening"), CallbackData: "slotperiod:evening"},
	})
	rows = append(rows, []telegram.InlineKeyboardButton{
		{Text: tr(lang, "slot_prev_day"), CallbackData: "slotday:prev"},
		{Text: tr(lang, "slot_next_day"), CallbackData: "slotday:next"},
	})
	return &telegram.ReplyMarkup{InlineKeyboard: rows}
}

func formatTimeSlots(lang string, slots []time.Time, limit int) string {
	limit = minInt(limit, len(slots))
	var sb strings.Builder
	currentDay := ""
	for i := 0; i < limit; i++ {
		day := dayHeader(lang, slots[i])
		if day != currentDay {
			currentDay = day
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(day)
			sb.WriteString("\n")
		}
		sb.WriteString(strconv.Itoa(i + 1))
		sb.WriteString(". ")
		sb.WriteString(slots[i].Format("15:04"))
		sb.WriteString("\n")
	}
	if len(slots) > limit {
		sb.WriteString("\n")
		sb.WriteString(tr(lang, "slots_more_hint", len(slots)-limit))
	}
	return sb.String()
}

func dayHeader(lang string, value time.Time) string {
	return value.Format("02.01") + " " + weekdayShort(lang, value.Weekday())
}

func weekdayShort(lang string, weekday time.Weekday) string {
	if lang == LangEN {
		switch weekday {
		case time.Monday:
			return "Mon"
		case time.Tuesday:
			return "Tue"
		case time.Wednesday:
			return "Wed"
		case time.Thursday:
			return "Thu"
		case time.Friday:
			return "Fri"
		case time.Saturday:
			return "Sat"
		default:
			return "Sun"
		}
	}
	switch weekday {
	case time.Monday:
		return "Пн"
	case time.Tuesday:
		return "Вт"
	case time.Wednesday:
		return "Ср"
	case time.Thursday:
		return "Чт"
	case time.Friday:
		return "Пт"
	case time.Saturday:
		return "Сб"
	default:
		return "Вс"
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func serviceCategories(services []ServiceView) []string {
	seen := map[string]bool{}
	var out []string
	for _, service := range services {
		key := service.Category
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	return out
}

func serviceSubcategories(services []ServiceView, category string) []string {
	seen := map[string]bool{}
	var out []string
	for _, service := range services {
		if service.Category != category {
			continue
		}
		key := service.Subcategory
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	return out
}

func serviceIndexesForPath(services []ServiceView, category, subcategory string) []int {
	var out []int
	for i, service := range services {
		if service.Category == category && service.Subcategory == subcategory {
			out = append(out, i+1)
		}
	}
	return out
}

func serviceDisplayName(service ServiceView) string {
	var parts []string
	if service.Category != "" {
		parts = append(parts, service.Category)
	}
	if service.Subcategory != "" {
		parts = append(parts, service.Subcategory)
	}
	parts = append(parts, service.Name)
	if len(parts) == 1 {
		return parts[0]
	}
	return strings.Join(parts, " > ")
}

func displayCategory(lang, category string) string {
	if category == "" {
		return tr(lang, "without_category")
	}
	return category
}

func formatCalendar(lang string, monthStart time.Time, items []CalendarDay) string {
	monthStart = time.Date(monthStart.Year(), monthStart.Month(), 1, 0, 0, 0, 0, monthStart.Location())
	byDay := make(map[int]CalendarDay, len(items))
	for _, item := range items {
		byDay[item.Date.Day()] = item
	}

	var sb strings.Builder
	sb.WriteString(tr(lang, "calendar_title", monthStart.Format(monthLayout)))
	sb.WriteString("\n")
	sb.WriteString(tr(lang, "calendar_weekdays"))
	sb.WriteString("\n")

	firstWeekday := int(monthStart.Weekday())
	if firstWeekday == 0 {
		firstWeekday = 7
	}
	for i := 1; i < firstWeekday; i++ {
		sb.WriteString("    ")
	}
	monthEnd := monthStart.AddDate(0, 1, 0)
	for day := monthStart; day.Before(monthEnd); day = day.AddDate(0, 0, 1) {
		item := byDay[day.Day()]
		sb.WriteString(fmt.Sprintf("%2d%s ", day.Day(), calendarMarker(item)))
		if day.Weekday() == time.Sunday {
			sb.WriteString("\n")
		}
	}
	if !strings.HasSuffix(sb.String(), "\n") {
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	sb.WriteString(tr(lang, "calendar_legend"))
	sb.WriteString("\n")

	detailCount := 0
	for day := monthStart; day.Before(monthEnd); day = day.AddDate(0, 0, 1) {
		item := byDay[day.Day()]
		if item.TotalSlots == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("%02d: %s\n", day.Day(), tr(lang, "calendar_day_summary", item.OpenSlots, item.Booked, item.Blocked, item.Closed)))
		detailCount++
		if detailCount >= 20 {
			sb.WriteString(tr(lang, "calendar_more_days"))
			sb.WriteString("\n")
			break
		}
	}
	if detailCount == 0 {
		sb.WriteString(tr(lang, "calendar_empty"))
	}
	return sb.String()
}

func calendarMarker(item CalendarDay) string {
	switch {
	case item.TotalSlots == 0:
		return "."
	case item.OpenSlots > 0:
		if item.OpenSlots > 9 {
			return "+"
		}
		return strconv.Itoa(item.OpenSlots)
	default:
		return "x"
	}
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
		InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{
				{Text: "Русский", CallbackData: "lang:ru"},
				{Text: "English", CallbackData: "lang:en"},
			},
		},
	}
}

func yesNoKeyboard(lang string) *telegram.ReplyMarkup {
	return &telegram.ReplyMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{
				{Text: tr(lang, "yes"), CallbackData: "more:yes"},
				{Text: tr(lang, "no"), CallbackData: "more:no"},
			},
		},
	}
}

func timeChoiceKeyboard(lang string) *telegram.ReplyMarkup {
	return &telegram.ReplyMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{
				{Text: tr(lang, "nearest_time"), CallbackData: "time:nearest"},
				{Text: tr(lang, "specific_dates"), CallbackData: "time:dates"},
			},
		},
	}
}

func numberKeyboard(count int) *telegram.ReplyMarkup {
	return numberKeyboardWithPrefix(count, "slot")
}

func numberKeyboardWithPrefix(count int, prefix string) *telegram.ReplyMarkup {
	if count <= 0 {
		return nil
	}
	if count > 12 {
		count = 12
	}
	rows := make([][]telegram.InlineKeyboardButton, 0, (count+2)/3)
	for i := 1; i <= count; i += 3 {
		var row []telegram.InlineKeyboardButton
		for j := i; j < i+3 && j <= count; j++ {
			value := strconv.Itoa(j)
			row = append(row, telegram.InlineKeyboardButton{Text: value, CallbackData: prefix + ":" + value})
		}
		rows = append(rows, row)
	}
	return &telegram.ReplyMarkup{InlineKeyboard: rows}
}
