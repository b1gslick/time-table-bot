package bot

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"

	"time-table-bot/internal/store"
	"time-table-bot/internal/telegram"
)

const (
	dateTimeLayout = "2006-01-02 15:04"
	monthLayout    = "2006-01"
)

const (
	conversationStepLanguage         = "language"
	conversationStepCategory         = "category"
	conversationStepSubcategory      = "subcategory"
	conversationStepService          = "service"
	conversationStepMore             = "more"
	conversationStepTimeChoice       = "time_choice"
	conversationStepDates            = "dates"
	conversationStepSlot             = "slot"
	conversationStepBookingConfirm   = "booking_confirm"
	conversationStepBookingEdit      = "booking_edit"
	conversationStepAdminBookingTime = "admin_booking_time"
	conversationStepAddSvcCat        = "admin_service_category"
	conversationStepAddSvcSub        = "admin_service_subcategory"
	conversationStepAddSvcName       = "admin_service_name"
	conversationStepAddSvcDur        = "admin_service_duration"
	conversationStepAddSvcDesc       = "admin_service_description"
	conversationStepEditSvc          = "admin_service_edit"
	conversationStepEditSvcData      = "admin_service_edit_data"
	conversationStepDeleteSvc        = "admin_service_delete"
	conversationStepSetProfile       = "admin_set_profile"
	conversationStepCategoryOrd      = "admin_category_order"
	conversationStepSetHours         = "admin_set_hours"
	conversationStepSetHoursDay      = "admin_set_hours_day"
	conversationStepSetDuration      = "admin_set_duration"
	conversationStepGenMode          = "admin_generate_mode"
	conversationStepGenMonth         = "admin_generate_month"
	conversationStepGenMonths        = "admin_generate_months"
	conversationStepGenDay           = "admin_generate_day"
	conversationStepGenWeekdays      = "admin_generate_weekdays"
	conversationStepGenDayRange      = "admin_generate_day_range"
	conversationStepGenDayStep       = "admin_generate_day_step"
	conversationStepGenWeekdaysRange = "admin_generate_weekdays_range"
	conversationStepGenWeekdaysStep  = "admin_generate_weekdays_step"
	conversationStepDeleteMonth      = "admin_delete_month"
	conversationStepAdminAdd         = "super_admin_add"
	conversationStepAdminRemove      = "super_admin_remove"
	conversationStepViewAdmin        = "super_admin_view_admin"
	conversationStepRoleUser         = "super_admin_role_user"
	conversationStepRoleValue        = "super_admin_role_value"
	conversationStepAppointKind      = "admin_appoint_kind"
	conversationStepAppointUser      = "admin_appoint_user"
	conversationStepAppointTime      = "admin_appoint_time"
	conversationStepCancelUser       = "admin_cancel_user"
	conversationStepCancelTime       = "admin_cancel_time"
	conversationStepReschUser        = "admin_reschedule_user"
	conversationStepReschFrom        = "admin_reschedule_from"
	conversationStepReschTo          = "admin_reschedule_to"
	conversationStepBlock            = "admin_block"
	conversationStepBlockDate        = "admin_block_date"
	conversationStepScheduleImport   = "admin_schedule_import"
	conversationStepScheduleEdit     = "admin_schedule_import_edit"
	conversationStepSchedulePlan     = "admin_schedule_plan_confirm"
	conversationStepSchedulePlanEdit = "admin_schedule_plan_edit"
	conversationStepServiceImport    = "admin_service_import"
	conversationStepServiceReplace   = "admin_service_replace"
	conversationStepFinanceInput     = "admin_finance_input"
	conversationStepFinanceConfirm   = "admin_finance_confirm"
	conversationStepFinanceResolve   = "admin_finance_resolve"
)

func (b *Bot) HandleMessage(ctx context.Context, msg *telegram.Message) error {
	user := UserRecord{
		TelegramID: msg.From.ID,
		Username:   normalizeUsername(msg.From.Username),
		FirstName:  msg.From.FirstName,
		LastName:   msg.From.LastName,
		Role:       RoleUser,
		Language:   LangRU,
	}
	if user.Username == b.superAdminUsername {
		user.Role = RoleSuperAdmin
	}

	current, err := b.store.RegisterOrUpdateUser(ctx, user)
	if err != nil {
		return b.sendText(ctx, msg.Chat.ID, tr(LangRU, "register_failed"))
	}
	current = b.applySuperAdminView(ctx, current)
	if msg.Voice != nil || msg.Audio != nil {
		return b.handleSpeechMessage(ctx, msg, current)
	}
	if len(msg.Photo) > 0 || isImageDocument(msg.Document) {
		return b.handleImageMessage(ctx, msg, current)
	}

	text := strings.TrimSpace(msg.Text)
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return nil
	}

	cmd := strings.ToLower(parts[0])
	b.logger.Printf("message received chat=%d user=%d username=%q role=%s cmd=%q", msg.Chat.ID, current.TelegramID, current.Username, current.Role, cmd)

	if !strings.HasPrefix(cmd, "/") {
		if handled, err := b.handleMenuButton(ctx, msg.Chat.ID, current, text); handled {
			return err
		}
		if handled, err := b.handleAdminContactAlias(ctx, msg.Chat.ID, current, text); handled {
			return err
		}
	}

	switch cmd {
	case "/start":
		return b.handleStart(ctx, msg.Chat.ID, current)
	case "/booking", "/start_booking", "/book_start":
		return b.handleBookingStart(ctx, msg.Chat.ID, current)
	case "/help":
		return b.sendHelp(ctx, msg.Chat.ID, current)
	case "/aliases":
		return b.sendContactAliases(ctx, msg.Chat.ID, current)
	case "/finance", "/report":
		period := "month"
		if len(parts) > 1 {
			period = strings.ToLower(parts[1])
		}
		return b.sendFinanceReport(ctx, msg.Chat.ID, current, period)
	case "/alias":
		if handled, err := b.handleAdminContactAlias(ctx, msg.Chat.ID, current, text); handled {
			return err
		}
		return b.sendText(ctx, msg.Chat.ID, tr(current.Language, "contact_alias_usage"))
	case "/alias_delete":
		alias := strings.TrimSpace(strings.TrimPrefix(text, parts[0]))
		if handled, err := b.handleAdminContactAlias(ctx, msg.Chat.ID, current, "удали алиас "+alias); handled {
			return err
		}
		return b.sendText(ctx, msg.Chat.ID, tr(current.Language, "contact_alias_delete_usage"))
	case "/lang", "/language":
		return b.handleLanguage(ctx, msg.Chat.ID, current, parts)
	case "/role":
		return b.handleRole(ctx, msg.Chat.ID, current, parts)
	case "/admin_add":
		return b.handleAdminAdd(ctx, msg.Chat.ID, current, parts)
	case "/admin_remove":
		return b.handleAdminRemove(ctx, msg.Chat.ID, current, parts)
	case "/admins", "/admin_list":
		return b.handleAdminList(ctx, msg.Chat.ID, current)
	case "/view_admin":
		return b.handleViewAdmin(ctx, msg.Chat.ID, current, parts)
	case "/view_user":
		return b.handleViewUser(ctx, msg.Chat.ID, current)
	case "/view_super", "/view_reset":
		return b.handleViewSuper(ctx, msg.Chat.ID, current)
	case "/setprofile":
		return b.handleSetProfile(ctx, msg.Chat.ID, current, text)
	case "/setservices":
		return b.handleServiceCatalogReplaceCommand(ctx, msg.Chat.ID, current, text)
	case "/category_order", "/categories_order":
		return b.handleCategoryOrder(ctx, msg.Chat.ID, current, parts)
	case "/service_add", "/addservice":
		return b.handleServiceAdd(ctx, msg.Chat.ID, current, parts)
	case "/service_edit", "/editservice":
		return b.handleServiceEdit(ctx, msg.Chat.ID, current, parts)
	case "/service_delete", "/service_remove", "/delservice":
		return b.handleServiceDelete(ctx, msg.Chat.ID, current, parts)
	case "/services":
		return b.handleServices(ctx, msg.Chat.ID, current)
	case "/sethours":
		return b.handleSetHours(ctx, msg.Chat.ID, current, text)
	case "/block_day", "/day_block", "/date_block":
		return b.handleBlockDate(ctx, msg.Chat.ID, current, parts)
	case "/setduration":
		return b.handleSetDuration(ctx, msg.Chat.ID, current, parts)
	case "/generate", "/gen":
		return b.handleGenerate(ctx, msg.Chat.ID, current, parts)
	case "/calendar_delete", "/schedule_delete", "/delete_calendar":
		return b.handleScheduleDelete(ctx, msg.Chat.ID, current, parts)
	case "/appoint":
		return b.handleAppoint(ctx, msg.Chat.ID, current, parts)
	case "/bookings", "/appointments":
		return b.handleAdminBookings(ctx, msg.Chat.ID, current, parts)
	case "/cancel":
		return b.handleCancel(ctx, msg.Chat.ID, current, parts)
	case "/reschedule":
		return b.handleReschedule(ctx, msg.Chat.ID, current, parts)
	case "/block":
		return b.handleBlock(ctx, msg.Chat.ID, current, parts)
	case "/free", "/schedule":
		return b.handleFree(ctx, msg.Chat.ID, current, parts)
	case "/week", "/week_schedule":
		return b.handleWeek(ctx, msg.Chat.ID, current, parts)
	case "/calendar", "/cal":
		return b.handleCalendar(ctx, msg.Chat.ID, current, parts)
	case "/month", "/request_month":
		return b.handleRequestMonth(ctx, msg.Chat.ID, current, parts)
	case "/my":
		return b.handleMy(ctx, msg.Chat.ID, current)
	case "/book":
		return b.handleBook(ctx, msg.Chat.ID, current, parts)
	case "/move":
		return b.handleMove(ctx, msg.Chat.ID, current, parts)
	default:
		if !strings.HasPrefix(cmd, "/") {
			if handled, err := b.handleConversation(ctx, msg.Chat.ID, current, text); handled {
				return err
			}
			if handled, err := b.handleAdminFinanceChartRequest(ctx, msg.Chat.ID, current, text); handled {
				return err
			}
			if handled, err := b.handleAdminFinanceReportRequest(ctx, msg.Chat.ID, current, text); handled {
				return err
			}
			if handled, err := b.handleAdminNaturalSchedule(ctx, msg.Chat.ID, current, text); handled {
				return err
			}
			if handled, err := b.handleAdminFinanceInput(ctx, msg.Chat.ID, current, text, "text", ""); handled {
				return err
			}
			if handled, err := b.handleAdminNaturalServiceImport(ctx, msg.Chat.ID, current, text); handled {
				return err
			}
			if handled, err := b.handleAdminNaturalBooking(ctx, msg.Chat.ID, current, text); handled {
				return err
			}
			if handled, err := b.handleNaturalBooking(ctx, msg.Chat.ID, current, text); handled {
				return err
			}
		}
		return b.sendText(ctx, msg.Chat.ID, tr(current.Language, "unknown_command"))
	}
}

func (b *Bot) HandleCallback(ctx context.Context, cb *telegram.CallbackQuery) error {
	if cb == nil || cb.Message == nil {
		return nil
	}
	if err := b.tg.AnswerCallbackQuery(ctx, telegram.AnswerCallbackQueryRequest{CallbackQueryID: cb.ID}); err != nil {
		b.logger.Printf("answer callback failed id=%q: %v", cb.ID, err)
	}
	if strings.HasPrefix(cb.Data, "slotdate:") || strings.HasPrefix(cb.Data, "slotday:") || strings.HasPrefix(cb.Data, "slotperiod:") {
		return b.handleSlotBrowseCallback(ctx, cb)
	}
	if strings.HasPrefix(cb.Data, "time:") {
		return b.handleTimeChoiceCallback(ctx, cb)
	}
	if strings.HasPrefix(cb.Data, "bookconfirm:") {
		return b.handleBookingConfirmCallback(ctx, cb)
	}
	if strings.HasPrefix(cb.Data, "bookedit:") {
		return b.handleBookingEditCallback(ctx, cb)
	}
	if strings.HasPrefix(cb.Data, "scheduleimport:") {
		return b.handleScheduleImportCallback(ctx, cb)
	}
	if strings.HasPrefix(cb.Data, "scheduleplan:") {
		return b.handleSchedulePlanCallback(ctx, cb)
	}
	if strings.HasPrefix(cb.Data, "serviceimport:") {
		return b.handleServiceImportCallback(ctx, cb)
	}
	if strings.HasPrefix(cb.Data, "financeentry:") {
		return b.handleFinanceEntryCallback(ctx, cb)
	}
	if strings.HasPrefix(cb.Data, "finance:") {
		return b.handleFinanceCallback(ctx, cb)
	}
	if cb.Data == "my:list" {
		return b.handleMyBookingsCallback(ctx, cb)
	}
	if cb.Data == "mycancel:list" {
		return b.handleMyCancelListCallback(ctx, cb)
	}
	if cb.Data == "mymove:list" {
		return b.handleMyMoveListCallback(ctx, cb)
	}
	if strings.HasPrefix(cb.Data, "mycancel:") {
		return b.handleMyCancelBookingCallback(ctx, cb)
	}
	if strings.HasPrefix(cb.Data, "mymove:") {
		return b.handleMyMoveBookingCallback(ctx, cb)
	}
	if strings.HasPrefix(cb.Data, "moveslot:") {
		return b.handleMyMoveSlotCallback(ctx, cb)
	}
	if strings.HasPrefix(cb.Data, "week:") {
		return b.handleWeekCallback(ctx, cb)
	}
	if strings.HasPrefix(cb.Data, "monthcal:") {
		return b.handleCalendarCallback(ctx, cb)
	}
	if cb.Data == "bookstart" {
		return b.handleBookingStartCallback(ctx, cb)
	}
	if strings.HasPrefix(cb.Data, "cancel:") {
		return b.handleCancelBookingCallback(ctx, cb)
	}
	if strings.HasPrefix(cb.Data, "back:") {
		return b.handleConversationBackCallback(ctx, cb)
	}
	text, ok := callbackText(cb.Data)
	if !ok {
		b.logger.Printf("unknown callback data user=%d data=%q", cb.From.ID, cb.Data)
		return nil
	}
	return b.HandleMessage(ctx, &telegram.Message{
		From: cb.From,
		Chat: cb.Message.Chat,
		Text: text,
	})
}

func (b *Bot) handleConversationBackCallback(ctx context.Context, cb *telegram.CallbackQuery) error {
	user := UserRecord{
		TelegramID: cb.From.ID,
		Username:   normalizeUsername(cb.From.Username),
		FirstName:  cb.From.FirstName,
		LastName:   cb.From.LastName,
		Role:       RoleUser,
		Language:   LangRU,
	}
	if user.Username == b.superAdminUsername {
		user.Role = RoleSuperAdmin
	}
	current, err := b.store.RegisterOrUpdateUser(ctx, user)
	if err != nil {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(LangRU, "register_failed"))
	}
	current = b.applySuperAdminView(ctx, current)
	state, err := b.store.GetConversationState(ctx, current.TelegramID)
	if err != nil {
		return b.handleStart(ctx, cb.Message.Chat.ID, current)
	}
	return b.conversationBack(ctx, cb.Message.Chat.ID, current, state)
}

func (b *Bot) handleSlotBrowseCallback(ctx context.Context, cb *telegram.CallbackQuery) error {
	user := UserRecord{
		TelegramID: cb.From.ID,
		Username:   normalizeUsername(cb.From.Username),
		FirstName:  cb.From.FirstName,
		LastName:   cb.From.LastName,
		Role:       RoleUser,
		Language:   LangRU,
	}
	if user.Username == b.superAdminUsername {
		user.Role = RoleSuperAdmin
	}
	current, err := b.store.RegisterOrUpdateUser(ctx, user)
	if err != nil {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(LangRU, "register_failed"))
	}
	current = b.applySuperAdminView(ctx, current)
	state, err := b.store.GetConversationState(ctx, current.TelegramID)
	if err != nil || state.Step != conversationStepSlot {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(current.Language, "book_need_schedule"))
	}
	slots, err := b.store.ListCachedAvailability(ctx, current.TelegramID)
	if err != nil {
		b.logger.Printf("slot browse: cached availability failed user=%d: %v", current.TelegramID, err)
		return b.sendText(ctx, cb.Message.Chat.ID, tr(current.Language, "book_need_schedule"))
	}
	if strings.HasPrefix(cb.Data, "slotdate:") {
		day := strings.TrimPrefix(cb.Data, "slotdate:")
		if !availabilityHasDay(slots, day) {
			return b.sendText(ctx, cb.Message.Chat.ID, tr(current.Language, "book_need_schedule"))
		}
		state.SlotDay = day
		state.SlotPeriod = "all"
	} else if strings.HasPrefix(cb.Data, "slotperiod:") {
		state.SlotPeriod = strings.TrimPrefix(cb.Data, "slotperiod:")
		state.SlotDay = chooseSlotDayForPeriod(slots, state.SlotDay, state.SlotPeriod)
	} else {
		state.SlotDay = moveSlotDay(slots, state.SlotDay, state.SlotPeriod, strings.TrimPrefix(cb.Data, "slotday:"))
	}
	text, kb, nextState := renderSlotBrowser(current.Language, state, slots)
	if err := b.store.SetConversationState(ctx, current.TelegramID, nextState); err != nil {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(current.Language, "conversation_failed"))
	}
	return b.sendTextWithKeyboard(ctx, cb.Message.Chat.ID, text, kb)
}

func (b *Bot) handleTimeChoiceCallback(ctx context.Context, cb *telegram.CallbackQuery) error {
	current, err := b.userFromCallback(ctx, cb)
	if err != nil {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(LangRU, "register_failed"))
	}
	state, err := b.store.GetConversationState(ctx, current.TelegramID)
	if errors.Is(err, store.ErrNotFound) {
		return b.handleBookingStart(ctx, cb.Message.Chat.ID, current)
	}
	if err != nil {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(current.Language, "conversation_failed"))
	}
	text, ok := callbackText(cb.Data)
	if !ok {
		b.logger.Printf("unknown time callback data user=%d data=%q", cb.From.ID, cb.Data)
		return nil
	}
	return b.conversationTimeChoice(ctx, cb.Message.Chat.ID, current, state, text)
}

func (b *Bot) handleBookingConfirmCallback(ctx context.Context, cb *telegram.CallbackQuery) error {
	current, err := b.userFromCallback(ctx, cb)
	if err != nil {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(LangRU, "register_failed"))
	}
	state, err := b.store.GetConversationState(ctx, current.TelegramID)
	if err != nil || state.Step != conversationStepBookingConfirm {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(current.Language, "book_need_schedule"))
	}
	choice := strings.TrimPrefix(cb.Data, "bookconfirm:")
	return b.conversationBookingConfirm(ctx, cb.Message.Chat.ID, current, state, choice)
}

func (b *Bot) handleBookingEditCallback(ctx context.Context, cb *telegram.CallbackQuery) error {
	current, err := b.userFromCallback(ctx, cb)
	if err != nil {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(LangRU, "register_failed"))
	}
	state, err := b.store.GetConversationState(ctx, current.TelegramID)
	if err != nil || state.Step != conversationStepBookingEdit {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(current.Language, "book_need_schedule"))
	}
	return b.conversationBookingEdit(ctx, cb.Message.Chat.ID, current, state, strings.TrimPrefix(cb.Data, "bookedit:"))
}

func (b *Bot) handleCancelBookingCallback(ctx context.Context, cb *telegram.CallbackQuery) error {
	current, err := b.userFromCallback(ctx, cb)
	if err != nil {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(LangRU, "register_failed"))
	}
	if !isAdmin(current.Role) {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(current.Language, "admin_only"))
	}
	rawID := strings.TrimPrefix(cb.Data, "cancel:")
	bookingID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || bookingID <= 0 {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(current.Language, "cancel_failed"))
	}
	result, err := b.store.DeleteBookingByID(ctx, current.TelegramID, bookingID)
	if err != nil {
		b.logger.Printf("cancel callback failed admin=%d booking=%d: %v", current.TelegramID, bookingID, err)
		return b.sendText(ctx, cb.Message.Chat.ID, tr(current.Language, "cancel_failed"))
	}
	b.notifyBookingChange(ctx, "cancelled", result, cb.Message.Chat.ID)
	client := formatClientContact(result.Username)
	if client == "" {
		client = tr(current.Language, "unknown_user")
	}
	return b.sendTextWithKeyboard(ctx, cb.Message.Chat.ID, tr(current.Language, "cancel_ok_contact", client), keyboardForUser(current))
}

func (b *Bot) handleMyBookingsCallback(ctx context.Context, cb *telegram.CallbackQuery) error {
	current, err := b.userFromCallback(ctx, cb)
	if err != nil {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(LangRU, "register_failed"))
	}
	return b.handleMy(ctx, cb.Message.Chat.ID, current)
}

func (b *Bot) handleMyCancelListCallback(ctx context.Context, cb *telegram.CallbackQuery) error {
	current, err := b.userFromCallback(ctx, cb)
	if err != nil {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(LangRU, "register_failed"))
	}
	return b.showMyCancelBookingPicker(ctx, cb.Message.Chat.ID, current)
}

func (b *Bot) handleMyMoveListCallback(ctx context.Context, cb *telegram.CallbackQuery) error {
	current, err := b.userFromCallback(ctx, cb)
	if err != nil {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(LangRU, "register_failed"))
	}
	return b.showMyMoveBookingPicker(ctx, cb.Message.Chat.ID, current)
}

func (b *Bot) handleMyCancelBookingCallback(ctx context.Context, cb *telegram.CallbackQuery) error {
	current, err := b.userFromCallback(ctx, cb)
	if err != nil {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(LangRU, "register_failed"))
	}
	rawID := strings.TrimPrefix(cb.Data, "mycancel:")
	bookingID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || bookingID <= 0 {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(current.Language, "my_cancel_failed"))
	}
	result, err := b.store.DeleteMyBookingByID(ctx, current.TelegramID, bookingID)
	if err != nil {
		b.logger.Printf("my cancel callback failed user=%d booking=%d: %v", current.TelegramID, bookingID, err)
		return b.sendText(ctx, cb.Message.Chat.ID, tr(current.Language, "my_cancel_failed"))
	}
	b.notifyBookingChange(ctx, "cancelled", result, cb.Message.Chat.ID)
	return b.sendTextWithKeyboard(ctx, cb.Message.Chat.ID, tr(current.Language, "my_cancel_ok", result.StartAt.Format(dateTimeLayout)), keyboardForUser(current))
}

func (b *Bot) handleMyMoveBookingCallback(ctx context.Context, cb *telegram.CallbackQuery) error {
	current, err := b.userFromCallback(ctx, cb)
	if err != nil {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(LangRU, "register_failed"))
	}
	rawID := strings.TrimPrefix(cb.Data, "mymove:")
	bookingID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || bookingID <= 0 {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(current.Language, "my_move_failed"))
	}
	return b.showMyMoveSlots(ctx, cb.Message.Chat.ID, current, bookingID)
}

func (b *Bot) handleMyMoveSlotCallback(ctx context.Context, cb *telegram.CallbackQuery) error {
	current, err := b.userFromCallback(ctx, cb)
	if err != nil {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(LangRU, "register_failed"))
	}
	parts := strings.Split(strings.TrimPrefix(cb.Data, "moveslot:"), ":")
	if len(parts) != 2 {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(current.Language, "my_move_failed"))
	}
	bookingID, err1 := strconv.ParseInt(parts[0], 10, 64)
	slotIndex, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || bookingID <= 0 || slotIndex <= 0 {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(current.Language, "my_move_failed"))
	}
	result, err := b.store.MoveMyBookingByIDToIndex(ctx, current.TelegramID, bookingID, slotIndex)
	if err != nil {
		b.logger.Printf("my move slot failed user=%d booking=%d slot=%d: %v", current.TelegramID, bookingID, slotIndex, err)
		return b.sendText(ctx, cb.Message.Chat.ID, tr(current.Language, "my_move_failed"))
	}
	b.notifyMove(ctx, result)
	return b.sendTextWithKeyboard(ctx, cb.Message.Chat.ID, tr(current.Language, "move_ok", result.FromStart.Format(dateTimeLayout), result.ToStart.Format(dateTimeLayout)), keyboardForUser(current))
}

func (b *Bot) handleBookingStartCallback(ctx context.Context, cb *telegram.CallbackQuery) error {
	current, err := b.userFromCallback(ctx, cb)
	if err != nil {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(LangRU, "register_failed"))
	}
	return b.handleBookingStart(ctx, cb.Message.Chat.ID, current)
}

func (b *Bot) userFromCallback(ctx context.Context, cb *telegram.CallbackQuery) (UserRecord, error) {
	user := UserRecord{
		TelegramID: cb.From.ID,
		Username:   normalizeUsername(cb.From.Username),
		FirstName:  cb.From.FirstName,
		LastName:   cb.From.LastName,
		Role:       RoleUser,
		Language:   LangRU,
	}
	if user.Username == b.superAdminUsername {
		user.Role = RoleSuperAdmin
	}
	current, err := b.store.RegisterOrUpdateUser(ctx, user)
	if err != nil {
		return UserRecord{}, err
	}
	return b.applySuperAdminView(ctx, current), nil
}

func callbackText(data string) (string, bool) {
	key, value, ok := strings.Cut(data, ":")
	if !ok {
		return "", false
	}
	switch key {
	case "lang":
		if value == LangRU || value == LangEN {
			return value, true
		}
	case "cat", "sub", "svc", "slot":
		if _, err := strconv.Atoi(value); err == nil {
			return value, true
		}
	case "more":
		if value == "yes" || value == "no" {
			return value, true
		}
	case "time":
		if value == "nearest" || value == "dates" {
			return value, true
		}
	case "hours":
		if value == "off" {
			return value, true
		}
	case "contact":
		if value == "telegram" || value == "phone" {
			return value, true
		}
	case "viewadmin":
		username := normalizeUsername(value)
		if username != "" {
			return username, true
		}
	}
	return "", false
}

func (b *Bot) handleStart(ctx context.Context, chatID int64, user UserRecord) error {
	if user.Role == RoleUser && !user.LanguageSet {
		if err := b.store.SetConversationState(ctx, user.TelegramID, ConversationState{Step: conversationStepLanguage}); err != nil {
			return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
		}
		return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "choose_language"), languageKeyboard())
	}
	return b.showStartOverview(ctx, chatID, user)
}

func (b *Bot) showStartOverview(ctx context.Context, chatID int64, user UserRecord) error {
	name := strings.TrimSpace(user.FirstName)
	if name == "" {
		name = formatClientContact(user.Username)
	}
	if name == "" {
		name = tr(user.Language, "start_guest")
	}
	if user.Role == RoleUser {
		services, err := b.store.ListServices(ctx, user.TelegramID)
		text := b.clientStartGreeting(ctx, user, name, services)
		if err != nil {
			b.logger.Printf("start overview services failed user=%d: %v", user.TelegramID, err)
			text += "\n\n" + tr(user.Language, "start_services_unavailable")
		} else {
			text += "\n\n" + formatStartServices(user.Language, services, 16)
		}
		return b.sendTextWithKeyboard(ctx, chatID, text, keyboardForUser(user))
	}

	txt := tr(user.Language, "start_admin", name)
	if user.ActualRole == RoleSuperAdmin && user.ViewRole != "" && user.ViewRole != RoleSuperAdmin {
		txt += "\n" + viewModeText(user.Language, user)
	}
	return b.sendTextWithKeyboard(ctx, chatID, txt, keyboardForUser(user))
}

func formatStartServices(lang string, services []ServiceView, limit int) string {
	if len(services) == 0 {
		return tr(lang, "start_services_empty")
	}
	if limit <= 0 || limit > len(services) {
		limit = len(services)
	}
	var sb strings.Builder
	sb.WriteString(tr(lang, "start_services_header"))
	for _, service := range services[:limit] {
		path := make([]string, 0, 3)
		for _, part := range []string{service.Category, service.Subcategory, service.Name} {
			if part = strings.TrimSpace(part); part != "" {
				path = append(path, part)
			}
		}
		sb.WriteString("\n• ")
		sb.WriteString(strings.Join(path, " / "))
		sb.WriteString(" — ")
		sb.WriteString(strconv.Itoa(service.DurationMin))
		sb.WriteString(" ")
		sb.WriteString(tr(lang, "minutes_short"))
		if description := strings.Join(strings.Fields(service.Description), " "); description != "" {
			sb.WriteString(" — ")
			sb.WriteString(description)
		}
		if service.AdminName != "" {
			sb.WriteString(" · @")
			sb.WriteString(service.AdminName)
		}
	}
	if len(services) > limit {
		sb.WriteString("\n")
		sb.WriteString(tr(lang, "start_services_more", len(services)-limit))
	}
	return sb.String()
}

func (b *Bot) handleBookingStart(ctx context.Context, chatID int64, user UserRecord) error {
	if err := b.store.ClearConversationState(ctx, user.TelegramID); err != nil {
		b.logger.Printf("booking start: clear state failed user=%d: %v", user.TelegramID, err)
	}
	return b.askCategory(ctx, chatID, user, nil)
}

func (b *Bot) handleMenuButton(ctx context.Context, chatID int64, user UserRecord, text string) (bool, error) {
	action := menuButtonAction(user.Language, text)
	if action == "start_booking" {
		return true, b.handleBookingStart(ctx, chatID, user)
	}
	if action == "action_view_super" {
		return true, b.handleViewSuper(ctx, chatID, user)
	}
	if action == "action_my" {
		return true, b.handleMy(ctx, chatID, user)
	}
	if action == "menu_calendar" && !isAdmin(user.Role) {
		return true, b.handleCalendar(ctx, chatID, user, []string{"/calendar"})
	}
	if !isAdmin(user.Role) {
		return false, nil
	}
	switch action {
	case "menu_calendar":
		return true, b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "menu_calendar_text"), calendarMenuKeyboard(user.Language))
	case "menu_bookings":
		return true, b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "menu_bookings_text"), bookingMenuKeyboard(user.Language))
	case "menu_services":
		return true, b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "menu_services_text"), servicesMenuKeyboard(user.Language))
	case "menu_schedule":
		return true, b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "menu_schedule_text"), scheduleMenuKeyboard(user.Language))
	case "menu_settings":
		return true, b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "menu_settings_text"), settingsMenuKeyboard(user.Language))
	case "menu_finance":
		if !isAdmin(user.Role) {
			return true, b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
		}
		return true, b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "menu_finance_text"), financeMenuKeyboard(user.Language))
	case "menu_admins":
		if user.ActualRole != RoleSuperAdmin {
			return true, b.sendText(ctx, chatID, tr(user.Language, "super_only_role"))
		}
		return true, b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "menu_admins_text"), adminsMenuKeyboard(user.Language))
	case "back", "main":
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return true, b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "menu_main_text"), keyboardForUser(user))
	case "action_calendar":
		return true, b.handleCalendar(ctx, chatID, user, []string{"/calendar"})
	case "action_week":
		return true, b.handleWeek(ctx, chatID, user, []string{"/week"})
	case "action_free":
		return true, b.handleFree(ctx, chatID, user, []string{"/schedule"})
	case "action_book":
		return true, b.handleBookingStart(ctx, chatID, user)
	case "action_client_bookings":
		return true, b.handleAdminBookings(ctx, chatID, user, []string{"/bookings"})
	case "action_bookings_today":
		return true, b.handleAdminBookings(ctx, chatID, user, []string{"/bookings", "today"})
	case "action_bookings_tomorrow":
		return true, b.handleAdminBookings(ctx, chatID, user, []string{"/bookings", "tomorrow"})
	case "action_service_add":
		return true, b.handleServiceAdd(ctx, chatID, user, []string{"/service_add"})
	case "action_service_edit":
		return true, b.handleServiceEdit(ctx, chatID, user, []string{"/service_edit"})
	case "action_service_delete":
		return true, b.handleServiceDelete(ctx, chatID, user, []string{"/service_delete"})
	case "action_service_list":
		return true, b.handleServices(ctx, chatID, user)
	case "action_service_replace":
		return true, b.beginServiceCatalogReplace(ctx, chatID, user)
	case "action_category_order":
		return true, b.handleCategoryOrder(ctx, chatID, user, []string{"/category_order"})
	case "action_set_hours":
		return true, b.handleSetHours(ctx, chatID, user, "/sethours")
	case "action_block_date":
		return true, b.handleBlockDate(ctx, chatID, user, []string{"/block_day"})
	case "action_set_duration":
		return true, b.handleSetDuration(ctx, chatID, user, []string{"/setduration"})
	case "action_schedule_change":
		return true, b.beginGenerateMode(ctx, chatID, user)
	case "action_generate":
		return true, b.beginGenerateMode(ctx, chatID, user)
	case "action_generate_month":
		return true, b.beginScheduleGenerateFlow(ctx, chatID, user, "month")
	case "action_generate_months":
		return true, b.beginScheduleGenerateFlow(ctx, chatID, user, "months")
	case "action_generate_day":
		return true, b.beginScheduleGenerateFlow(ctx, chatID, user, "day")
	case "action_generate_weekday":
		return true, b.beginScheduleGenerateFlow(ctx, chatID, user, "weekday")
	case "action_delete_month":
		return true, b.handleScheduleDelete(ctx, chatID, user, []string{"/calendar_delete"})
	case "action_set_profile":
		return true, b.handleSetProfile(ctx, chatID, user, "/setprofile")
	case "action_contact_aliases":
		return true, b.sendContactAliases(ctx, chatID, user)
	case "action_lang_ru":
		return true, b.handleLanguage(ctx, chatID, user, []string{"/lang", LangRU})
	case "action_lang_en":
		return true, b.handleLanguage(ctx, chatID, user, []string{"/lang", LangEN})
	case "action_admin_add":
		return true, b.handleAdminAdd(ctx, chatID, user, []string{"/admin_add"})
	case "action_admin_remove":
		return true, b.handleAdminRemove(ctx, chatID, user, []string{"/admin_remove"})
	case "action_admin_list":
		return true, b.handleAdminList(ctx, chatID, user)
	case "action_view_admin":
		return true, b.handleViewAdmin(ctx, chatID, user, []string{"/view_admin"})
	case "action_view_user":
		return true, b.handleViewUser(ctx, chatID, user)
	case "action_role":
		return true, b.handleRole(ctx, chatID, user, []string{"/role"})
	case "action_appoint":
		return true, b.handleAppoint(ctx, chatID, user, []string{"/appoint"})
	case "action_cancel":
		return true, b.handleCancel(ctx, chatID, user, []string{"/cancel"})
	case "action_reschedule":
		return true, b.handleReschedule(ctx, chatID, user, []string{"/reschedule"})
	case "action_block":
		return true, b.handleBlock(ctx, chatID, user, []string{"/block"})
	case "finance_month":
		return true, b.sendFinanceReport(ctx, chatID, user, "month")
	case "finance_quarter":
		return true, b.sendFinanceReport(ctx, chatID, user, "quarter")
	case "finance_year":
		return true, b.sendFinanceReport(ctx, chatID, user, "year")
	case "finance_add_income":
		return true, b.beginFinanceInput(ctx, chatID, user, "income", "")
	case "finance_add_expense":
		return true, b.beginFinanceInput(ctx, chatID, user, "expense", "")
	case "finance_chart":
		if !isAdmin(user.Role) {
			return true, b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
		}
		return true, b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "finance_chart_choose"), financeChartPeriodKeyboard(user.Language))
	default:
		return false, nil
	}
}

func menuButtonAction(lang, text string) string {
	buttons := []struct {
		action string
		key    string
	}{
		{"menu_calendar", "button_menu_calendar"},
		{"start_booking", "button_start_booking"},
		{"menu_bookings", "button_menu_bookings"},
		{"menu_services", "button_menu_services"},
		{"menu_schedule", "button_menu_schedule"},
		{"menu_settings", "button_menu_settings"},
		{"menu_finance", "button_menu_finance"},
		{"menu_admins", "button_menu_admins"},
		{"back", "button_back"},
		{"main", "button_main"},
		{"action_calendar", "button_action_calendar"},
		{"action_week", "button_action_week"},
		{"action_free", "button_action_free"},
		{"action_book", "button_action_book"},
		{"action_my", "button_action_my"},
		{"action_client_bookings", "button_action_client_bookings"},
		{"action_bookings_today", "button_action_bookings_today"},
		{"action_bookings_tomorrow", "button_action_bookings_tomorrow"},
		{"action_service_add", "button_action_service_add"},
		{"action_service_edit", "button_action_service_edit"},
		{"action_service_delete", "button_action_service_delete"},
		{"action_service_list", "button_action_service_list"},
		{"action_service_replace", "button_action_service_replace"},
		{"action_category_order", "button_action_category_order"},
		{"action_set_hours", "button_action_set_hours"},
		{"action_block_date", "button_action_block_date"},
		{"action_set_duration", "button_action_set_duration"},
		{"action_schedule_change", "button_action_schedule_change"},
		{"action_generate", "button_action_generate"},
		{"action_generate_month", "button_action_generate_month"},
		{"action_generate_months", "button_action_generate_months"},
		{"action_generate_day", "button_action_generate_day"},
		{"action_generate_weekday", "button_action_generate_weekday"},
		{"action_delete_month", "button_action_delete_month"},
		{"action_set_profile", "button_action_set_profile"},
		{"action_contact_aliases", "button_action_contact_aliases"},
		{"action_lang_ru", "button_action_lang_ru"},
		{"action_lang_en", "button_action_lang_en"},
		{"action_admin_add", "button_action_admin_add"},
		{"action_admin_remove", "button_action_admin_remove"},
		{"action_admin_list", "button_action_admin_list"},
		{"action_view_admin", "button_action_view_admin"},
		{"action_view_user", "button_action_view_user"},
		{"action_view_super", "button_action_view_super"},
		{"action_role", "button_action_role"},
		{"action_appoint", "button_action_appoint"},
		{"action_cancel", "button_action_cancel"},
		{"action_reschedule", "button_action_reschedule"},
		{"action_block", "button_action_block"},
		{"finance_month", "button_finance_month"},
		{"finance_quarter", "button_finance_quarter"},
		{"finance_year", "button_finance_year"},
		{"finance_add_income", "button_finance_add_income"},
		{"finance_add_expense", "button_finance_add_expense"},
		{"finance_chart", "button_finance_chart"},
	}
	normalized := normalizeMenuButton(text)
	for _, button := range buttons {
		if normalized == normalizeMenuButton(tr(lang, button.key)) ||
			normalized == normalizeMenuButton(tr(LangRU, button.key)) ||
			normalized == normalizeMenuButton(tr(LangEN, button.key)) {
			return button.action
		}
	}
	return ""
}

func normalizeMenuButton(text string) string {
	return strings.ToLower(strings.TrimSpace(text))
}

func (b *Bot) beginConversation(ctx context.Context, chatID int64, user UserRecord, state ConversationState, messageKey string, kb *telegram.ReplyMarkup) error {
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	if kb != nil {
		return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, messageKey), kb)
	}
	return b.sendText(ctx, chatID, tr(user.Language, messageKey))
}

func (b *Bot) sendHelp(ctx context.Context, chatID int64, user UserRecord) error {
	text := tr(user.Language, "help_base")
	if user.Role == RoleAdmin || user.Role == RoleSuperAdmin {
		text += "\nAdmin:\n" + tr(user.Language, "help_admin")
	}
	if user.ActualRole == RoleSuperAdmin {
		text += "\nSuper admin:\n" + tr(user.Language, "help_super")
	}
	if user.ActualRole == RoleSuperAdmin && user.ViewRole != "" && user.ViewRole != RoleSuperAdmin {
		text += "\n" + viewModeText(user.Language, user)
	}
	return b.sendTextWithKeyboard(ctx, chatID, text, keyboardForUser(user))
}

func (b *Bot) applySuperAdminView(ctx context.Context, user UserRecord) UserRecord {
	if user.ActualRole == "" {
		user.ActualRole = user.Role
	}
	if user.ActualRole != RoleSuperAdmin {
		return user
	}
	view, err := b.store.GetSuperAdminView(ctx, user.TelegramID)
	if err != nil {
		return user
	}
	switch view.Role {
	case RoleAdmin:
		user.Role = RoleAdmin
		user.ViewRole = RoleAdmin
		user.ViewAdminName = normalizeUsername(view.AdminUsername)
	case RoleUser:
		user.Role = RoleUser
		user.ViewRole = RoleUser
		user.ViewAdminName = ""
	}
	return user
}

func (b *Bot) handleViewAdmin(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if actor.ActualRole != RoleSuperAdmin {
		return b.sendText(ctx, chatID, tr(actor.Language, "super_only_role"))
	}
	if len(parts) < 2 {
		return b.askViewAdmin(ctx, chatID, actor)
	}
	username := normalizeUsername(parts[1])
	if username == "" {
		return b.sendText(ctx, chatID, tr(actor.Language, "bad_username"))
	}
	if err := b.store.SetSuperAdminView(ctx, actor.TelegramID, SuperAdminView{Role: RoleAdmin, AdminUsername: username}); err != nil {
		b.logger.Printf("set view admin failed user=%d target=%q: %v", actor.TelegramID, username, err)
		return b.sendText(ctx, chatID, tr(actor.Language, "view_admin_failed"))
	}
	actor.Role = RoleAdmin
	actor.ViewRole = RoleAdmin
	actor.ViewAdminName = username
	return b.sendTextWithKeyboard(ctx, chatID, tr(actor.Language, "view_admin_ok", username), keyboardForUser(actor))
}

func (b *Bot) askViewAdmin(ctx context.Context, chatID int64, actor UserRecord) error {
	admins, err := b.store.ListAdmins(ctx)
	if err != nil {
		b.logger.Printf("view admin list failed user=%d: %v", actor.TelegramID, err)
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_list_failed"))
	}
	if len(admins) == 0 {
		return b.sendText(ctx, chatID, tr(actor.Language, "view_admin_empty"))
	}
	if err := b.store.SetConversationState(ctx, actor.TelegramID, ConversationState{Step: conversationStepViewAdmin}); err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "conversation_failed"))
	}
	return b.sendTextWithKeyboard(ctx, chatID, tr(actor.Language, "view_admin_ask_username"), viewAdminKeyboard(actor.Language, admins))
}

func (b *Bot) handleViewUser(ctx context.Context, chatID int64, actor UserRecord) error {
	if actor.ActualRole != RoleSuperAdmin {
		return b.sendText(ctx, chatID, tr(actor.Language, "super_only_role"))
	}
	if err := b.store.SetSuperAdminView(ctx, actor.TelegramID, SuperAdminView{Role: RoleUser}); err != nil {
		b.logger.Printf("set view user failed user=%d: %v", actor.TelegramID, err)
		return b.sendText(ctx, chatID, tr(actor.Language, "view_user_failed"))
	}
	actor.Role = RoleUser
	actor.ViewRole = RoleUser
	actor.ViewAdminName = ""
	return b.sendTextWithKeyboard(ctx, chatID, tr(actor.Language, "view_user_ok"), keyboardForUser(actor))
}

func (b *Bot) handleViewSuper(ctx context.Context, chatID int64, actor UserRecord) error {
	if actor.ActualRole != RoleSuperAdmin {
		return b.sendText(ctx, chatID, tr(actor.Language, "super_only_role"))
	}
	if err := b.store.SetSuperAdminView(ctx, actor.TelegramID, SuperAdminView{Role: RoleSuperAdmin}); err != nil {
		b.logger.Printf("reset view failed user=%d: %v", actor.TelegramID, err)
		return b.sendText(ctx, chatID, tr(actor.Language, "view_super_failed"))
	}
	actor.Role = RoleSuperAdmin
	actor.ViewRole = ""
	actor.ViewAdminName = ""
	return b.sendTextWithKeyboard(ctx, chatID, tr(actor.Language, "view_super_ok"), keyboardForUser(actor))
}

func viewModeText(lang string, user UserRecord) string {
	if user.ViewRole == RoleAdmin && user.ViewAdminName != "" {
		return tr(lang, "view_mode_admin", user.ViewAdminName)
	}
	if user.ViewRole == RoleUser {
		return tr(lang, "view_mode_user")
	}
	return ""
}

func (b *Bot) handleAdminList(ctx context.Context, chatID int64, actor UserRecord) error {
	if actor.ActualRole != RoleSuperAdmin {
		return b.sendText(ctx, chatID, tr(actor.Language, "super_only_role"))
	}
	admins, err := b.store.ListAdmins(ctx)
	if err != nil {
		b.logger.Printf("admin list failed user=%d: %v", actor.TelegramID, err)
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_list_failed"))
	}
	if len(admins) == 0 {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_list_empty"))
	}
	var sb strings.Builder
	sb.WriteString(tr(actor.Language, "admin_list_header"))
	for i, admin := range admins {
		sb.WriteString(strconv.Itoa(i + 1))
		sb.WriteString(". @")
		sb.WriteString(admin.Username)
		sb.WriteString(" - ")
		sb.WriteString(roleLabel(actor.Language, admin.Role))
		sb.WriteString(", ")
		sb.WriteString(tr(actor.Language, "admin_list_counts", admin.ActiveServices, admin.OpenSlots, admin.BookedSlots))
		sb.WriteString("\n")
	}
	return b.sendText(ctx, chatID, sb.String())
}

func (b *Bot) handleAdminAdd(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if actor.ActualRole != RoleSuperAdmin {
		return b.sendText(ctx, chatID, tr(actor.Language, "super_only_add"))
	}
	if len(parts) < 2 {
		return b.beginConversation(ctx, chatID, actor, ConversationState{Step: conversationStepAdminAdd}, "admin_add_ask_username", nil)
	}
	username := normalizeUsername(parts[1])
	if username == "" {
		return b.sendText(ctx, chatID, tr(actor.Language, "bad_username"))
	}
	if err := b.store.SetUserRole(ctx, username, RoleAdmin); err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_add_failed"))
	}
	return b.sendText(ctx, chatID, tr(actor.Language, "admin_added", username))
}

func (b *Bot) handleAdminRemove(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if actor.ActualRole != RoleSuperAdmin {
		return b.sendText(ctx, chatID, tr(actor.Language, "super_only_remove"))
	}
	if len(parts) < 2 {
		return b.beginConversation(ctx, chatID, actor, ConversationState{Step: conversationStepAdminRemove}, "admin_remove_ask_username", nil)
	}
	username := normalizeUsername(parts[1])
	if username == "" {
		return b.sendText(ctx, chatID, tr(actor.Language, "bad_username"))
	}
	if err := b.store.SetUserRole(ctx, username, RoleUser); err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_remove_failed"))
	}
	return b.sendText(ctx, chatID, tr(actor.Language, "admin_removed", username))
}

func (b *Bot) handleRole(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if actor.ActualRole != RoleSuperAdmin {
		return b.sendText(ctx, chatID, tr(actor.Language, "super_only_role"))
	}
	if len(parts) < 2 {
		return b.beginConversation(ctx, chatID, actor, ConversationState{Step: conversationStepRoleUser}, "role_ask_username", nil)
	}
	username := normalizeUsername(parts[1])
	if username == "" {
		return b.sendText(ctx, chatID, tr(actor.Language, "bad_username"))
	}
	if len(parts) == 2 {
		user, err := b.store.GetUserByUsername(ctx, username)
		if err != nil {
			return b.sendText(ctx, chatID, tr(actor.Language, "role_show_failed"))
		}
		return b.sendText(ctx, chatID, tr(actor.Language, "role_current", username, user.Role))
	}
	role := Role(strings.ToLower(parts[2]))
	if role != RoleUser && role != RoleAdmin && role != RoleSuperAdmin {
		return b.sendText(ctx, chatID, tr(actor.Language, "role_bad"))
	}
	if err := b.store.SetUserRole(ctx, username, role); err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "role_set_failed"))
	}
	return b.sendText(ctx, chatID, tr(actor.Language, "role_set", username, role))
}

func (b *Bot) handleLanguage(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if len(parts) < 2 {
		return b.sendText(ctx, chatID, tr(actor.Language, "lang_usage"))
	}
	lang := strings.ToLower(parts[1])
	if lang != LangRU && lang != LangEN {
		return b.sendText(ctx, chatID, tr(actor.Language, "lang_usage"))
	}
	if err := b.store.SetUserLanguage(ctx, actor.TelegramID, lang); err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "lang_failed"))
	}
	return b.sendText(ctx, chatID, tr(lang, "lang_set"))
}

func (b *Bot) handleSetProfile(ctx context.Context, chatID int64, actor UserRecord, text string) error {
	if !isAdmin(actor.Role) {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_only"))
	}
	value := strings.TrimSpace(strings.TrimPrefix(text, "/setprofile"))
	if value == "" {
		return b.askSetProfile(ctx, chatID, actor)
	}
	if err := b.store.SetProfileText(ctx, actor.TelegramID, value); err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "profile_failed"))
	}
	return b.sendText(ctx, chatID, tr(actor.Language, "profile_ok"))
}

func (b *Bot) askSetProfile(ctx context.Context, chatID int64, actor UserRecord) error {
	if !isAdmin(actor.Role) {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_only"))
	}
	current, err := b.store.GetProfileText(ctx, actor.TelegramID)
	if err != nil {
		b.logger.Printf("get profile text failed admin=%d: %v", actor.TelegramID, err)
		return b.sendText(ctx, chatID, tr(actor.Language, "profile_failed"))
	}
	if err := b.store.SetConversationState(ctx, actor.TelegramID, ConversationState{Step: conversationStepSetProfile}); err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "conversation_failed"))
	}
	var sb strings.Builder
	if strings.TrimSpace(current) == "" {
		sb.WriteString(tr(actor.Language, "profile_current_empty"))
	} else {
		sb.WriteString(tr(actor.Language, "profile_current"))
		sb.WriteString("\n")
		sb.WriteString(strings.TrimSpace(current))
	}
	sb.WriteString("\n\n")
	sb.WriteString(tr(actor.Language, "profile_ask_text"))
	return b.sendText(ctx, chatID, sb.String())
}

func (b *Bot) handleCategoryOrder(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if !isAdmin(actor.Role) {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_only"))
	}
	services, err := b.store.ListServices(ctx, actor.TelegramID)
	if err != nil {
		b.logger.Printf("category order list failed admin=%d: %v", actor.TelegramID, err)
		return b.sendText(ctx, chatID, tr(actor.Language, "services_list_failed"))
	}
	categories := serviceCategories(services)
	if len(categories) == 0 {
		return b.sendText(ctx, chatID, tr(actor.Language, "services_empty"))
	}
	if len(parts) > 1 {
		return b.applyCategoryOrder(ctx, chatID, actor, categories, strings.Join(parts[1:], " "))
	}
	if err := b.store.SetConversationState(ctx, actor.TelegramID, ConversationState{Step: conversationStepCategoryOrd}); err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "conversation_failed"))
	}
	var sb strings.Builder
	sb.WriteString(tr(actor.Language, "category_order_ask"))
	sb.WriteString("\n")
	sb.WriteString(formatCategories(actor.Language, categories))
	return b.sendText(ctx, chatID, sb.String())
}

func (b *Bot) applyCategoryOrder(ctx context.Context, chatID int64, actor UserRecord, categories []string, raw string) error {
	ordered, ok := parseCategoryOrder(raw, categories)
	if !ok {
		var sb strings.Builder
		sb.WriteString(tr(actor.Language, "category_order_bad"))
		sb.WriteString("\n")
		sb.WriteString(formatCategories(actor.Language, categories))
		return b.sendText(ctx, chatID, sb.String())
	}
	if err := b.store.SetCategoryOrder(ctx, actor.TelegramID, ordered); err != nil {
		b.logger.Printf("category order save failed admin=%d order=%v: %v", actor.TelegramID, ordered, err)
		return b.sendText(ctx, chatID, tr(actor.Language, "category_order_failed"))
	}
	_ = b.store.ClearConversationState(ctx, actor.TelegramID)
	return b.sendTextWithKeyboard(ctx, chatID, tr(actor.Language, "category_order_ok"), keyboardForUser(actor))
}

func (b *Bot) handleServiceAdd(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if !isAdmin(actor.Role) {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_only"))
	}
	if len(parts) == 1 {
		return b.askServiceAddCategory(ctx, chatID, actor, ConversationState{Step: conversationStepAddSvcCat})
	}
	if len(parts) < 3 {
		return b.sendText(ctx, chatID, tr(actor.Language, "service_add_usage"))
	}
	duration, err := strconv.Atoi(parts[1])
	if err != nil || duration <= 0 {
		return b.sendText(ctx, chatID, tr(actor.Language, "duration_bad"))
	}
	name := strings.TrimSpace(strings.Join(parts[2:], " "))
	name, description := splitServiceNameDescription(name)
	if name == "" {
		return b.sendText(ctx, chatID, tr(actor.Language, "service_add_usage"))
	}
	if err := b.store.AddService(ctx, actor.TelegramID, name, duration, description); err != nil {
		b.logger.Printf("service add failed admin=%d duration=%d name=%q: %v", actor.TelegramID, duration, name, err)
		return b.sendText(ctx, chatID, tr(actor.Language, "service_add_failed"))
	}
	b.logger.Printf("service added admin=%d duration=%d name=%q", actor.TelegramID, duration, name)
	return b.sendText(ctx, chatID, tr(actor.Language, "service_add_ok", name, duration))
}

func (b *Bot) handleServices(ctx context.Context, chatID int64, actor UserRecord) error {
	services, err := b.store.ListServices(ctx, actor.TelegramID)
	if err != nil {
		b.logger.Printf("services list failed user=%d role=%s: %v", actor.TelegramID, actor.Role, err)
		return b.sendText(ctx, chatID, tr(actor.Language, "services_list_failed"))
	}
	b.logger.Printf("services list user=%d role=%s count=%d", actor.TelegramID, actor.Role, len(services))
	if len(services) == 0 {
		return b.sendText(ctx, chatID, tr(actor.Language, "services_empty"))
	}
	var sb strings.Builder
	sb.WriteString(formatServices(actor.Language, services))
	sb.WriteString(tr(actor.Language, "services_footer"))
	return b.sendText(ctx, chatID, sb.String())
}

func (b *Bot) handleServiceEdit(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if !isAdmin(actor.Role) {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_only"))
	}
	if len(parts) == 1 {
		return b.askServiceEdit(ctx, chatID, actor)
	}
	if len(parts) < 4 {
		return b.sendText(ctx, chatID, tr(actor.Language, "service_edit_usage"))
	}
	index, err := strconv.Atoi(parts[1])
	if err != nil || index <= 0 {
		return b.sendText(ctx, chatID, tr(actor.Language, "service_edit_usage"))
	}
	duration, err := strconv.Atoi(parts[2])
	if err != nil || duration <= 0 {
		return b.sendText(ctx, chatID, tr(actor.Language, "duration_bad"))
	}
	name := strings.TrimSpace(strings.Join(parts[3:], " "))
	name, description := splitServiceNameDescription(name)
	if name == "" {
		return b.sendText(ctx, chatID, tr(actor.Language, "service_edit_usage"))
	}
	if err := b.store.EditServiceByIndex(ctx, actor.TelegramID, index, name, duration, description); err != nil {
		b.logger.Printf("service edit failed admin=%d index=%d duration=%d name=%q: %v", actor.TelegramID, index, duration, name, err)
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalidArgument) {
			return b.sendText(ctx, chatID, tr(actor.Language, "service_edit_bad_index"))
		}
		return b.sendText(ctx, chatID, tr(actor.Language, "service_edit_failed"))
	}
	return b.sendText(ctx, chatID, tr(actor.Language, "service_edit_ok", index))
}

func (b *Bot) handleServiceDelete(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if !isAdmin(actor.Role) {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_only"))
	}
	if len(parts) < 2 {
		return b.askServiceDelete(ctx, chatID, actor)
	}
	index, err := strconv.Atoi(parts[1])
	if err != nil || index <= 0 {
		return b.sendText(ctx, chatID, tr(actor.Language, "service_delete_usage"))
	}
	if err := b.store.DeleteServiceByIndex(ctx, actor.TelegramID, index); err != nil {
		b.logger.Printf("service delete failed admin=%d index=%d: %v", actor.TelegramID, index, err)
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalidArgument) {
			return b.sendText(ctx, chatID, tr(actor.Language, "service_delete_bad_index"))
		}
		return b.sendText(ctx, chatID, tr(actor.Language, "service_delete_failed"))
	}
	return b.sendText(ctx, chatID, tr(actor.Language, "service_delete_ok", index))
}

func (b *Bot) askServiceEdit(ctx context.Context, chatID int64, actor UserRecord) error {
	services, err := b.store.ListServices(ctx, actor.TelegramID)
	if err != nil {
		b.logger.Printf("service edit list failed admin=%d: %v", actor.TelegramID, err)
		return b.sendText(ctx, chatID, tr(actor.Language, "services_list_failed"))
	}
	if len(services) == 0 {
		return b.sendText(ctx, chatID, tr(actor.Language, "services_empty"))
	}
	if err := b.store.SetConversationState(ctx, actor.TelegramID, ConversationState{Step: conversationStepEditSvc}); err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "conversation_failed"))
	}
	var sb strings.Builder
	sb.WriteString(formatServices(actor.Language, services))
	sb.WriteString("\n")
	sb.WriteString(tr(actor.Language, "service_edit_ask_index"))
	return b.sendText(ctx, chatID, sb.String())
}

func (b *Bot) askServiceDelete(ctx context.Context, chatID int64, actor UserRecord) error {
	services, err := b.store.ListServices(ctx, actor.TelegramID)
	if err != nil {
		b.logger.Printf("service delete list failed admin=%d: %v", actor.TelegramID, err)
		return b.sendText(ctx, chatID, tr(actor.Language, "services_list_failed"))
	}
	if len(services) == 0 {
		return b.sendText(ctx, chatID, tr(actor.Language, "services_empty"))
	}
	if err := b.store.SetConversationState(ctx, actor.TelegramID, ConversationState{Step: conversationStepDeleteSvc}); err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "conversation_failed"))
	}
	var sb strings.Builder
	sb.WriteString(formatServices(actor.Language, services))
	sb.WriteString("\n")
	sb.WriteString(tr(actor.Language, "service_delete_ask_index"))
	return b.sendText(ctx, chatID, sb.String())
}

func (b *Bot) handleSetHours(ctx context.Context, chatID int64, actor UserRecord, text string) error {
	if !isAdmin(actor.Role) {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_only"))
	}
	value := strings.TrimSpace(strings.TrimPrefix(text, "/sethours"))
	if value == "" {
		return b.askWeeklyHoursDay(ctx, chatID, actor, ConversationState{Step: conversationStepSetHoursDay, WeekdayIndex: 0})
	}
	if err := b.store.SetWorkHoursText(ctx, actor.TelegramID, value); err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "hours_failed"))
	}
	return b.sendText(ctx, chatID, tr(actor.Language, "hours_ok"))
}

func (b *Bot) handleBlockDate(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if !isAdmin(actor.Role) {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_only"))
	}
	if len(parts) < 2 {
		return b.beginConversation(ctx, chatID, actor, ConversationState{Step: conversationStepBlockDate}, "block_date_ask", nil)
	}
	date, err := parseSingleDate(parts[1])
	if err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "block_date_bad"))
	}
	result, err := b.store.BlockScheduleDate(ctx, actor.TelegramID, date)
	if err != nil {
		b.logger.Printf("block date failed admin=%d date=%s: %v", actor.TelegramID, date.Format("2006-01-02"), err)
		return b.sendText(ctx, chatID, tr(actor.Language, "block_date_failed"))
	}
	return b.sendText(ctx, chatID, tr(actor.Language, "block_date_ok", result.Date.Format("2006-01-02"), result.ClosedSlots))
}

func (b *Bot) handleSetDuration(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if !isAdmin(actor.Role) {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_only"))
	}
	if len(parts) < 2 {
		return b.beginConversation(ctx, chatID, actor, ConversationState{Step: conversationStepSetDuration}, "duration_ask_value", nil)
	}
	duration, err := strconv.Atoi(parts[1])
	if err != nil || duration <= 0 {
		return b.sendText(ctx, chatID, tr(actor.Language, "duration_bad"))
	}
	if err := b.store.SetSessionDuration(ctx, actor.TelegramID, duration); err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "duration_failed"))
	}
	return b.sendText(ctx, chatID, tr(actor.Language, "duration_ok"))
}

func (b *Bot) handleGenerate(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if !isAdmin(actor.Role) {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_only"))
	}
	if len(parts) < 2 {
		return b.beginGenerateMode(ctx, chatID, actor)
	}
	req := GenerateScheduleRequest{Months: 1}
	if date, err := time.Parse("2006-01-02", parts[1]); err == nil {
		if len(parts) < 3 {
			return b.sendText(ctx, chatID, tr(actor.Language, "generate_usage"))
		}
		start, end, err := parseDayRange(parts[2])
		if err != nil {
			return b.sendText(ctx, chatID, tr(actor.Language, "generate_bad_time"))
		}
		if end <= start {
			return b.sendText(ctx, chatID, tr(actor.Language, "generate_bad_range"))
		}
		req.Date = date
		req.DayStart = start
		req.DayEnd = end
		if len(parts) >= 4 {
			duration, err := strconv.Atoi(parts[3])
			if err != nil || duration <= 0 {
				return b.sendText(ctx, chatID, tr(actor.Language, "duration_bad"))
			}
			req.DurationMin = duration
		}
		result, err := b.store.GenerateSchedule(ctx, actor.TelegramID, req)
		if err != nil {
			return b.sendText(ctx, chatID, tr(actor.Language, "generate_failed"))
		}
		return b.sendText(ctx, chatID, tr(actor.Language, "generate_ok", result.Created, result.Skipped))
	}
	monthStart, err := time.Parse(monthLayout, parts[1])
	if err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "generate_usage"))
	}
	req.Month = monthStart
	if len(parts) == 3 {
		months, err := strconv.Atoi(parts[2])
		if err != nil || months <= 0 {
			return b.sendText(ctx, chatID, tr(actor.Language, "generate_usage"))
		}
		req.Months = months
	} else if len(parts) >= 4 {
		days, err := parseWeekdays(parts[2])
		if err != nil {
			return b.sendText(ctx, chatID, tr(actor.Language, "generate_bad_days"))
		}
		start, end, err := parseDayRange(parts[3])
		if err != nil {
			return b.sendText(ctx, chatID, tr(actor.Language, "generate_bad_time"))
		}
		if end <= start {
			return b.sendText(ctx, chatID, tr(actor.Language, "generate_bad_range"))
		}
		req.Weekdays = days
		req.DayStart = start
		req.DayEnd = end
	}
	if len(parts) >= 5 {
		duration, err := strconv.Atoi(parts[4])
		if err != nil || duration <= 0 {
			return b.sendText(ctx, chatID, tr(actor.Language, "duration_bad"))
		}
		req.DurationMin = duration
	}
	result, err := b.store.GenerateSchedule(ctx, actor.TelegramID, req)
	if err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "generate_failed"))
	}
	return b.sendText(ctx, chatID, tr(actor.Language, "generate_ok", result.Created, result.Skipped))
}

func (b *Bot) beginGenerateMode(ctx context.Context, chatID int64, actor UserRecord) error {
	if !isAdmin(actor.Role) {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_only"))
	}
	return b.beginConversation(ctx, chatID, actor, ConversationState{Step: conversationStepGenMode}, "generate_ask_mode", scheduleChangeKeyboard(actor.Language))
}

func (b *Bot) beginScheduleGenerateFlow(ctx context.Context, chatID int64, actor UserRecord, mode string) error {
	if !isAdmin(actor.Role) {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_only"))
	}
	return b.beginGeneratedMonthConversation(ctx, chatID, actor, mode)
}

func (b *Bot) beginGeneratedMonthConversation(ctx context.Context, chatID int64, actor UserRecord, mode string) error {
	months, err := b.store.ListScheduleMonths(ctx, actor.TelegramID)
	if err != nil {
		b.logger.Printf("list schedule months failed admin=%d mode=%s: %v", actor.TelegramID, mode, err)
		months = nil
	}
	messageKey := "generate_ask_month_pick"
	if len(months) == 0 {
		messageKey = "generate_ask_no_months"
	}
	return b.beginConversation(ctx, chatID, actor, ConversationState{Step: conversationStepGenMonth, GenerateMode: mode}, messageKey, scheduleMonthsKeyboard(actor.Language, months))
}

func (b *Bot) beginDeleteMonthConversation(ctx context.Context, chatID int64, actor UserRecord) error {
	months, err := b.store.ListScheduleMonths(ctx, actor.TelegramID)
	if err != nil {
		b.logger.Printf("list schedule months for delete failed admin=%d: %v", actor.TelegramID, err)
		months = nil
	}
	messageKey := "schedule_delete_ask_month"
	if len(months) == 0 {
		messageKey = "schedule_delete_empty"
	}
	return b.beginConversation(ctx, chatID, actor, ConversationState{Step: conversationStepDeleteMonth}, messageKey, scheduleMonthsKeyboard(actor.Language, months))
}

func (b *Bot) handleScheduleDelete(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if !isAdmin(actor.Role) {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_only"))
	}
	if len(parts) < 2 {
		return b.beginDeleteMonthConversation(ctx, chatID, actor)
	}
	monthStart, err := time.Parse(monthLayout, parts[1])
	if err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "schedule_delete_usage"))
	}
	result, err := b.store.DeleteScheduleMonth(ctx, actor.TelegramID, monthStart)
	if err != nil {
		b.logger.Printf("schedule delete failed admin=%d month=%s: %v", actor.TelegramID, monthStart.Format(monthLayout), err)
		return b.sendText(ctx, chatID, tr(actor.Language, "schedule_delete_failed"))
	}
	return b.sendText(ctx, chatID, tr(actor.Language, "schedule_delete_ok", monthStart.Format(monthLayout), result.Deleted))
}

func (b *Bot) handleAppoint(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if !isAdmin(actor.Role) {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_only"))
	}
	if len(parts) < 2 {
		return b.beginConversation(ctx, chatID, actor, ConversationState{Step: conversationStepAppointKind}, "appoint_ask_contact_type", contactTypeKeyboard(actor.Language))
	}
	if strings.EqualFold(parts[1], "phone") || strings.EqualFold(parts[1], "телефон") {
		if len(parts) == 3 {
			phone := normalizePhone(parts[2])
			if phone == "" {
				return b.sendText(ctx, chatID, tr(actor.Language, "appoint_ask_phone"))
			}
			return b.askAdminAppointmentServices(ctx, chatID, actor, ConversationState{ContactType: "phone", Username: phone})
		}
		if len(parts) < 5 {
			return b.sendText(ctx, chatID, tr(actor.Language, "appoint_phone_usage"))
		}
		phone := strings.TrimSpace(parts[2])
		start, err := parseDateTime(parts[3], parts[4])
		if err != nil {
			return b.sendText(ctx, chatID, tr(actor.Language, "datetime_bad_example"))
		}
		result, err := b.store.AddBookingByPhone(ctx, actor.TelegramID, phone, start)
		if err != nil {
			return b.sendText(ctx, chatID, tr(actor.Language, "appoint_failed"))
		}
		b.notifyBookingChange(ctx, "created", result, chatID)
		return b.sendText(ctx, chatID, tr(actor.Language, "appoint_ok_contact", phone))
	}
	if len(parts) == 2 {
		username := normalizeUsername(parts[1])
		if username == "" {
			return b.sendText(ctx, chatID, tr(actor.Language, "bad_username"))
		}
		return b.askAdminAppointmentServices(ctx, chatID, actor, ConversationState{ContactType: "telegram", Username: username})
	}
	if len(parts) < 4 {
		return b.beginConversation(ctx, chatID, actor, ConversationState{Step: conversationStepAppointKind}, "appoint_ask_contact_type", contactTypeKeyboard(actor.Language))
	}
	username := normalizeUsername(parts[1])
	start, err := parseDateTime(parts[2], parts[3])
	if err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "datetime_bad_example"))
	}
	result, err := b.store.AddBookingByUsername(ctx, actor.TelegramID, username, start)
	if err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "appoint_failed"))
	}
	b.notifyBookingChange(ctx, "created", result, chatID)
	return b.sendText(ctx, chatID, tr(actor.Language, "appoint_ok", username))
}

func (b *Bot) handleCancel(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if !isAdmin(actor.Role) {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_only"))
	}
	if len(parts) < 4 {
		return b.showCancelBookingPicker(ctx, chatID, actor)
	}
	username := normalizeUsername(parts[1])
	start, err := parseDateTime(parts[2], parts[3])
	if err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "datetime_bad"))
	}
	result, err := b.store.DeleteBookingByUsername(ctx, actor.TelegramID, username, start)
	if err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "cancel_failed"))
	}
	b.notifyBookingChange(ctx, "cancelled", result, chatID)
	return b.sendText(ctx, chatID, tr(actor.Language, "cancel_ok", username))
}

func (b *Bot) showCancelBookingPicker(ctx context.Context, chatID int64, actor UserRecord) error {
	items, err := b.store.ListAdminBookingsRange(ctx, actor.TelegramID, time.Now(), time.Time{})
	if err != nil {
		b.logger.Printf("cancel picker bookings failed user=%d role=%s: %v", actor.TelegramID, actor.Role, err)
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_bookings_failed"))
	}
	if len(items) == 0 {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_bookings_empty"))
	}
	return b.sendTextWithKeyboard(ctx, chatID, tr(actor.Language, "cancel_choose_booking"), cancelBookingKeyboard(actor.Language, items))
}

func (b *Bot) handleReschedule(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if !isAdmin(actor.Role) {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_only"))
	}
	if len(parts) < 6 {
		return b.beginConversation(ctx, chatID, actor, ConversationState{Step: conversationStepReschUser}, "reschedule_ask_username", nil)
	}
	username := normalizeUsername(parts[1])
	fromStart, err := parseDateTime(parts[2], parts[3])
	if err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "from_datetime_bad"))
	}
	toStart, err := parseDateTime(parts[4], parts[5])
	if err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "to_datetime_bad"))
	}
	result, err := b.store.RescheduleBookingByUsername(ctx, actor.TelegramID, username, fromStart, toStart)
	if err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "reschedule_failed"))
	}
	b.notifyBookingChange(ctx, "rescheduled", result, chatID)
	return b.sendText(ctx, chatID, tr(actor.Language, "reschedule_ok", username))
}

func (b *Bot) handleBlock(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if !isAdmin(actor.Role) {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_only"))
	}
	if len(parts) < 3 {
		return b.beginConversation(ctx, chatID, actor, ConversationState{Step: conversationStepBlock}, "block_ask_datetime", nil)
	}
	start, err := parseDateTime(parts[1], parts[2])
	if err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "datetime_bad"))
	}
	if err := b.store.BlockSlot(ctx, actor.TelegramID, start); err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "block_failed"))
	}
	return b.sendText(ctx, chatID, tr(actor.Language, "block_ok"))
}

func (b *Bot) handleAdminBookings(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if !isAdmin(actor.Role) {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_only"))
	}
	from, to, day, daily, err := adminBookingsRange(parts)
	if err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_bookings_usage"))
	}
	items, err := b.store.ListAdminBookingsRange(ctx, actor.TelegramID, from, to)
	if err != nil {
		b.logger.Printf("admin bookings failed user=%d role=%s: %v", actor.TelegramID, actor.Role, err)
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_bookings_failed"))
	}
	if len(items) == 0 {
		if daily {
			return b.sendText(ctx, chatID, tr(actor.Language, "admin_bookings_empty_day", day.Format("02.01.2006")))
		}
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_bookings_empty"))
	}
	header := tr(actor.Language, "admin_bookings_header")
	if daily {
		header = tr(actor.Language, "admin_bookings_day_header", day.Format("02.01.2006"))
	}
	return b.sendText(ctx, chatID, formatAdminBookings(actor.Language, header, items, 30, true))
}

func (b *Bot) handleFree(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	var monthStart time.Time
	var err error
	var serviceIndexes []int
	explicitMonth := false
	for _, part := range parts[1:] {
		if month, parseErr := time.Parse(monthLayout, part); parseErr == nil {
			monthStart = month
			explicitMonth = true
			continue
		}
		for _, token := range strings.Split(part, ",") {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			index, parseErr := strconv.Atoi(token)
			if parseErr != nil || index <= 0 {
				return b.sendText(ctx, chatID, tr(actor.Language, "free_usage"))
			}
			serviceIndexes = append(serviceIndexes, index)
		}
	}
	if !monthStart.IsZero() {
		// Parsed above.
	} else if len(parts) >= 2 && len(serviceIndexes) == 0 {
		monthStart, err = time.Parse(monthLayout, parts[1])
		if err != nil {
			return b.sendText(ctx, chatID, tr(actor.Language, "free_usage"))
		}
	} else {
		now := time.Now()
		monthStart = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	}
	if isAdmin(actor.Role) && len(serviceIndexes) == 0 {
		start := time.Now()
		if explicitMonth {
			start = monthStart
		}
		return b.handleWeek(ctx, chatID, actor, []string{"/week", start.Format("2006-01-02")})
	}

	if len(serviceIndexes) > 0 {
		slots, err := b.store.ListFreeSlotsForServices(ctx, actor.TelegramID, serviceIndexes, monthStart)
		if err != nil {
			b.logger.Printf("schedule services failed user=%d role=%s month=%s services=%v: %v", actor.TelegramID, actor.Role, monthStart.Format(monthLayout), serviceIndexes, err)
			if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalidArgument) {
				return b.sendText(ctx, chatID, tr(actor.Language, "free_services_need_list"))
			}
			return b.sendText(ctx, chatID, tr(actor.Language, "free_failed"))
		}
		b.logger.Printf("schedule services user=%d role=%s month=%s services=%v slots=%d", actor.TelegramID, actor.Role, monthStart.Format(monthLayout), serviceIndexes, len(slots))
		if len(slots) == 0 {
			return b.sendText(ctx, chatID, tr(actor.Language, "free_empty"))
		}
		var sb strings.Builder
		sb.WriteString(tr(actor.Language, "free_services_header", slots[0].DurationMin, strings.Join(slots[0].ServiceNames, ", ")))
		sb.WriteString(formatAvailabilitySlots(actor.Language, slots, 30))
		return b.sendText(ctx, chatID, sb.String())
	}

	slots, err := b.store.ListFreeSlotsForMonth(ctx, actor.TelegramID, monthStart)
	if err != nil {
		b.logger.Printf("schedule month failed user=%d role=%s month=%s: %v", actor.TelegramID, actor.Role, monthStart.Format(monthLayout), err)
		return b.sendText(ctx, chatID, tr(actor.Language, "free_failed"))
	}
	b.logger.Printf("schedule month user=%d role=%s month=%s slots=%d", actor.TelegramID, actor.Role, monthStart.Format(monthLayout), len(slots))
	if len(slots) == 0 {
		return b.sendText(ctx, chatID, tr(actor.Language, "free_empty"))
	}

	var sb strings.Builder
	sb.WriteString(tr(actor.Language, "free_header"))
	sb.WriteString(formatTimeSlots(actor.Language, slots, 30))

	return b.sendText(ctx, chatID, sb.String())
}

func (b *Bot) handleCalendar(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	var monthStart time.Time
	if len(parts) >= 2 {
		parsed, err := time.Parse(monthLayout, parts[1])
		if err != nil {
			return b.sendText(ctx, chatID, tr(actor.Language, "calendar_usage"))
		}
		monthStart = parsed
	} else {
		now := time.Now()
		monthStart = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	}
	days, err := b.store.AdminCalendar(ctx, actor.TelegramID, monthStart)
	if err != nil {
		b.logger.Printf("calendar failed user=%d role=%s month=%s: %v", actor.TelegramID, actor.Role, monthStart.Format(monthLayout), err)
		return b.sendText(ctx, chatID, tr(actor.Language, "calendar_failed"))
	}
	b.logger.Printf("calendar user=%d role=%s month=%s days=%d", actor.TelegramID, actor.Role, monthStart.Format(monthLayout), len(days))
	image, err := renderCalendarMonthImage(actor.Language, monthStart, days, !isAdmin(actor.Role))
	if err != nil {
		b.logger.Printf("calendar image failed user=%d month=%s: %v", actor.TelegramID, monthStart.Format(monthLayout), err)
		return b.sendText(ctx, chatID, tr(actor.Language, "calendar_failed"))
	}
	return b.sendPhoto(ctx, chatID, image, tr(actor.Language, "calendar_caption", monthStart.Format(monthLayout)), calendarNavigationKeyboard(actor.Language, monthStart))
}

func (b *Bot) handleWeek(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	weekStart, err := parseWeekStart(parts)
	if err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "week_usage"))
	}
	weekEnd := weekStart.AddDate(0, 0, 7)
	slots, err := b.store.AdminSchedule(ctx, actor.TelegramID, weekStart, weekEnd)
	if err != nil {
		b.logger.Printf("week schedule failed user=%d role=%s from=%s: %v", actor.TelegramID, actor.Role, weekStart.Format("2006-01-02"), err)
		return b.sendText(ctx, chatID, tr(actor.Language, "week_failed"))
	}
	var bookings []BookingView
	if isAdmin(actor.Role) {
		bookings, err = b.store.ListAdminBookingsRange(ctx, actor.TelegramID, weekStart, weekEnd)
		if err != nil {
			b.logger.Printf("week bookings failed user=%d role=%s from=%s: %v", actor.TelegramID, actor.Role, weekStart.Format("2006-01-02"), err)
			return b.sendText(ctx, chatID, tr(actor.Language, "week_failed"))
		}
	}
	b.logger.Printf("week schedule user=%d role=%s from=%s slots=%d bookings=%d", actor.TelegramID, actor.Role, weekStart.Format("2006-01-02"), len(slots), len(bookings))
	renderStart := weekStart
	if len(slots) > 0 {
		loc := slots[0].StartAt.Location()
		renderStart = time.Date(weekStart.Year(), weekStart.Month(), weekStart.Day(), 0, 0, 0, 0, loc)
	}
	private := !isAdmin(actor.Role)
	image, err := renderScheduleWeekImageForAudience(actor.Language, renderStart, slots, bookings, private)
	if err != nil {
		b.logger.Printf("week schedule image failed user=%d from=%s: %v", actor.TelegramID, weekStart.Format("2006-01-02"), err)
		return b.sendText(ctx, chatID, tr(actor.Language, "week_failed"))
	}
	captionKey := "week_caption"
	if private {
		captionKey = "week_caption_client"
	}
	return b.sendPhoto(ctx, chatID, image, tr(actor.Language, captionKey, renderStart.Format("02.01"), renderStart.AddDate(0, 0, 6).Format("02.01")), weekNavigationKeyboard(actor.Language, renderStart))
}

func (b *Bot) handleCalendarCallback(ctx context.Context, cb *telegram.CallbackQuery) error {
	current, err := b.userFromCallback(ctx, cb)
	if err != nil {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(LangRU, "register_failed"))
	}
	month, err := time.Parse(monthLayout, strings.TrimPrefix(cb.Data, "monthcal:"))
	if err != nil {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(current.Language, "calendar_usage"))
	}
	return b.handleCalendar(ctx, cb.Message.Chat.ID, current, []string{"/calendar", month.Format(monthLayout)})
}

func (b *Bot) handleWeekCallback(ctx context.Context, cb *telegram.CallbackQuery) error {
	current, err := b.userFromCallback(ctx, cb)
	if err != nil {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(LangRU, "register_failed"))
	}
	raw := strings.TrimPrefix(cb.Data, "week:")
	day, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(current.Language, "week_usage"))
	}
	return b.handleWeek(ctx, cb.Message.Chat.ID, current, []string{"/week", day.Format("2006-01-02")})
}

func (b *Bot) handleRequestMonth(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if len(parts) < 2 {
		return b.sendText(ctx, chatID, tr(actor.Language, "month_request_usage"))
	}
	monthStart, err := time.Parse(monthLayout, parts[1])
	if err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "month_request_usage"))
	}
	requested, err := b.store.RequestMissingMonth(ctx, actor.TelegramID, monthStart)
	if err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "month_request_failed"))
	}
	month := monthStart.Format(monthLayout)
	if !requested {
		return b.sendText(ctx, chatID, tr(actor.Language, "month_request_exists", month, month))
	}
	return b.sendText(ctx, chatID, tr(actor.Language, "month_request_ok", month))
}

func (b *Bot) handleMy(ctx context.Context, chatID int64, actor UserRecord) error {
	items, err := b.store.ListMyBookings(ctx, actor.TelegramID, time.Now())
	if err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "my_failed"))
	}
	if len(items) == 0 {
		return b.sendText(ctx, chatID, tr(actor.Language, "my_empty"))
	}
	var sb strings.Builder
	sb.WriteString(tr(actor.Language, "my_header"))
	limit := len(items)
	if limit > 20 {
		limit = 20
	}
	for i := 0; i < limit; i++ {
		sb.WriteString(strconv.Itoa(i + 1))
		sb.WriteString(". ")
		sb.WriteString(items[i].StartAt.Format(dateTimeLayout))
		if items[i].AdminName != "" {
			sb.WriteString(" - ")
			sb.WriteString(items[i].AdminName)
		}
		sb.WriteString("\n")
	}
	return b.sendTextWithKeyboard(ctx, chatID, sb.String(), myActionsKeyboard(actor.Language))
}

func (b *Bot) showMyCancelBookingPicker(ctx context.Context, chatID int64, actor UserRecord) error {
	items, err := b.store.ListMyBookings(ctx, actor.TelegramID, time.Now())
	if err != nil {
		b.logger.Printf("my cancel picker failed user=%d: %v", actor.TelegramID, err)
		return b.sendText(ctx, chatID, tr(actor.Language, "my_failed"))
	}
	if len(items) == 0 {
		return b.sendText(ctx, chatID, tr(actor.Language, "my_empty"))
	}
	return b.sendTextWithKeyboard(ctx, chatID, tr(actor.Language, "my_cancel_choose"), myBookingActionKeyboard(actor.Language, items, "mycancel"))
}

func (b *Bot) showMyMoveBookingPicker(ctx context.Context, chatID int64, actor UserRecord) error {
	items, err := b.store.ListMyBookings(ctx, actor.TelegramID, time.Now())
	if err != nil {
		b.logger.Printf("my move picker failed user=%d: %v", actor.TelegramID, err)
		return b.sendText(ctx, chatID, tr(actor.Language, "my_failed"))
	}
	if len(items) == 0 {
		return b.sendText(ctx, chatID, tr(actor.Language, "my_empty"))
	}
	return b.sendTextWithKeyboard(ctx, chatID, tr(actor.Language, "my_move_choose"), myBookingActionKeyboard(actor.Language, items, "mymove"))
}

func (b *Bot) showMyMoveSlots(ctx context.Context, chatID int64, actor UserRecord, bookingID int64) error {
	now := time.Now()
	slots, err := b.store.ListMoveTargetsForBooking(ctx, actor.TelegramID, bookingID, now, now.AddDate(0, 0, 14))
	if err != nil {
		b.logger.Printf("my move slots failed user=%d booking=%d: %v", actor.TelegramID, bookingID, err)
		return b.sendText(ctx, chatID, tr(actor.Language, "my_move_failed"))
	}
	if len(slots) == 0 {
		return b.sendText(ctx, chatID, tr(actor.Language, "my_move_empty"))
	}
	return b.sendTextWithKeyboard(ctx, chatID, tr(actor.Language, "my_move_slot_choose"), moveSlotKeyboard(actor.Language, bookingID, slots))
}

func (b *Bot) handleBook(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if len(parts) < 2 {
		return b.sendText(ctx, chatID, tr(actor.Language, "book_usage"))
	}
	if len(parts) == 2 {
		index, err := strconv.Atoi(parts[1])
		if err != nil || index <= 0 {
			return b.sendText(ctx, chatID, tr(actor.Language, "book_usage"))
		}
		result, err := b.store.BookForUserByIndex(ctx, actor.TelegramID, index)
		if err != nil {
			b.logger.Printf("book by index failed user=%d index=%d: %v", actor.TelegramID, index, err)
			if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalidArgument) {
				return b.sendText(ctx, chatID, tr(actor.Language, "book_need_schedule"))
			}
			return b.sendText(ctx, chatID, tr(actor.Language, "book_failed"))
		}
		b.logger.Printf("book by index ok user=%d index=%d start=%s", actor.TelegramID, index, result.StartAt.Format(time.RFC3339))
		b.notifyBookingChange(ctx, "created", result, chatID)
		return b.sendText(ctx, chatID, tr(actor.Language, "book_ok", result.StartAt.Format(dateTimeLayout)))
	}
	if len(parts) != 3 {
		return b.sendText(ctx, chatID, tr(actor.Language, "book_usage"))
	}
	start, err := parseDateTime(parts[1], parts[2])
	if err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "datetime_bad"))
	}
	result, err := b.store.BookForUser(ctx, actor.TelegramID, start)
	if err != nil {
		b.logger.Printf("book by datetime failed user=%d start=%s: %v", actor.TelegramID, start.Format(time.RFC3339), err)
		return b.sendText(ctx, chatID, tr(actor.Language, "book_failed"))
	}
	b.logger.Printf("book by datetime ok user=%d start=%s", actor.TelegramID, start.Format(time.RFC3339))
	b.notifyBookingChange(ctx, "created", result, chatID)
	return b.sendText(ctx, chatID, tr(actor.Language, "book_ok", start.Format(dateTimeLayout)))
}

func (b *Bot) handleMove(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if len(parts) < 3 {
		return b.sendText(ctx, chatID, tr(actor.Language, "move_usage"))
	}
	if len(parts) == 3 {
		bookingIndex, err1 := strconv.Atoi(parts[1])
		slotIndex, err2 := strconv.Atoi(parts[2])
		if err1 != nil || err2 != nil || bookingIndex <= 0 || slotIndex <= 0 {
			return b.sendText(ctx, chatID, tr(actor.Language, "move_usage"))
		}
		result, err := b.store.MoveBookingForUserByIndex(ctx, actor.TelegramID, bookingIndex, slotIndex)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalidArgument) {
				return b.sendText(ctx, chatID, tr(actor.Language, "move_need_lists"))
			}
			return b.sendText(ctx, chatID, tr(actor.Language, "move_failed"))
		}
		b.notifyMove(ctx, result)
		return b.sendText(ctx, chatID, tr(actor.Language, "move_ok", result.FromStart.Format(dateTimeLayout), result.ToStart.Format(dateTimeLayout)))
	}
	if len(parts) < 5 {
		return b.sendText(ctx, chatID, tr(actor.Language, "move_usage"))
	}
	fromStart, err := parseDateTime(parts[1], parts[2])
	if err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "from_datetime_bad"))
	}
	toStart, err := parseDateTime(parts[3], parts[4])
	if err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "to_datetime_bad"))
	}
	result, err := b.store.MoveBookingForUser(ctx, actor.TelegramID, fromStart, toStart)
	if err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "move_failed"))
	}
	b.notifyMove(ctx, result)
	return b.sendText(ctx, chatID, tr(actor.Language, "move_ok", fromStart.Format(dateTimeLayout), toStart.Format(dateTimeLayout)))
}

func (b *Bot) notifyMove(ctx context.Context, result MoveResult) {
	if result.AdminChatID > 0 {
		text := tr(result.AdminLanguage, "move_admin_notice", result.Username, result.FromStart.Format(dateTimeLayout), result.ToStart.Format(dateTimeLayout))
		if err := b.sendText(ctx, result.AdminChatID, text); err != nil {
			b.logger.Printf("notify admin about move failed: %v", err)
		}
	}
}

func (b *Bot) notifyBookingChange(ctx context.Context, action string, result BookingChangeResult, skipChatID int64) {
	if result.AdminChatID <= 0 || result.AdminChatID == skipChatID {
		return
	}
	client := formatClientContact(result.Username)
	if client == "" {
		client = tr(result.AdminLanguage, "unknown_user")
	}
	services := ""
	if len(result.ServiceNames) > 0 {
		services = " - " + strings.Join(result.ServiceNames, ", ")
	}
	var text string
	switch action {
	case "created":
		text = tr(result.AdminLanguage, "admin_booking_created", result.StartAt.Format(dateTimeLayout), client, services)
	case "cancelled":
		text = tr(result.AdminLanguage, "admin_booking_cancelled", result.StartAt.Format(dateTimeLayout), client, services)
	case "rescheduled":
		to := result.NewStartAt
		if to.IsZero() {
			to = result.StartAt
		}
		text = tr(result.AdminLanguage, "admin_booking_rescheduled", client, result.StartAt.Format(dateTimeLayout), to.Format(dateTimeLayout), services)
	default:
		return
	}
	if err := b.sendText(ctx, result.AdminChatID, text); err != nil {
		b.logger.Printf("notify admin about booking change failed action=%s: %v", action, err)
	}
}

func (b *Bot) sendText(ctx context.Context, chatID int64, text string) error {
	if err := b.tg.SendMessage(ctx, telegram.SendMessageRequest{
		ChatID: chatID,
		Text:   text,
	}); err != nil {
		b.logger.Printf("send message failed chat=%d: %v", chatID, err)
		return err
	}
	return nil
}

func (b *Bot) sendTextWithKeyboard(ctx context.Context, chatID int64, text string, kb *telegram.ReplyMarkup) error {
	if err := b.tg.SendMessage(ctx, telegram.SendMessageRequest{
		ChatID:      chatID,
		Text:        text,
		ReplyMarkup: kb,
	}); err != nil {
		b.logger.Printf("send message with keyboard failed chat=%d: %v", chatID, err)
		return err
	}
	return nil
}

func (b *Bot) sendPhoto(ctx context.Context, chatID int64, image []byte, caption string, kb *telegram.ReplyMarkup) error {
	if err := b.tg.SendPhoto(ctx, telegram.SendPhotoRequest{
		ChatID:      chatID,
		PhotoName:   "schedule-week.png",
		Photo:       image,
		Caption:     caption,
		ReplyMarkup: kb,
	}); err != nil {
		b.logger.Printf("send photo failed chat=%d: %v", chatID, err)
		return err
	}
	return nil
}

func normalizeUsername(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "@")
	return strings.ToLower(value)
}

func formatClientContact(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	first := value[0]
	if first == '+' || first >= '0' && first <= '9' {
		return value
	}
	for _, char := range value {
		if unicode.IsSpace(char) || char > unicode.MaxASCII {
			return value
		}
	}
	return "@" + value
}

func adminBookingsRange(parts []string) (time.Time, time.Time, time.Time, bool, error) {
	if len(parts) < 2 {
		return time.Now(), time.Time{}, time.Time{}, false, nil
	}
	raw := strings.ToLower(strings.TrimSpace(parts[1]))
	now := time.Now()
	var day time.Time
	switch raw {
	case "today", "сегодня":
		day = dateOnly(now)
	case "tomorrow", "завтра":
		day = dateOnly(now).AddDate(0, 0, 1)
	default:
		parsed, err := parseSingleDate(raw)
		if err != nil {
			return time.Time{}, time.Time{}, time.Time{}, false, err
		}
		day = dateOnly(parsed)
	}
	return day, day.AddDate(0, 0, 1), day, true, nil
}

func parseWeekStart(parts []string) (time.Time, error) {
	now := dateOnly(time.Now())
	if len(parts) < 2 {
		return weekStart(now), nil
	}
	value := strings.ToLower(strings.TrimSpace(parts[1]))
	switch value {
	case "today", "сегодня":
		return weekStart(now), nil
	case "tomorrow", "завтра":
		return weekStart(now.AddDate(0, 0, 1)), nil
	case "next", "следующая", "след":
		return weekStart(now.AddDate(0, 0, 7)), nil
	case "prev", "previous", "прошлая", "пред":
		return weekStart(now.AddDate(0, 0, -7)), nil
	}
	if parsed, err := parseUserDate(value, now); err == nil {
		return weekStart(parsed), nil
	}
	return time.Time{}, errors.New("bad week date")
}

func weekStart(day time.Time) time.Time {
	day = dateOnly(day)
	weekday := int(day.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	return day.AddDate(0, 0, 1-weekday)
}

func formatAdminBookings(lang, header string, items []BookingView, limit int, includeDate bool) string {
	var sb strings.Builder
	sb.WriteString(header)
	limit = minInt(limit, len(items))
	for i := 0; i < limit; i++ {
		item := items[i]
		sb.WriteString(strconv.Itoa(i + 1))
		sb.WriteString(". ")
		if includeDate {
			sb.WriteString(item.StartAt.Format(dateTimeLayout))
		} else {
			sb.WriteString(item.StartAt.Format("15:04"))
			if !item.EndAt.IsZero() {
				sb.WriteString("-")
				sb.WriteString(item.EndAt.Format("15:04"))
			}
		}
		sb.WriteString(" - ")
		if item.Username != "" {
			sb.WriteString(formatClientContact(item.Username))
		} else {
			sb.WriteString(tr(lang, "unknown_user"))
		}
		if item.AdminName != "" {
			sb.WriteString(" (@")
			sb.WriteString(item.AdminName)
			sb.WriteString(")")
		}
		if len(item.ServiceNames) > 0 {
			sb.WriteString(" - ")
			sb.WriteString(strings.Join(item.ServiceNames, ", "))
		}
		sb.WriteString("\n")
	}
	if len(items) > limit {
		sb.WriteString(tr(lang, "admin_bookings_more", len(items)-limit))
		sb.WriteString("\n")
	}
	return sb.String()
}

func parseDateTime(datePart, timePart string) (time.Time, error) {
	return time.ParseInLocation(dateTimeLayout, datePart+" "+timePart, time.Local)
}

func isAdmin(role Role) bool {
	return role == RoleAdmin || role == RoleSuperAdmin
}
