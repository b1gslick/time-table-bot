package bot

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"time-table-bot/internal/store"
	"time-table-bot/internal/telegram"
)

const (
	dateTimeLayout = "2006-01-02 15:04"
	monthLayout    = "2006-01"
)

const (
	conversationStepLanguage    = "language"
	conversationStepCategory    = "category"
	conversationStepSubcategory = "subcategory"
	conversationStepService     = "service"
	conversationStepMore        = "more"
	conversationStepTimeChoice  = "time_choice"
	conversationStepDates       = "dates"
	conversationStepSlot        = "slot"
)

func (b *Bot) HandleMessage(ctx context.Context, msg *telegram.Message) error {
	user := UserRecord{
		TelegramID: msg.From.ID,
		Username:   normalizeUsername(msg.From.Username),
		FirstName:  msg.From.FirstName,
		LastName:   msg.From.LastName,
		Role:       RoleUser,
		TravelMin:  30,
		Language:   LangRU,
	}
	if user.Username == b.superAdminUsername {
		user.Role = RoleSuperAdmin
	}

	current, err := b.store.RegisterOrUpdateUser(ctx, user)
	if err != nil {
		return b.sendText(ctx, msg.Chat.ID, tr(LangRU, "register_failed"))
	}

	text := strings.TrimSpace(msg.Text)
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return nil
	}

	cmd := strings.ToLower(parts[0])
	b.logger.Printf("message received chat=%d user=%d username=%q role=%s cmd=%q", msg.Chat.ID, current.TelegramID, current.Username, current.Role, cmd)

	switch cmd {
	case "/start":
		return b.handleStart(ctx, msg.Chat.ID, current)
	case "/booking", "/start_booking", "/book_start":
		return b.handleBookingStart(ctx, msg.Chat.ID, current)
	case "/help":
		return b.sendHelp(ctx, msg.Chat.ID, current)
	case "/lang", "/language":
		return b.handleLanguage(ctx, msg.Chat.ID, current, parts)
	case "/role":
		return b.handleRole(ctx, msg.Chat.ID, current, parts)
	case "/admin_add":
		return b.handleAdminAdd(ctx, msg.Chat.ID, current, parts)
	case "/admin_remove":
		return b.handleAdminRemove(ctx, msg.Chat.ID, current, parts)
	case "/setprofile":
		return b.handleSetProfile(ctx, msg.Chat.ID, current, text)
	case "/setservices":
		return b.handleSetServices(ctx, msg.Chat.ID, current, text)
	case "/service_add", "/addservice":
		return b.handleServiceAdd(ctx, msg.Chat.ID, current, parts)
	case "/services":
		return b.handleServices(ctx, msg.Chat.ID, current)
	case "/sethours":
		return b.handleSetHours(ctx, msg.Chat.ID, current, text)
	case "/setduration":
		return b.handleSetDuration(ctx, msg.Chat.ID, current, parts)
	case "/generate", "/gen":
		return b.handleGenerate(ctx, msg.Chat.ID, current, parts)
	case "/appoint":
		return b.handleAppoint(ctx, msg.Chat.ID, current, parts)
	case "/cancel":
		return b.handleCancel(ctx, msg.Chat.ID, current, parts)
	case "/reschedule":
		return b.handleReschedule(ctx, msg.Chat.ID, current, parts)
	case "/block":
		return b.handleBlock(ctx, msg.Chat.ID, current, parts)
	case "/free", "/schedule":
		return b.handleFree(ctx, msg.Chat.ID, current, parts)
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
	case "/settravel":
		return b.handleSetTravel(ctx, msg.Chat.ID, current, parts)
	default:
		if !strings.HasPrefix(cmd, "/") && current.Role == RoleUser {
			if handled, err := b.handleConversation(ctx, msg.Chat.ID, current, text); handled {
				return err
			}
		}
		return b.sendText(ctx, msg.Chat.ID, tr(current.Language, "unknown_command"))
	}
}

func (b *Bot) handleStart(ctx context.Context, chatID int64, user UserRecord) error {
	if user.Role == RoleUser {
		if err := b.store.SetConversationState(ctx, user.TelegramID, ConversationState{Step: conversationStepLanguage}); err != nil {
			return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
		}
		return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "choose_language"), languageKeyboard())
	}
	txt := tr(user.Language, "start", user.Username, roleLabel(user.Language, user.Role), user.Language)
	return b.sendTextWithKeyboard(ctx, chatID, txt, keyboardForRole(user.Role))
}

func (b *Bot) handleBookingStart(ctx context.Context, chatID int64, user UserRecord) error {
	if err := b.store.ClearConversationState(ctx, user.TelegramID); err != nil {
		b.logger.Printf("booking start: clear state failed user=%d: %v", user.TelegramID, err)
	}
	return b.askCategory(ctx, chatID, user, nil)
}

func (b *Bot) sendHelp(ctx context.Context, chatID int64, user UserRecord) error {
	text := tr(user.Language, "help_base")
	if user.Role == RoleAdmin || user.Role == RoleSuperAdmin {
		text += "\nAdmin:\n" + tr(user.Language, "help_admin")
	}
	if user.Role == RoleSuperAdmin {
		text += "\nSuper admin:\n" + tr(user.Language, "help_super")
	}
	return b.sendText(ctx, chatID, text)
}

func (b *Bot) handleAdminAdd(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if actor.Role != RoleSuperAdmin {
		return b.sendText(ctx, chatID, tr(actor.Language, "super_only_add"))
	}
	if len(parts) < 2 {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_add_usage"))
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
	if actor.Role != RoleSuperAdmin {
		return b.sendText(ctx, chatID, tr(actor.Language, "super_only_remove"))
	}
	if len(parts) < 2 {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_remove_usage"))
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
	if actor.Role != RoleSuperAdmin {
		return b.sendText(ctx, chatID, tr(actor.Language, "super_only_role"))
	}
	if len(parts) < 2 {
		return b.sendText(ctx, chatID, tr(actor.Language, "role_usage"))
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
		return b.sendText(ctx, chatID, tr(actor.Language, "profile_usage"))
	}
	if err := b.store.SetProfileText(ctx, actor.TelegramID, value); err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "profile_failed"))
	}
	return b.sendText(ctx, chatID, tr(actor.Language, "profile_ok"))
}

func (b *Bot) handleSetServices(ctx context.Context, chatID int64, actor UserRecord, text string) error {
	if !isAdmin(actor.Role) {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_only"))
	}
	value := strings.TrimSpace(strings.TrimPrefix(text, "/setservices"))
	if value == "" {
		return b.sendText(ctx, chatID, tr(actor.Language, "services_usage"))
	}
	if err := b.store.SetServicesText(ctx, actor.TelegramID, value); err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "services_failed"))
	}
	return b.sendText(ctx, chatID, tr(actor.Language, "services_ok"))
}

func (b *Bot) handleServiceAdd(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if !isAdmin(actor.Role) {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_only"))
	}
	if len(parts) < 3 {
		return b.sendText(ctx, chatID, tr(actor.Language, "service_add_usage"))
	}
	duration, err := strconv.Atoi(parts[1])
	if err != nil || duration <= 0 {
		return b.sendText(ctx, chatID, tr(actor.Language, "duration_bad"))
	}
	name := strings.TrimSpace(strings.Join(parts[2:], " "))
	if name == "" {
		return b.sendText(ctx, chatID, tr(actor.Language, "service_add_usage"))
	}
	if err := b.store.AddService(ctx, actor.TelegramID, name, duration, ""); err != nil {
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
	sb.WriteString(tr(actor.Language, "services_header"))
	for i, service := range services {
		sb.WriteString(strconv.Itoa(i + 1))
		sb.WriteString(". ")
		sb.WriteString(serviceDisplayName(service))
		sb.WriteString(" - ")
		sb.WriteString(strconv.Itoa(service.DurationMin))
		sb.WriteString(" ")
		sb.WriteString(tr(actor.Language, "minutes_short"))
		if service.AdminName != "" {
			sb.WriteString(" (@")
			sb.WriteString(service.AdminName)
			sb.WriteString(")")
		}
		sb.WriteString("\n")
	}
	sb.WriteString(tr(actor.Language, "services_footer"))
	return b.sendText(ctx, chatID, sb.String())
}

func (b *Bot) handleSetHours(ctx context.Context, chatID int64, actor UserRecord, text string) error {
	if !isAdmin(actor.Role) {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_only"))
	}
	value := strings.TrimSpace(strings.TrimPrefix(text, "/sethours"))
	if value == "" {
		return b.sendText(ctx, chatID, tr(actor.Language, "hours_usage"))
	}
	if err := b.store.SetWorkHoursText(ctx, actor.TelegramID, value); err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "hours_failed"))
	}
	return b.sendText(ctx, chatID, tr(actor.Language, "hours_ok"))
}

func (b *Bot) handleSetDuration(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if !isAdmin(actor.Role) {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_only"))
	}
	if len(parts) < 2 {
		return b.sendText(ctx, chatID, tr(actor.Language, "duration_usage"))
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
		return b.sendText(ctx, chatID, tr(actor.Language, "generate_usage"))
	}
	monthStart, err := time.Parse(monthLayout, parts[1])
	if err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "generate_usage"))
	}
	req := GenerateScheduleRequest{Month: monthStart, Months: 1}
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

func (b *Bot) handleAppoint(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if !isAdmin(actor.Role) {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_only"))
	}
	if len(parts) < 4 {
		return b.sendText(ctx, chatID, tr(actor.Language, "appoint_usage"))
	}
	username := normalizeUsername(parts[1])
	start, err := parseDateTime(parts[2], parts[3])
	if err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "datetime_bad_example"))
	}
	travel := 30
	if len(parts) >= 5 {
		travel, err = strconv.Atoi(parts[4])
		if err != nil || travel < 0 {
			return b.sendText(ctx, chatID, tr(actor.Language, "travel_bad"))
		}
	}
	if err := b.store.AddBookingByUsername(ctx, actor.TelegramID, username, start, travel); err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "appoint_failed"))
	}
	return b.sendText(ctx, chatID, tr(actor.Language, "appoint_ok", username))
}

func (b *Bot) handleCancel(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if !isAdmin(actor.Role) {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_only"))
	}
	if len(parts) < 4 {
		return b.sendText(ctx, chatID, tr(actor.Language, "cancel_usage"))
	}
	username := normalizeUsername(parts[1])
	start, err := parseDateTime(parts[2], parts[3])
	if err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "datetime_bad"))
	}
	if err := b.store.DeleteBookingByUsername(ctx, actor.TelegramID, username, start); err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "cancel_failed"))
	}
	return b.sendText(ctx, chatID, tr(actor.Language, "cancel_ok", username))
}

func (b *Bot) handleReschedule(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if !isAdmin(actor.Role) {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_only"))
	}
	if len(parts) < 6 {
		return b.sendText(ctx, chatID, tr(actor.Language, "reschedule_usage"))
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
	if err := b.store.RescheduleBookingByUsername(ctx, actor.TelegramID, username, fromStart, toStart); err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "reschedule_failed"))
	}
	return b.sendText(ctx, chatID, tr(actor.Language, "reschedule_ok", username))
}

func (b *Bot) handleBlock(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if !isAdmin(actor.Role) {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_only"))
	}
	if len(parts) < 3 {
		return b.sendText(ctx, chatID, tr(actor.Language, "block_usage"))
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

func (b *Bot) handleFree(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	var monthStart time.Time
	var err error
	var serviceIndexes []int
	for _, part := range parts[1:] {
		if month, parseErr := time.Parse(monthLayout, part); parseErr == nil {
			monthStart = month
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
		limit := len(slots)
		if limit > 40 {
			limit = 40
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
			sb.WriteString(tr(actor.Language, "free_more", len(slots)-limit))
		}
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
	limit := len(slots)
	if limit > 40 {
		limit = 40
	}
	for i := 0; i < limit; i++ {
		sb.WriteString(strconv.Itoa(i + 1))
		sb.WriteString(". ")
		sb.WriteString(slots[i].Format(dateTimeLayout))
		sb.WriteString("\n")
	}
	if len(slots) > limit {
		sb.WriteString(tr(actor.Language, "free_more", len(slots)-limit))
	}

	return b.sendText(ctx, chatID, sb.String())
}

func (b *Bot) handleCalendar(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if !isAdmin(actor.Role) {
		return b.sendText(ctx, chatID, tr(actor.Language, "admin_only"))
	}
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
	return b.sendText(ctx, chatID, formatCalendar(actor.Language, monthStart, days))
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
	return b.sendText(ctx, chatID, sb.String())
}

func (b *Bot) handleBook(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if len(parts) < 2 {
		return b.sendText(ctx, chatID, tr(actor.Language, "book_usage"))
	}
	travel := actor.TravelMin
	if travel <= 0 {
		travel = 30
	}
	if len(parts) == 2 {
		index, err := strconv.Atoi(parts[1])
		if err != nil || index <= 0 {
			return b.sendText(ctx, chatID, tr(actor.Language, "book_usage"))
		}
		start, err := b.store.BookForUserByIndex(ctx, actor.TelegramID, index, travel)
		if err != nil {
			b.logger.Printf("book by index failed user=%d index=%d travel=%d: %v", actor.TelegramID, index, travel, err)
			if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalidArgument) {
				return b.sendText(ctx, chatID, tr(actor.Language, "book_need_schedule"))
			}
			return b.sendText(ctx, chatID, tr(actor.Language, "book_failed"))
		}
		b.logger.Printf("book by index ok user=%d index=%d start=%s travel=%d", actor.TelegramID, index, start.Format(time.RFC3339), travel)
		return b.sendText(ctx, chatID, tr(actor.Language, "book_ok", start.Format(dateTimeLayout), travel))
	}
	start, err := parseDateTime(parts[1], parts[2])
	if err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "datetime_bad"))
	}
	if len(parts) >= 4 {
		travel, err = strconv.Atoi(parts[3])
		if err != nil || travel < 0 {
			return b.sendText(ctx, chatID, tr(actor.Language, "travel_bad"))
		}
	}
	if err := b.store.BookForUser(ctx, actor.TelegramID, start, travel); err != nil {
		b.logger.Printf("book by datetime failed user=%d start=%s travel=%d: %v", actor.TelegramID, start.Format(time.RFC3339), travel, err)
		return b.sendText(ctx, chatID, tr(actor.Language, "book_failed"))
	}
	b.logger.Printf("book by datetime ok user=%d start=%s travel=%d", actor.TelegramID, start.Format(time.RFC3339), travel)
	return b.sendText(ctx, chatID, tr(actor.Language, "book_ok", start.Format(dateTimeLayout), travel))
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

func (b *Bot) handleSetTravel(ctx context.Context, chatID int64, actor UserRecord, parts []string) error {
	if len(parts) < 2 {
		return b.sendText(ctx, chatID, tr(actor.Language, "settravel_usage"))
	}
	minutes, err := strconv.Atoi(parts[1])
	if err != nil || minutes < 0 {
		return b.sendText(ctx, chatID, tr(actor.Language, "settravel_bad"))
	}
	if err := b.store.SetUserTravelDefault(ctx, actor.TelegramID, minutes); err != nil {
		return b.sendText(ctx, chatID, tr(actor.Language, "settravel_failed"))
	}
	return b.sendText(ctx, chatID, tr(actor.Language, "settravel_ok", minutes))
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

func normalizeUsername(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "@")
	return strings.ToLower(value)
}

func parseDateTime(datePart, timePart string) (time.Time, error) {
	return time.ParseInLocation(dateTimeLayout, datePart+" "+timePart, time.Local)
}

func isAdmin(role Role) bool {
	return role == RoleAdmin || role == RoleSuperAdmin
}
