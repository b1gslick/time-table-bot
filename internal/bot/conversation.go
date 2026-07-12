package bot

import (
	"context"
	"errors"
	"fmt"
	"sort"
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
	case conversationStepAddSvcDesc:
		return true, b.conversationAddServiceDescription(ctx, chatID, user, state, text)
	case conversationStepEditSvc:
		return true, b.conversationEditServiceIndex(ctx, chatID, user, state, text)
	case conversationStepEditSvcData:
		return true, b.conversationEditServiceData(ctx, chatID, user, state, text)
	case conversationStepDeleteSvc:
		return true, b.conversationDeleteService(ctx, chatID, user, text)
	case conversationStepSetProfile:
		return true, b.conversationSetProfile(ctx, chatID, user, text)
	case conversationStepSetServices:
		return true, b.conversationSetServices(ctx, chatID, user, text)
	case conversationStepCategoryOrd:
		return true, b.conversationCategoryOrder(ctx, chatID, user, text)
	case conversationStepSetHours:
		return true, b.conversationSetHours(ctx, chatID, user, text)
	case conversationStepSetHoursDay:
		return true, b.conversationSetHoursDay(ctx, chatID, user, state, text)
	case conversationStepSetDuration:
		return true, b.conversationSetDuration(ctx, chatID, user, text)
	case conversationStepGenMode:
		return true, b.conversationGenerateMode(ctx, chatID, user, text)
	case conversationStepGenMonth:
		return true, b.conversationGenerateMonth(ctx, chatID, user, state, text)
	case conversationStepGenMonths:
		return true, b.conversationGenerateMonths(ctx, chatID, user, state, text)
	case conversationStepGenDay:
		return true, b.conversationGenerateDay(ctx, chatID, user, state, text)
	case conversationStepGenWeekdays:
		return true, b.conversationGenerateWeekdays(ctx, chatID, user, state, text)
	case conversationStepGenDayRange:
		return true, b.conversationGenerateDayRange(ctx, chatID, user, state, text)
	case conversationStepGenDayStep:
		return true, b.conversationGenerateDayStep(ctx, chatID, user, state, text)
	case conversationStepGenWeekdaysRange:
		return true, b.conversationGenerateWeekdaysRange(ctx, chatID, user, state, text)
	case conversationStepGenWeekdaysStep:
		return true, b.conversationGenerateWeekdaysStep(ctx, chatID, user, state, text)
	case conversationStepDeleteMonth:
		return true, b.conversationDeleteScheduleMonth(ctx, chatID, user, text)
	case conversationStepAdminAdd:
		return true, b.conversationAdminAdd(ctx, chatID, user, text)
	case conversationStepAdminRemove:
		return true, b.conversationAdminRemove(ctx, chatID, user, text)
	case conversationStepViewAdmin:
		return true, b.conversationViewAdmin(ctx, chatID, user, text)
	case conversationStepRoleUser:
		return true, b.conversationRoleUser(ctx, chatID, user, state, text)
	case conversationStepRoleValue:
		return true, b.conversationRoleValue(ctx, chatID, user, state, text)
	case conversationStepAppointKind:
		return true, b.conversationAppointKind(ctx, chatID, user, state, text)
	case conversationStepAppointUser:
		return true, b.conversationAppointUser(ctx, chatID, user, state, text)
	case conversationStepAppointTime:
		return true, b.conversationAppointTime(ctx, chatID, user, state, text)
	case conversationStepCancelUser:
		return true, b.conversationCancelUser(ctx, chatID, user, state, text)
	case conversationStepCancelTime:
		return true, b.conversationCancelTime(ctx, chatID, user, state, text)
	case conversationStepReschUser:
		return true, b.conversationRescheduleUser(ctx, chatID, user, state, text)
	case conversationStepReschFrom:
		return true, b.conversationRescheduleFrom(ctx, chatID, user, state, text)
	case conversationStepReschTo:
		return true, b.conversationRescheduleTo(ctx, chatID, user, state, text)
	case conversationStepBlock:
		return true, b.conversationBlock(ctx, chatID, user, text)
	case conversationStepBlockDate:
		return true, b.conversationBlockDate(ctx, chatID, user, text)
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
		return b.askCategoryWithState(ctx, chatID, user, state)
	}
	services, err := b.store.ListServices(ctx, user.TelegramID)
	if err != nil {
		b.logger.Printf("conversation category: list services failed user=%d: %v", user.TelegramID, err)
		return b.sendText(ctx, chatID, tr(user.Language, "services_list_failed"))
	}
	categories := serviceCategories(services)
	if index > len(categories) {
		return b.askCategoryWithState(ctx, chatID, user, state)
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
	if intInSlice(state.ServiceIndexes, globalIndex) {
		state.VisibleServiceIndexes = nil
		state.Category = ""
		state.Subcategory = ""
		state.Step = conversationStepMore
		if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
			b.logger.Printf("conversation service duplicate: save state failed user=%d: %v", user.TelegramID, err)
			return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
		}
		return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "service_already_selected")+"\n"+tr(user.Language, "ask_more_services"), yesNoKeyboard(user.Language))
	}
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
		return b.askCategoryWithState(ctx, chatID, user, state)
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
		state = resetSlotBrowserState(state)
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
		state = resetSlotBrowserState(state)
		state.Step = conversationStepDates
		if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
			b.logger.Printf("conversation dates choice: save state failed user=%d: %v", user.TelegramID, err)
			return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
		}
		return b.sendText(ctx, chatID, tr(user.Language, "ask_dates"))
	}
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "ask_time_choice"), timeChoiceKeyboard(user.Language))
}

func resetSlotBrowserState(state ConversationState) ConversationState {
	state.SlotDay = ""
	state.SlotPeriod = ""
	state.VisibleSlotIndexes = nil
	return state
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
	var result BookingChangeResult
	if isAdminAppointmentState(state) {
		result, err = b.store.AddBookingForContactByIndex(ctx, user.TelegramID, state.ContactType, state.Username, index)
	} else {
		result, err = b.store.BookForUserByIndex(ctx, user.TelegramID, index)
	}
	if err != nil {
		b.logger.Printf("conversation slot: book failed user=%d index=%d: %v", user.TelegramID, index, err)
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalidArgument) {
			return b.sendText(ctx, chatID, tr(user.Language, "book_need_schedule"))
		}
		return b.sendText(ctx, chatID, tr(user.Language, "book_failed"))
	}
	b.logger.Printf("conversation slot: booked user=%d index=%d start=%s", user.TelegramID, index, result.StartAt.Format(time.RFC3339))
	b.notifyBookingChange(ctx, "created", result, chatID)
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	if isAdminAppointmentState(state) {
		if state.ContactType == "phone" {
			return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "appoint_ok_contact", state.Username), keyboardForUser(user))
		}
		return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "appoint_ok", state.Username), keyboardForUser(user))
	}
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "book_ok", result.StartAt.Format(dateTimeLayout)), keyboardForUser(user))
}

func (b *Bot) conversationBack(ctx context.Context, chatID int64, user UserRecord, state ConversationState) error {
	switch state.Step {
	case conversationStepCategory:
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "menu_main_text"), keyboardForUser(user))
	case conversationStepSubcategory:
		return b.askCategoryWithState(ctx, chatID, user, state)
	case conversationStepService:
		services, err := b.store.ListServices(ctx, user.TelegramID)
		if err != nil {
			return b.sendText(ctx, chatID, tr(user.Language, "services_list_failed"))
		}
		subcategories := serviceSubcategories(services, state.Category)
		if len(subcategories) > 1 || len(subcategories) == 1 && subcategories[0] != "" {
			state.Step = conversationStepSubcategory
			state.Subcategory = ""
			state.VisibleServiceIndexes = nil
			return b.askSubcategory(ctx, chatID, user, state)
		}
		return b.askCategoryWithState(ctx, chatID, user, state)
	case conversationStepMore:
		return b.askCategoryWithState(ctx, chatID, user, state)
	case conversationStepTimeChoice:
		return b.askCategory(ctx, chatID, user, state.ServiceIndexes)
	case conversationStepDates, conversationStepSlot:
		state.Step = conversationStepTimeChoice
		state.VisibleSlotIndexes = nil
		if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
			return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
		}
		return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "ask_time_choice"), timeChoiceKeyboard(user.Language))
	case conversationStepSetHoursDay:
		return b.conversationSetHoursDay(ctx, chatID, user, state, "back")
	case conversationStepAppointKind:
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "menu_bookings_text"), bookingMenuKeyboard(user.Language))
	case conversationStepAppointUser:
		state.Step = conversationStepAppointKind
		state.ContactType = ""
		state.Username = ""
		if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
			return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
		}
		return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "appoint_ask_contact_type"), contactTypeKeyboard(user.Language))
	case conversationStepAppointTime:
		state.Step = conversationStepAppointUser
		if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
			return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
		}
		if state.ContactType == "phone" {
			return b.sendText(ctx, chatID, tr(user.Language, "appoint_ask_phone"))
		}
		return b.sendText(ctx, chatID, tr(user.Language, "appoint_ask_username"))
	default:
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.handleStart(ctx, chatID, user)
	}
}

func (b *Bot) conversationAddServiceCategory(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	category, ok, err := b.resolveAdminServiceCategory(ctx, user, text)
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "services_list_failed"))
	}
	if !ok {
		return b.askServiceAddCategory(ctx, chatID, user, state)
	}
	state.Category = category
	state.Step = conversationStepAddSvcSub
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	return b.askServiceAddSubcategory(ctx, chatID, user, state)
}

func (b *Bot) conversationAddServiceSubcategory(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	subcategory, ok, err := b.resolveAdminServiceSubcategory(ctx, user, state.Category, text)
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "services_list_failed"))
	}
	if !ok {
		return b.askServiceAddSubcategory(ctx, chatID, user, state)
	}
	state.Subcategory = subcategory
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
	state.ServiceIndex = duration
	state.Step = conversationStepAddSvcDesc
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	return b.sendText(ctx, chatID, tr(user.Language, "service_add_ask_description"))
}

func (b *Bot) conversationAddServiceDescription(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	path := servicePathFromState(state)
	description := normalizeOptionalText(text)
	if err := b.store.AddService(ctx, user.TelegramID, path, state.ServiceIndex, description); err != nil {
		b.logger.Printf("interactive service add failed admin=%d path=%q duration=%d: %v", user.TelegramID, path, state.ServiceIndex, err)
		return b.sendText(ctx, chatID, tr(user.Language, "service_add_failed"))
	}
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	b.logger.Printf("interactive service added admin=%d path=%q duration=%d", user.TelegramID, path, state.ServiceIndex)
	return b.sendText(ctx, chatID, tr(user.Language, "service_add_ok", path, state.ServiceIndex))
}

func (b *Bot) conversationDeleteService(ctx context.Context, chatID int64, user UserRecord, text string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	index, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || index <= 0 {
		return b.sendText(ctx, chatID, tr(user.Language, "service_delete_ask_index"))
	}
	if err := b.store.DeleteServiceByIndex(ctx, user.TelegramID, index); err != nil {
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalidArgument) {
			return b.sendText(ctx, chatID, tr(user.Language, "service_delete_bad_index"))
		}
		b.logger.Printf("interactive service delete failed admin=%d index=%d: %v", user.TelegramID, index, err)
		return b.sendText(ctx, chatID, tr(user.Language, "service_delete_failed"))
	}
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "service_delete_ok", index), keyboardForUser(user))
}

func (b *Bot) conversationEditServiceIndex(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	index, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || index <= 0 {
		return b.sendText(ctx, chatID, tr(user.Language, "service_edit_ask_index"))
	}
	services, err := b.store.ListServices(ctx, user.TelegramID)
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "services_list_failed"))
	}
	if index > len(services) {
		return b.sendText(ctx, chatID, tr(user.Language, "service_edit_bad_index"))
	}
	state.ServiceIndex = index
	state.Step = conversationStepEditSvcData
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	return b.sendText(ctx, chatID, tr(user.Language, "service_edit_ask_data"))
}

func (b *Bot) conversationEditServiceData(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	duration, name, description, ok := parseServiceEditData(text)
	if !ok {
		return b.sendText(ctx, chatID, tr(user.Language, "service_edit_ask_data"))
	}
	if err := b.store.EditServiceByIndex(ctx, user.TelegramID, state.ServiceIndex, name, duration, description); err != nil {
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalidArgument) {
			return b.sendText(ctx, chatID, tr(user.Language, "service_edit_bad_index"))
		}
		b.logger.Printf("interactive service edit failed admin=%d index=%d duration=%d name=%q: %v", user.TelegramID, state.ServiceIndex, duration, name, err)
		return b.sendText(ctx, chatID, tr(user.Language, "service_edit_failed"))
	}
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "service_edit_ok", state.ServiceIndex), keyboardForUser(user))
}

func (b *Bot) conversationSetProfile(ctx context.Context, chatID int64, user UserRecord, text string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	value := strings.TrimSpace(text)
	if value == "" {
		return b.sendText(ctx, chatID, tr(user.Language, "profile_ask_text"))
	}
	if err := b.store.SetProfileText(ctx, user.TelegramID, value); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "profile_failed"))
	}
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "profile_ok"), keyboardForUser(user))
}

func (b *Bot) conversationSetServices(ctx context.Context, chatID int64, user UserRecord, text string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	value := strings.TrimSpace(text)
	if value == "" {
		return b.sendText(ctx, chatID, tr(user.Language, "services_ask_text"))
	}
	if err := b.store.SetServicesText(ctx, user.TelegramID, value); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "services_failed"))
	}
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "services_ok"), keyboardForUser(user))
}

func (b *Bot) conversationCategoryOrder(ctx context.Context, chatID int64, user UserRecord, text string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	services, err := b.store.ListServices(ctx, user.TelegramID)
	if err != nil {
		b.logger.Printf("conversation category order list failed admin=%d: %v", user.TelegramID, err)
		return b.sendText(ctx, chatID, tr(user.Language, "services_list_failed"))
	}
	categories := serviceCategories(services)
	return b.applyCategoryOrder(ctx, chatID, user, categories, text)
}

func (b *Bot) conversationSetHours(ctx context.Context, chatID int64, user UserRecord, text string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	value := strings.TrimSpace(text)
	if value == "" {
		return b.sendText(ctx, chatID, tr(user.Language, "hours_ask_text"))
	}
	if err := b.store.SetWorkHoursText(ctx, user.TelegramID, value); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "hours_failed"))
	}
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "hours_ok"), keyboardForUser(user))
}

func (b *Bot) askWeeklyHoursDay(ctx context.Context, chatID int64, user UserRecord, state ConversationState) error {
	if state.WeekdayIndex < 0 {
		state.WeekdayIndex = 0
	}
	if state.WeekdayIndex >= len(weeklyWizardDays) {
		if err := b.store.SetWeeklyHours(ctx, user.TelegramID, state.WeeklyHours); err != nil {
			b.logger.Printf("weekly hours save failed admin=%d: %v", user.TelegramID, err)
			return b.sendText(ctx, chatID, tr(user.Language, "hours_failed"))
		}
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "hours_ok"), keyboardForUser(user))
	}
	state.Step = conversationStepSetHoursDay
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "hours_ask_day", weekdayLabel(user.Language, weeklyWizardDays[state.WeekdayIndex])), weeklyHoursKeyboard(user.Language, state.WeekdayIndex > 0))
}

func (b *Bot) conversationSetHoursDay(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	choice := normalizeChoice(text)
	if choice == "back" || strings.EqualFold(strings.TrimSpace(text), tr(user.Language, "button_back")) {
		if state.WeekdayIndex <= 0 {
			_ = b.store.ClearConversationState(ctx, user.TelegramID)
			return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "menu_main_text"), keyboardForUser(user))
		}
		state.WeekdayIndex--
		if len(state.WeeklyHours) > state.WeekdayIndex {
			state.WeeklyHours = state.WeeklyHours[:state.WeekdayIndex]
		}
		return b.askWeeklyHoursDay(ctx, chatID, user, state)
	}
	if state.WeekdayIndex < 0 || state.WeekdayIndex >= len(weeklyWizardDays) {
		state.WeekdayIndex = 0
	}
	day := weeklyWizardDays[state.WeekdayIndex]
	entry := WeekdayHours{Weekday: day, Working: false}
	if choice != "off" && choice != "not_working" && !strings.EqualFold(strings.TrimSpace(text), tr(user.Language, "button_not_working")) {
		start, end, err := parseDayRange(strings.TrimSpace(text))
		if err != nil || end <= start {
			return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "hours_day_bad"), weeklyHoursKeyboard(user.Language, state.WeekdayIndex > 0))
		}
		entry.Working = true
		entry.Start = formatClockDuration(start)
		entry.End = formatClockDuration(end)
	}
	if len(state.WeeklyHours) > state.WeekdayIndex {
		state.WeeklyHours[state.WeekdayIndex] = entry
	} else {
		state.WeeklyHours = append(state.WeeklyHours, entry)
	}
	state.WeekdayIndex++
	return b.askWeeklyHoursDay(ctx, chatID, user, state)
}

func (b *Bot) conversationSetDuration(ctx context.Context, chatID int64, user UserRecord, text string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	duration, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || duration <= 0 {
		return b.sendText(ctx, chatID, tr(user.Language, "duration_bad"))
	}
	if err := b.store.SetSessionDuration(ctx, user.TelegramID, duration); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "duration_failed"))
	}
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "duration_ok"), keyboardForUser(user))
}

func (b *Bot) conversationGenerateMode(ctx context.Context, chatID int64, user UserRecord, text string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	mode, ok := parseGenerateMode(user.Language, text)
	if !ok {
		value := strings.TrimSpace(text)
		if _, err := time.Parse("2006-01-02", value); err == nil {
			return b.conversationGenerateMonth(ctx, chatID, user, ConversationState{Step: conversationStepGenMonth, GenerateMode: "day"}, value)
		}
		if _, err := time.Parse(monthLayout, value); err == nil {
			return b.conversationGenerateMonth(ctx, chatID, user, ConversationState{Step: conversationStepGenMonth, GenerateMode: "month"}, value)
		}
		return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "generate_ask_mode"), scheduleChangeKeyboard(user.Language))
	}
	if mode == "delete_month" {
		return b.beginDeleteMonthConversation(ctx, chatID, user)
	}
	return b.beginGeneratedMonthConversation(ctx, chatID, user, mode)
}

func parseGenerateMode(lang, text string) (string, bool) {
	value := normalizeMenuButton(text)
	switch value {
	case "1":
		return "month", true
	case "2":
		return "months", true
	case "3":
		return "day", true
	case "4":
		return "weekday", true
	case "5":
		return "delete_month", true
	}
	switch menuButtonAction(lang, text) {
	case "action_generate_month":
		return "month", true
	case "action_generate_months":
		return "months", true
	case "action_generate_day":
		return "day", true
	case "action_generate_weekday":
		return "weekday", true
	case "action_delete_month":
		return "delete_month", true
	default:
		return "", false
	}
}

func (b *Bot) conversationGenerateMonth(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	value := strings.TrimSpace(text)
	switch state.GenerateMode {
	case "day":
		if day, err := time.Parse("2006-01-02", value); err == nil {
			state.ServiceName = day.Format("2006-01-02")
			state.Step = conversationStepGenDayRange
			if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
				return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
			}
			return b.sendText(ctx, chatID, tr(user.Language, "generate_ask_day_range"))
		}
		monthStart, err := time.Parse(monthLayout, value)
		if err != nil {
			return b.sendText(ctx, chatID, tr(user.Language, "generate_ask_month_pick"))
		}
		days, err := b.store.ListScheduleDays(ctx, user.TelegramID, monthStart)
		if err != nil {
			b.logger.Printf("list schedule days failed admin=%d month=%s: %v", user.TelegramID, monthStart.Format(monthLayout), err)
			return b.sendText(ctx, chatID, tr(user.Language, "generate_failed"))
		}
		state.ServiceName = monthStart.Format(monthLayout)
		state.Step = conversationStepGenDay
		if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
			return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
		}
		messageKey := "generate_ask_day_pick"
		if len(days) == 0 {
			messageKey = "generate_ask_day_empty"
		}
		return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, messageKey), scheduleDaysKeyboard(user.Language, days))
	case "month", "months", "weekday":
		monthStart, err := time.Parse(monthLayout, value)
		if err != nil {
			return b.sendText(ctx, chatID, tr(user.Language, "generate_ask_month_pick"))
		}
		state.ServiceName = monthStart.Format(monthLayout)
		switch state.GenerateMode {
		case "month":
			result, err := b.store.GenerateSchedule(ctx, user.TelegramID, GenerateScheduleRequest{Month: monthStart, Months: 1})
			if err != nil {
				return b.sendText(ctx, chatID, tr(user.Language, "generate_failed"))
			}
			_ = b.store.ClearConversationState(ctx, user.TelegramID)
			return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "generate_ok", result.Created, result.Skipped), keyboardForUser(user))
		case "months":
			state.Step = conversationStepGenMonths
			if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
				return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
			}
			return b.sendText(ctx, chatID, tr(user.Language, "generate_ask_month_count"))
		case "weekday":
			weekdays, err := b.store.ListScheduleWeekdays(ctx, user.TelegramID, monthStart)
			if err != nil {
				b.logger.Printf("list schedule weekdays failed admin=%d month=%s: %v", user.TelegramID, monthStart.Format(monthLayout), err)
				return b.sendText(ctx, chatID, tr(user.Language, "generate_failed"))
			}
			state.Step = conversationStepGenWeekdays
			if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
				return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
			}
			messageKey := "generate_ask_weekdays"
			if len(weekdays) == 0 {
				messageKey = "generate_ask_weekdays_empty"
			}
			return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, messageKey), scheduleWeekdaysKeyboard(user.Language, weekdays))
		}
	}
	if day, err := time.Parse("2006-01-02", value); err == nil {
		state.ServiceName = day.Format("2006-01-02")
		state.Step = conversationStepGenDayRange
		if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
			return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
		}
		return b.sendText(ctx, chatID, tr(user.Language, "generate_ask_day_range"))
	}
	monthStart, err := time.Parse(monthLayout, value)
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "generate_ask_month"))
	}
	state.ServiceName = monthStart.Format(monthLayout)
	state.Step = conversationStepGenMonths
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	return b.sendText(ctx, chatID, tr(user.Language, "generate_ask_months"))
}

func (b *Bot) conversationGenerateMonths(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	months := 1
	value := strings.TrimSpace(text)
	if value != "" && value != "-" {
		parsed, err := strconv.Atoi(value)
		if err == nil {
			if parsed <= 0 {
				return b.sendText(ctx, chatID, tr(user.Language, "generate_ask_months"))
			}
			months = parsed
		} else {
			if state.GenerateMode == "months" {
				return b.sendText(ctx, chatID, tr(user.Language, "generate_ask_month_count"))
			}
			days, err := parseWeekdays(value)
			if err != nil {
				return b.sendText(ctx, chatID, tr(user.Language, "generate_bad_days"))
			}
			state.GenerateWeekdays = days
			state.Step = conversationStepGenWeekdaysRange
			if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
				return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
			}
			return b.sendText(ctx, chatID, tr(user.Language, "generate_ask_weekdays_range"))
		}
	}
	monthStart, err := time.Parse(monthLayout, state.ServiceName)
	if err != nil {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "generate_ask_month"))
	}
	result, err := b.store.GenerateSchedule(ctx, user.TelegramID, GenerateScheduleRequest{Month: monthStart, Months: months})
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "generate_failed"))
	}
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "generate_ok", result.Created, result.Skipped), keyboardForUser(user))
}

func (b *Bot) conversationGenerateDay(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	day, err := time.Parse("2006-01-02", strings.TrimSpace(text))
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "generate_ask_day_pick"))
	}
	state.ServiceName = day.Format("2006-01-02")
	state.Step = conversationStepGenDayRange
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	return b.sendText(ctx, chatID, tr(user.Language, "generate_ask_day_range"))
}

func (b *Bot) conversationGenerateWeekdays(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	days, err := parseWeekdays(strings.TrimSpace(text))
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "generate_bad_days"))
	}
	state.GenerateWeekdays = days
	state.Step = conversationStepGenWeekdaysRange
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	return b.sendText(ctx, chatID, tr(user.Language, "generate_ask_weekdays_range"))
}

func (b *Bot) conversationGenerateWeekdaysRange(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	start, end, err := parseDayRange(strings.TrimSpace(text))
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "generate_bad_time"))
	}
	if end <= start {
		return b.sendText(ctx, chatID, tr(user.Language, "generate_bad_range"))
	}
	state.FromDateTime = strings.TrimSpace(text)
	state.Step = conversationStepGenWeekdaysStep
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	return b.sendText(ctx, chatID, tr(user.Language, "generate_ask_day_step"))
}

func (b *Bot) conversationGenerateWeekdaysStep(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	monthStart, err := time.Parse(monthLayout, state.ServiceName)
	if err != nil {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "generate_ask_month"))
	}
	if len(state.GenerateWeekdays) == 0 {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "generate_bad_days"))
	}
	start, end, err := parseDayRange(state.FromDateTime)
	if err != nil || end <= start {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "generate_ask_weekdays_range"))
	}
	duration := 0
	value := strings.TrimSpace(text)
	if value != "" && value != "-" {
		duration, err = strconv.Atoi(value)
		if err != nil || duration <= 0 {
			return b.sendText(ctx, chatID, tr(user.Language, "duration_bad"))
		}
	}
	result, err := b.store.GenerateSchedule(ctx, user.TelegramID, GenerateScheduleRequest{
		Month:       monthStart,
		Months:      1,
		Weekdays:    state.GenerateWeekdays,
		DayStart:    start,
		DayEnd:      end,
		DurationMin: duration,
	})
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "generate_failed"))
	}
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "generate_ok", result.Created, result.Skipped), keyboardForUser(user))
}

func (b *Bot) conversationGenerateDayRange(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	start, end, err := parseDayRange(strings.TrimSpace(text))
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "generate_bad_time"))
	}
	if end <= start {
		return b.sendText(ctx, chatID, tr(user.Language, "generate_bad_range"))
	}
	state.FromDateTime = strings.TrimSpace(text)
	state.Step = conversationStepGenDayStep
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	return b.sendText(ctx, chatID, tr(user.Language, "generate_ask_day_step"))
}

func (b *Bot) conversationGenerateDayStep(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	day, err := time.Parse("2006-01-02", state.ServiceName)
	if err != nil {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "generate_ask_month"))
	}
	start, end, err := parseDayRange(state.FromDateTime)
	if err != nil || end <= start {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "generate_ask_day_range"))
	}
	duration := 0
	value := strings.TrimSpace(text)
	if value != "" && value != "-" {
		duration, err = strconv.Atoi(value)
		if err != nil || duration <= 0 {
			return b.sendText(ctx, chatID, tr(user.Language, "duration_bad"))
		}
	}
	result, err := b.store.GenerateSchedule(ctx, user.TelegramID, GenerateScheduleRequest{
		Date:        day,
		DayStart:    start,
		DayEnd:      end,
		DurationMin: duration,
	})
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "generate_failed"))
	}
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "generate_ok", result.Created, result.Skipped), keyboardForUser(user))
}

func (b *Bot) conversationDeleteScheduleMonth(ctx context.Context, chatID int64, user UserRecord, text string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	monthStart, err := time.Parse(monthLayout, strings.TrimSpace(text))
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "schedule_delete_usage"))
	}
	result, err := b.store.DeleteScheduleMonth(ctx, user.TelegramID, monthStart)
	if err != nil {
		b.logger.Printf("interactive schedule delete failed admin=%d month=%s: %v", user.TelegramID, monthStart.Format(monthLayout), err)
		return b.sendText(ctx, chatID, tr(user.Language, "schedule_delete_failed"))
	}
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "schedule_delete_ok", monthStart.Format(monthLayout), result.Deleted), keyboardForUser(user))
}

func (b *Bot) conversationAdminAdd(ctx context.Context, chatID int64, user UserRecord, text string) error {
	if user.ActualRole != RoleSuperAdmin {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "super_only_add"))
	}
	username := normalizeUsername(text)
	if username == "" {
		return b.sendText(ctx, chatID, tr(user.Language, "bad_username"))
	}
	if err := b.store.SetUserRole(ctx, username, RoleAdmin); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "admin_add_failed"))
	}
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "admin_added", username), keyboardForUser(user))
}

func (b *Bot) conversationAdminRemove(ctx context.Context, chatID int64, user UserRecord, text string) error {
	if user.ActualRole != RoleSuperAdmin {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "super_only_remove"))
	}
	username := normalizeUsername(text)
	if username == "" {
		return b.sendText(ctx, chatID, tr(user.Language, "bad_username"))
	}
	if err := b.store.SetUserRole(ctx, username, RoleUser); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "admin_remove_failed"))
	}
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "admin_removed", username), keyboardForUser(user))
}

func (b *Bot) conversationViewAdmin(ctx context.Context, chatID int64, user UserRecord, text string) error {
	if user.ActualRole != RoleSuperAdmin {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "super_only_role"))
	}
	username := normalizeUsername(text)
	if username == "" {
		return b.sendText(ctx, chatID, tr(user.Language, "bad_username"))
	}
	if err := b.store.SetSuperAdminView(ctx, user.TelegramID, SuperAdminView{Role: RoleAdmin, AdminUsername: username}); err != nil {
		b.logger.Printf("interactive set view admin failed user=%d target=%q: %v", user.TelegramID, username, err)
		return b.sendText(ctx, chatID, tr(user.Language, "view_admin_failed"))
	}
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	user.Role = RoleAdmin
	user.ViewRole = RoleAdmin
	user.ViewAdminName = username
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "view_admin_ok", username), keyboardForUser(user))
}

func (b *Bot) conversationRoleUser(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	if user.ActualRole != RoleSuperAdmin {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "super_only_role"))
	}
	username := normalizeUsername(text)
	if username == "" {
		return b.sendText(ctx, chatID, tr(user.Language, "bad_username"))
	}
	state.Username = username
	state.Step = conversationStepRoleValue
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "role_ask_value"), roleChoiceKeyboard())
}

func (b *Bot) conversationRoleValue(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	if user.ActualRole != RoleSuperAdmin {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "super_only_role"))
	}
	value := strings.ToLower(strings.TrimSpace(text))
	if value == "show" || value == "показать" {
		target, err := b.store.GetUserByUsername(ctx, state.Username)
		if err != nil {
			return b.sendText(ctx, chatID, tr(user.Language, "role_show_failed"))
		}
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "role_current", state.Username, target.Role), keyboardForUser(user))
	}
	role := Role(value)
	if role != RoleUser && role != RoleAdmin && role != RoleSuperAdmin {
		return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "role_bad"), roleChoiceKeyboard())
	}
	if err := b.store.SetUserRole(ctx, state.Username, role); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "role_set_failed"))
	}
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "role_set", state.Username, role), keyboardForUser(user))
}

func (b *Bot) conversationAppointKind(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	switch normalizeChoice(text) {
	case "telegram":
		state.ContactType = "telegram"
		state.Step = conversationStepAppointUser
		if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
			return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
		}
		return b.sendText(ctx, chatID, tr(user.Language, "appoint_ask_username"))
	case "phone":
		state.ContactType = "phone"
		state.Step = conversationStepAppointUser
		if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
			return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
		}
		return b.sendText(ctx, chatID, tr(user.Language, "appoint_ask_phone"))
	default:
		return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "appoint_ask_contact_type"), contactTypeKeyboard(user.Language))
	}
}

func (b *Bot) conversationAppointUser(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	if state.ContactType == "phone" {
		phone := normalizePhone(text)
		if phone == "" {
			return b.sendText(ctx, chatID, tr(user.Language, "appoint_ask_phone"))
		}
		state.Username = phone
		state.Step = conversationStepAppointTime
		if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
			return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
		}
		return b.askAdminAppointmentServices(ctx, chatID, user, state)
	}
	state.ContactType = "telegram"
	if err := b.setAdminTargetUsername(ctx, user, &state, text); err != nil {
		if errors.Is(err, store.ErrInvalidArgument) {
			return b.sendText(ctx, chatID, tr(user.Language, "bad_username"))
		}
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	return b.askAdminAppointmentServices(ctx, chatID, user, state)
}

func (b *Bot) conversationAppointTime(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	start, err := parseDateTimeInput(text)
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "datetime_bad_example"))
	}
	if state.ContactType == "phone" {
		result, err := b.store.AddBookingByPhone(ctx, user.TelegramID, state.Username, start)
		if err != nil {
			return b.sendText(ctx, chatID, tr(user.Language, "appoint_failed"))
		}
		b.notifyBookingChange(ctx, "created", result, chatID)
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "appoint_ok_contact", state.Username), keyboardForUser(user))
	}
	result, err := b.store.AddBookingByUsername(ctx, user.TelegramID, state.Username, start)
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "appoint_failed"))
	}
	b.notifyBookingChange(ctx, "created", result, chatID)
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "appoint_ok", state.Username), keyboardForUser(user))
}

func (b *Bot) conversationCancelUser(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	return b.askAdminTargetUsername(ctx, chatID, user, state, text, conversationStepCancelTime, "cancel_ask_datetime")
}

func (b *Bot) conversationCancelTime(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	start, err := parseDateTimeInput(text)
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "datetime_bad"))
	}
	result, err := b.store.DeleteBookingByUsername(ctx, user.TelegramID, state.Username, start)
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "cancel_failed"))
	}
	b.notifyBookingChange(ctx, "cancelled", result, chatID)
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "cancel_ok", state.Username), keyboardForUser(user))
}

func (b *Bot) conversationRescheduleUser(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	return b.askAdminTargetUsername(ctx, chatID, user, state, text, conversationStepReschFrom, "reschedule_ask_from")
}

func (b *Bot) conversationRescheduleFrom(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	fromStart, err := parseDateTimeInput(text)
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "from_datetime_bad"))
	}
	state.FromDateTime = fromStart.Format(dateTimeLayout)
	state.Step = conversationStepReschTo
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	return b.sendText(ctx, chatID, tr(user.Language, "reschedule_ask_to"))
}

func (b *Bot) conversationRescheduleTo(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	fromStart, err := parseDateTimeInput(state.FromDateTime)
	if err != nil {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "from_datetime_bad"))
	}
	toStart, err := parseDateTimeInput(text)
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "to_datetime_bad"))
	}
	result, err := b.store.RescheduleBookingByUsername(ctx, user.TelegramID, state.Username, fromStart, toStart)
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "reschedule_failed"))
	}
	b.notifyBookingChange(ctx, "rescheduled", result, chatID)
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "reschedule_ok", state.Username), keyboardForUser(user))
}

func (b *Bot) conversationBlock(ctx context.Context, chatID int64, user UserRecord, text string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	start, err := parseDateTimeInput(text)
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "datetime_bad"))
	}
	if err := b.store.BlockSlot(ctx, user.TelegramID, start); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "block_failed"))
	}
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "block_ok"), keyboardForUser(user))
}

func (b *Bot) conversationBlockDate(ctx context.Context, chatID int64, user UserRecord, text string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	date, err := parseSingleDate(text)
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "block_date_bad"))
	}
	result, err := b.store.BlockScheduleDate(ctx, user.TelegramID, date)
	if err != nil {
		b.logger.Printf("interactive block date failed admin=%d date=%s: %v", user.TelegramID, date.Format("2006-01-02"), err)
		return b.sendText(ctx, chatID, tr(user.Language, "block_date_failed"))
	}
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "block_date_ok", result.Date.Format("2006-01-02"), result.ClosedSlots), keyboardForUser(user))
}

func (b *Bot) askAdminTargetUsername(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text, nextStep, promptKey string) error {
	if !isAdmin(user.Role) {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	if err := b.setAdminTargetUsername(ctx, user, &state, text); err != nil {
		if errors.Is(err, store.ErrInvalidArgument) {
			return b.sendText(ctx, chatID, tr(user.Language, "bad_username"))
		}
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	state.Step = nextStep
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	return b.sendText(ctx, chatID, tr(user.Language, promptKey))
}

func (b *Bot) setAdminTargetUsername(ctx context.Context, user UserRecord, state *ConversationState, text string) error {
	username := normalizeUsername(text)
	if username == "" {
		return store.ErrInvalidArgument
	}
	state.Username = username
	return nil
}

func (b *Bot) askAdminAppointmentServices(ctx context.Context, chatID int64, user UserRecord, state ConversationState) error {
	state.Step = conversationStepCategory
	state.Category = ""
	state.Subcategory = ""
	state.ServiceIndexes = nil
	state.VisibleServiceIndexes = nil
	return b.askCategoryWithState(ctx, chatID, user, state)
}

func (b *Bot) askCategory(ctx context.Context, chatID int64, user UserRecord, selected []int) error {
	return b.askCategoryWithState(ctx, chatID, user, ConversationState{Step: conversationStepCategory, ServiceIndexes: selected})
}

func (b *Bot) askCategoryWithState(ctx context.Context, chatID int64, user UserRecord, state ConversationState) error {
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
	state.Step = conversationStepCategory
	state.Category = ""
	state.Subcategory = ""
	state.VisibleServiceIndexes = nil
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		b.logger.Printf("ask category: save state failed user=%d: %v", user.TelegramID, err)
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	var sb strings.Builder
	if strings.TrimSpace(intro) != "" {
		sb.WriteString(intro)
		sb.WriteString("\n\n")
	}
	if len(state.ServiceIndexes) > 0 {
		sb.WriteString(tr(user.Language, "selected_services", formatIndexes(state.ServiceIndexes)))
		sb.WriteString("\n\n")
	}
	sb.WriteString(formatCategories(user.Language, categories))
	sb.WriteString("\n")
	sb.WriteString(tr(user.Language, "ask_category"))
	return b.sendTextWithKeyboard(ctx, chatID, sb.String(), numberKeyboardWithPrefixLang(len(categories), "cat", user.Language))
}

func isAdminAppointmentState(state ConversationState) bool {
	return (state.ContactType == "phone" || state.ContactType == "telegram") && strings.TrimSpace(state.Username) != ""
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
	return b.sendTextWithKeyboard(ctx, chatID, sb.String(), numberKeyboardWithPrefixLang(len(subcategories), "sub", user.Language))
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
	return b.sendTextWithKeyboard(ctx, chatID, sb.String(), numberKeyboardWithPrefixLang(len(visibleServices), "svc", user.Language))
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
		if service.Description != "" {
			sb.WriteString("   ")
			sb.WriteString(service.Description)
			sb.WriteString("\n")
		}
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

func splitServiceNameDescription(text string) (string, string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ""
	}
	if before, after, ok := strings.Cut(text, "|"); ok {
		return strings.TrimSpace(before), normalizeOptionalText(after)
	}
	lines := strings.SplitN(text, "\n", 2)
	if len(lines) == 2 {
		return strings.TrimSpace(lines[0]), normalizeOptionalText(lines[1])
	}
	return text, ""
}

func parseServiceEditData(text string) (int, string, string, bool) {
	parts := strings.Fields(strings.TrimSpace(text))
	if len(parts) < 2 {
		return 0, "", "", false
	}
	duration, err := strconv.Atoi(parts[0])
	if err != nil || duration <= 0 {
		return 0, "", "", false
	}
	name, description := splitServiceNameDescription(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), parts[0])))
	if name == "" {
		return 0, "", "", false
	}
	return duration, name, description, true
}

func (b *Bot) askServiceAddCategory(ctx context.Context, chatID int64, user UserRecord, state ConversationState) error {
	state.Step = conversationStepAddSvcCat
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	services, err := b.store.ListServices(ctx, user.TelegramID)
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "services_list_failed"))
	}
	categories := serviceCategories(services)
	var sb strings.Builder
	sb.WriteString(tr(user.Language, "service_add_ask_category"))
	if len(categories) > 0 {
		sb.WriteString("\n")
		sb.WriteString(formatCategories(user.Language, categories))
	}
	return b.sendText(ctx, chatID, sb.String())
}

func (b *Bot) askServiceAddSubcategory(ctx context.Context, chatID int64, user UserRecord, state ConversationState) error {
	state.Step = conversationStepAddSvcSub
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	services, err := b.store.ListServices(ctx, user.TelegramID)
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "services_list_failed"))
	}
	subcategories := serviceSubcategories(services, state.Category)
	var sb strings.Builder
	sb.WriteString(tr(user.Language, "service_add_ask_subcategory"))
	if len(subcategories) > 0 {
		sb.WriteString("\n")
		sb.WriteString(formatSubcategories(user.Language, subcategories))
	}
	return b.sendText(ctx, chatID, sb.String())
}

func (b *Bot) resolveAdminServiceCategory(ctx context.Context, user UserRecord, text string) (string, bool, error) {
	value := strings.TrimSpace(text)
	if value == "" {
		return "", false, nil
	}
	if index, err := strconv.Atoi(value); err == nil {
		services, err := b.store.ListServices(ctx, user.TelegramID)
		if err != nil {
			return "", false, err
		}
		categories := serviceCategories(services)
		if index <= 0 || index > len(categories) {
			return "", false, nil
		}
		return categories[index-1], true, nil
	}
	return normalizeOptionalText(value), true, nil
}

func (b *Bot) resolveAdminServiceSubcategory(ctx context.Context, user UserRecord, category, text string) (string, bool, error) {
	value := strings.TrimSpace(text)
	if value == "" {
		return "", false, nil
	}
	if index, err := strconv.Atoi(value); err == nil {
		services, err := b.store.ListServices(ctx, user.TelegramID)
		if err != nil {
			return "", false, err
		}
		subcategories := serviceSubcategories(services, category)
		if index <= 0 || index > len(subcategories) {
			return "", false, nil
		}
		return subcategories[index-1], true, nil
	}
	return normalizeOptionalText(value), true, nil
}

func parseCategoryOrder(raw string, categories []string) ([]string, bool) {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
	})
	if len(parts) != len(categories) {
		return nil, false
	}
	seen := make(map[int]bool, len(categories))
	ordered := make([]string, 0, len(categories))
	for _, part := range parts {
		index, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || index <= 0 || index > len(categories) || seen[index] {
			return nil, false
		}
		seen[index] = true
		ordered = append(ordered, categories[index-1])
	}
	return ordered, true
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
	state.SlotDay = clampSlotDay(slots, state.SlotDay)
	visible := visibleSlotsForDayPeriod(slots, state.SlotDay, state.SlotPeriod)
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

func moveSlotDay(slots []AvailabilitySlot, currentDay, _ string, direction string) string {
	minDay, ok := firstSlotDay(slots)
	if !ok {
		return ""
	}
	day, err := time.Parse("2006-01-02", currentDay)
	if err != nil {
		day, err = time.Parse("2006-01-02", minDay)
		if err != nil {
			return ""
		}
	}
	switch direction {
	case "prev":
		day = day.AddDate(0, 0, -1)
	case "next":
		day = day.AddDate(0, 0, 1)
	}
	return clampMinDayString(day.Format("2006-01-02"), minDay)
}

func clampSlotDay(slots []AvailabilitySlot, day string) string {
	minDay, ok := firstSlotDay(slots)
	if !ok {
		return ""
	}
	return clampMinDayString(day, minDay)
}

func clampMinDayString(day, minDay string) string {
	if day == "" || day < minDay {
		return minDay
	}
	return day
}

func firstSlotDay(slots []AvailabilitySlot) (string, bool) {
	if len(slots) == 0 {
		return "", false
	}
	minDay := slots[0].StartAt.Format("2006-01-02")
	for _, slot := range slots[1:] {
		day := slot.StartAt.Format("2006-01-02")
		if day < minDay {
			minDay = day
		}
	}
	return minDay, true
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
	rows = append(rows, []telegram.InlineKeyboardButton{
		{Text: tr(lang, "button_back"), CallbackData: "back:slot"},
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

func intInSlice(values []int, needle int) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
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
	if hasAdminCalendarNames(items) {
		return formatCalendarByAdmin(lang, monthStart, items)
	}
	return formatCalendarSingle(lang, monthStart, items)
}

func formatCalendarByAdmin(lang string, monthStart time.Time, items []CalendarDay) string {
	grouped := make(map[string][]CalendarDay)
	var names []string
	for _, item := range items {
		name := strings.TrimSpace(item.AdminName)
		if name == "" {
			name = "admin"
		}
		if _, ok := grouped[name]; !ok {
			names = append(names, name)
		}
		item.AdminName = ""
		grouped[name] = append(grouped[name], item)
	}
	sort.Strings(names)

	var sb strings.Builder
	for i, name := range names {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString("@")
		sb.WriteString(name)
		sb.WriteString("\n")
		sb.WriteString(formatCalendarSingle(lang, monthStart, grouped[name]))
	}
	return sb.String()
}

func hasAdminCalendarNames(items []CalendarDay) bool {
	for _, item := range items {
		if strings.TrimSpace(item.AdminName) != "" {
			return true
		}
	}
	return false
}

func formatCalendarSingle(lang string, monthStart time.Time, items []CalendarDay) string {
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
	case "не работаю", "выходной", "off", "not working", "not_working":
		return "off"
	case "назад", "back":
		return "back"
	case "telegram", "tg", "тг", "телеграм":
		return "telegram"
	case "phone", "телефон", "номер", "номер телефона":
		return "phone"
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

func parseDateTimeInput(text string) (time.Time, error) {
	parts := strings.Fields(text)
	if len(parts) < 2 {
		return time.Time{}, fmt.Errorf("datetime must contain date and time")
	}
	return parseDateTime(parts[0], parts[1])
}

func parseSingleDate(text string) (time.Time, error) {
	parts := strings.Fields(strings.TrimSpace(text))
	if len(parts) == 0 {
		return time.Time{}, fmt.Errorf("date is required")
	}
	return parseUserDate(parts[0], time.Now())
}

func normalizePhone(text string) string {
	value := strings.TrimSpace(text)
	if value == "" {
		return ""
	}
	digits := strings.Builder{}
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
	rawDigits := digits.String()
	if len(rawDigits) < 5 {
		return ""
	}
	if strings.HasPrefix(value, "+") {
		return "+" + rawDigits
	}
	return rawDigits
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

func formatClockDuration(value time.Duration) string {
	totalMinutes := int(value / time.Minute)
	return fmt.Sprintf("%02d:%02d", totalMinutes/60, totalMinutes%60)
}

var weeklyWizardDays = []time.Weekday{
	time.Monday,
	time.Tuesday,
	time.Wednesday,
	time.Thursday,
	time.Friday,
	time.Saturday,
	time.Sunday,
}

func weekdayLabel(lang string, day time.Weekday) string {
	if lang == LangEN {
		switch day {
		case time.Monday:
			return "Monday"
		case time.Tuesday:
			return "Tuesday"
		case time.Wednesday:
			return "Wednesday"
		case time.Thursday:
			return "Thursday"
		case time.Friday:
			return "Friday"
		case time.Saturday:
			return "Saturday"
		case time.Sunday:
			return "Sunday"
		}
	}
	switch day {
	case time.Monday:
		return "понедельник"
	case time.Tuesday:
		return "вторник"
	case time.Wednesday:
		return "среда"
	case time.Thursday:
		return "четверг"
	case time.Friday:
		return "пятница"
	case time.Saturday:
		return "суббота"
	case time.Sunday:
		return "воскресенье"
	default:
		return ""
	}
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
			{
				{Text: tr(lang, "button_back"), CallbackData: "back:more"},
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
			{
				{Text: tr(lang, "button_back"), CallbackData: "back:time"},
			},
		},
	}
}

func weeklyHoursKeyboard(lang string, canGoBack bool) *telegram.ReplyMarkup {
	rows := [][]telegram.InlineKeyboardButton{
		{{Text: tr(lang, "button_not_working"), CallbackData: "hours:off"}},
	}
	if canGoBack {
		rows = append(rows, []telegram.InlineKeyboardButton{{Text: tr(lang, "button_back"), CallbackData: "back:hours"}})
	}
	return &telegram.ReplyMarkup{InlineKeyboard: rows}
}

func contactTypeKeyboard(lang string) *telegram.ReplyMarkup {
	return &telegram.ReplyMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{
				{Text: tr(lang, "button_contact_telegram"), CallbackData: "contact:telegram"},
				{Text: tr(lang, "button_contact_phone"), CallbackData: "contact:phone"},
			},
			{
				{Text: tr(lang, "button_back"), CallbackData: "back:appoint"},
			},
		},
	}
}

func numberKeyboard(count int) *telegram.ReplyMarkup {
	return numberKeyboardWithPrefix(count, "slot")
}

func numberKeyboardWithPrefix(count int, prefix string) *telegram.ReplyMarkup {
	return numberKeyboardWithPrefixLang(count, prefix, LangRU)
}

func numberKeyboardWithPrefixLang(count int, prefix, lang string) *telegram.ReplyMarkup {
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
	rows = append(rows, []telegram.InlineKeyboardButton{{Text: tr(lang, "button_back"), CallbackData: "back:" + prefix}})
	return &telegram.ReplyMarkup{InlineKeyboard: rows}
}
