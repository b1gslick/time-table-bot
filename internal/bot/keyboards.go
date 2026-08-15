package bot

import (
	"fmt"
	"strings"
	"time"

	"time-table-bot/internal/telegram"
)

func keyboardForRole(role Role, lang string) *telegram.ReplyMarkup {
	switch role {
	case RoleSuperAdmin:
		return menuKeyboard([][]string{
			{tr(lang, "button_menu_calendar"), tr(lang, "button_menu_bookings")},
			{tr(lang, "button_menu_services"), tr(lang, "button_menu_schedule")},
			{tr(lang, "button_menu_finance"), tr(lang, "button_menu_settings")},
			{tr(lang, "button_menu_admins")},
		})
	case RoleAdmin:
		return menuKeyboard([][]string{
			{tr(lang, "button_menu_calendar"), tr(lang, "button_menu_bookings")},
			{tr(lang, "button_menu_services"), tr(lang, "button_menu_schedule")},
			{tr(lang, "button_menu_finance"), tr(lang, "button_menu_settings")},
		})
	default:
		return menuKeyboard([][]string{
			{tr(lang, "button_start_booking")},
			{tr(lang, "button_action_my")},
		})
	}
}

func financeMenuKeyboard(lang string) *telegram.ReplyMarkup {
	return menuKeyboard([][]string{
		{tr(lang, "button_finance_month"), tr(lang, "button_finance_quarter"), tr(lang, "button_finance_year")},
		{tr(lang, "button_finance_add_income"), tr(lang, "button_finance_add_expense")},
		{tr(lang, "button_finance_chart")},
		{tr(lang, "button_back")},
	})
}

func financeInputKeyboard(lang string) *telegram.ReplyMarkup {
	return menuKeyboard([][]string{{tr(lang, "button_back")}})
}

func financeChartPeriodKeyboard(lang string) *telegram.ReplyMarkup {
	return &telegram.ReplyMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{
			{Text: tr(lang, "button_finance_month"), CallbackData: "finance:chart:month"},
			{Text: tr(lang, "button_finance_quarter"), CallbackData: "finance:chart:quarter"},
			{Text: tr(lang, "button_finance_year"), CallbackData: "finance:chart:year"},
		},
	}}
}

func financeEntryKeyboard(lang string, canConfirm bool) *telegram.ReplyMarkup {
	rows := make([][]telegram.InlineKeyboardButton, 0, 2)
	if canConfirm {
		rows = append(rows, []telegram.InlineKeyboardButton{{Text: tr(lang, "finance_confirm"), CallbackData: "financeentry:yes"}})
	}
	rows = append(rows, []telegram.InlineKeyboardButton{{Text: tr(lang, "no"), CallbackData: "financeentry:no"}})
	return &telegram.ReplyMarkup{InlineKeyboard: rows}
}

func financeReportKeyboard(lang string, unresolved []FinanceUnresolved, period string) *telegram.ReplyMarkup {
	rows := make([][]telegram.InlineKeyboardButton, 0, 12)
	limit := len(unresolved)
	if limit > 8 {
		limit = 8
	}
	for i := 0; i < limit; i++ {
		item := unresolved[i]
		label := fmt.Sprintf("%s %s", item.StartAt.Format("02.01"), item.Client)
		if len([]rune(label)) > 32 {
			label = string([]rune(label)[:32])
		}
		rows = append(rows, []telegram.InlineKeyboardButton{{
			Text: tr(lang, "finance_resolve_button", label), CallbackData: fmt.Sprintf("finance:resolve:%d:%s", item.BookingID, period),
		}})
	}
	rows = append(rows,
		[]telegram.InlineKeyboardButton{
			{Text: tr(lang, "button_finance_add_income"), CallbackData: "finance:addincome:" + period},
			{Text: tr(lang, "button_finance_add_expense"), CallbackData: "finance:addexpense:" + period},
		},
		[]telegram.InlineKeyboardButton{{Text: tr(lang, "button_finance_chart"), CallbackData: "finance:chart:" + period}},
	)
	return &telegram.ReplyMarkup{InlineKeyboard: rows}
}

func keyboardForUser(user UserRecord) *telegram.ReplyMarkup {
	if user.ActualRole != RoleSuperAdmin || user.Role == RoleSuperAdmin {
		return keyboardForRole(user.Role, user.Language)
	}
	base := keyboardForRole(user.Role, user.Language)
	if base == nil {
		return base
	}
	base.Keyboard = append(base.Keyboard, []telegram.KeyboardButton{{Text: tr(user.Language, "button_action_view_super")}})
	return base
}

func menuKeyboard(rows [][]string) *telegram.ReplyMarkup {
	keyboard := make([][]telegram.KeyboardButton, 0, len(rows))
	for _, row := range rows {
		buttons := make([]telegram.KeyboardButton, 0, len(row))
		for _, text := range row {
			buttons = append(buttons, telegram.KeyboardButton{Text: text})
		}
		keyboard = append(keyboard, buttons)
	}
	return &telegram.ReplyMarkup{ResizeKeyboard: true, Keyboard: keyboard}
}

func bookingConfirmationKeyboard(lang string) *telegram.ReplyMarkup {
	return &telegram.ReplyMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{
			{Text: tr(lang, "yes"), CallbackData: "bookconfirm:yes"},
			{Text: tr(lang, "no"), CallbackData: "bookconfirm:no"},
		},
		{
			{Text: tr(lang, "booking_edit"), CallbackData: "bookconfirm:edit"},
			{Text: tr(lang, "booking_find_other"), CallbackData: "bookconfirm:other"},
		},
	}}
}

func bookingEditKeyboard(lang string, admin bool) *telegram.ReplyMarkup {
	row := []telegram.InlineKeyboardButton{
		{Text: tr(lang, "booking_edit_service"), CallbackData: "bookedit:service"},
		{Text: tr(lang, "booking_edit_time"), CallbackData: "bookedit:time"},
	}
	rows := [][]telegram.InlineKeyboardButton{row}
	if admin {
		rows = append(rows, []telegram.InlineKeyboardButton{{Text: tr(lang, "booking_edit_client"), CallbackData: "bookedit:client"}})
	}
	return &telegram.ReplyMarkup{InlineKeyboard: rows}
}

func calendarMenuKeyboard(lang string) *telegram.ReplyMarkup {
	return menuKeyboard([][]string{
		{tr(lang, "button_action_week"), tr(lang, "button_action_calendar")},
		{tr(lang, "button_action_free")},
		{tr(lang, "button_back")},
	})
}

func weekNavigationKeyboard(lang string, start time.Time) *telegram.ReplyMarkup {
	return &telegram.ReplyMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{
			{Text: tr(lang, "week_prev"), CallbackData: "week:" + start.AddDate(0, 0, -7).Format("2006-01-02")},
			{Text: tr(lang, "week_today"), CallbackData: "week:" + weekStart(time.Now()).Format("2006-01-02")},
			{Text: tr(lang, "week_next"), CallbackData: "week:" + start.AddDate(0, 0, 7).Format("2006-01-02")},
		},
	}}
}

func bookingMenuKeyboard(lang string) *telegram.ReplyMarkup {
	return menuKeyboard([][]string{
		{tr(lang, "button_action_bookings_today"), tr(lang, "button_action_bookings_tomorrow")},
		{tr(lang, "button_action_client_bookings")},
		{tr(lang, "button_action_book"), tr(lang, "button_action_my")},
		{tr(lang, "button_action_appoint"), tr(lang, "button_action_cancel")},
		{tr(lang, "button_action_reschedule"), tr(lang, "button_action_block")},
		{tr(lang, "button_back")},
	})
}

func servicesMenuKeyboard(lang string) *telegram.ReplyMarkup {
	return menuKeyboard([][]string{
		{tr(lang, "button_action_service_list"), tr(lang, "button_action_service_add")},
		{tr(lang, "button_action_service_edit"), tr(lang, "button_action_service_delete")},
		{tr(lang, "button_action_services_text"), tr(lang, "button_action_category_order")},
		{tr(lang, "button_back")},
	})
}

func scheduleMenuKeyboard(lang string) *telegram.ReplyMarkup {
	return menuKeyboard([][]string{
		{tr(lang, "button_action_calendar"), tr(lang, "button_action_free")},
		{tr(lang, "button_action_schedule_change"), tr(lang, "button_action_block_date")},
		{tr(lang, "button_action_set_hours"), tr(lang, "button_action_set_duration")},
		{tr(lang, "button_back")},
	})
}

func scheduleChangeKeyboard(lang string) *telegram.ReplyMarkup {
	return menuKeyboard([][]string{
		{tr(lang, "button_action_generate_month"), tr(lang, "button_action_generate_months")},
		{tr(lang, "button_action_generate_day"), tr(lang, "button_action_generate_weekday")},
		{tr(lang, "button_action_delete_month")},
		{tr(lang, "button_back")},
	})
}

func scheduleMonthsKeyboard(lang string, months []ScheduleMonth) *telegram.ReplyMarkup {
	if len(months) == 0 {
		return menuKeyboard([][]string{{tr(lang, "button_back")}})
	}
	var rows [][]string
	for i := 0; i < len(months); i += 3 {
		var row []string
		for j := i; j < len(months) && j < i+3; j++ {
			row = append(row, months[j].Month.Format(monthLayout))
		}
		rows = append(rows, row)
	}
	rows = append(rows, []string{tr(lang, "button_back")})
	return menuKeyboard(rows)
}

func scheduleDaysKeyboard(lang string, days []ScheduleDay) *telegram.ReplyMarkup {
	if len(days) == 0 {
		return menuKeyboard([][]string{{tr(lang, "button_back")}})
	}
	var rows [][]string
	for i := 0; i < len(days); i += 4 {
		var row []string
		for j := i; j < len(days) && j < i+4; j++ {
			row = append(row, days[j].Date.Format("2006-01-02"))
		}
		rows = append(rows, row)
	}
	rows = append(rows, []string{tr(lang, "button_back")})
	return menuKeyboard(rows)
}

func scheduleWeekdaysKeyboard(lang string, weekdays []ScheduleWeekday) *telegram.ReplyMarkup {
	if len(weekdays) == 0 {
		return menuKeyboard([][]string{{tr(lang, "button_back")}})
	}
	var rows [][]string
	for i := 0; i < len(weekdays); i += 3 {
		var row []string
		for j := i; j < len(weekdays) && j < i+3; j++ {
			row = append(row, weekdayInputValue(lang, weekdays[j].Weekday))
		}
		rows = append(rows, row)
	}
	rows = append(rows, []string{tr(lang, "button_back")})
	return menuKeyboard(rows)
}

func weekdayInputValue(lang string, weekday time.Weekday) string {
	if lang == LangEN {
		switch weekday {
		case time.Monday:
			return "mon"
		case time.Tuesday:
			return "tue"
		case time.Wednesday:
			return "wed"
		case time.Thursday:
			return "thu"
		case time.Friday:
			return "fri"
		case time.Saturday:
			return "sat"
		case time.Sunday:
			return "sun"
		}
	}
	switch weekday {
	case time.Monday:
		return "пн"
	case time.Tuesday:
		return "вт"
	case time.Wednesday:
		return "ср"
	case time.Thursday:
		return "чт"
	case time.Friday:
		return "пт"
	case time.Saturday:
		return "сб"
	case time.Sunday:
		return "вс"
	default:
		return ""
	}
}

func settingsMenuKeyboard(lang string) *telegram.ReplyMarkup {
	return menuKeyboard([][]string{
		{tr(lang, "button_action_set_profile"), tr(lang, "button_action_contact_aliases")},
		{tr(lang, "button_action_lang_ru"), tr(lang, "button_action_lang_en")},
		{"/help", tr(lang, "button_back")},
	})
}

func adminsMenuKeyboard(lang string) *telegram.ReplyMarkup {
	return menuKeyboard([][]string{
		{tr(lang, "button_action_admin_list")},
		{tr(lang, "button_action_view_admin"), tr(lang, "button_action_view_user")},
		{tr(lang, "button_action_view_super")},
		{tr(lang, "button_action_admin_add"), tr(lang, "button_action_admin_remove")},
		{tr(lang, "button_action_role")},
		{tr(lang, "button_back")},
	})
}

func viewAdminKeyboard(lang string, admins []AdminView) *telegram.ReplyMarkup {
	rows := make([][]telegram.InlineKeyboardButton, 0, len(admins))
	for _, admin := range admins {
		if admin.Username == "" {
			continue
		}
		rows = append(rows, []telegram.InlineKeyboardButton{{
			Text:         fmt.Sprintf("@%s - %s", admin.Username, roleLabel(lang, admin.Role)),
			CallbackData: "viewadmin:" + admin.Username,
		}})
	}
	if len(rows) == 0 {
		return nil
	}
	return &telegram.ReplyMarkup{InlineKeyboard: rows}
}

func cancelBookingKeyboard(lang string, items []BookingView) *telegram.ReplyMarkup {
	limit := len(items)
	if limit > 30 {
		limit = 30
	}
	rows := make([][]telegram.InlineKeyboardButton, 0, limit)
	for i := 0; i < limit; i++ {
		item := items[i]
		label := item.StartAt.Format("02.01 15:04")
		client := formatClientContact(item.Username)
		if client == "" {
			client = tr(lang, "unknown_user")
		}
		label += " - " + client
		if len(item.ServiceNames) > 0 {
			label += " - " + strings.Join(item.ServiceNames, ", ")
		}
		if item.AdminName != "" {
			label += " (@" + item.AdminName + ")"
		}
		rows = append(rows, []telegram.InlineKeyboardButton{{
			Text:         label,
			CallbackData: fmt.Sprintf("cancel:%d", item.ID),
		}})
	}
	return &telegram.ReplyMarkup{InlineKeyboard: rows}
}

func myActionsKeyboard(lang string) *telegram.ReplyMarkup {
	return &telegram.ReplyMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{
			{Text: tr(lang, "button_action_my"), CallbackData: "my:list"},
		},
		{
			{Text: tr(lang, "button_my_cancel"), CallbackData: "mycancel:list"},
			{Text: tr(lang, "button_my_move"), CallbackData: "mymove:list"},
		},
		{
			{Text: tr(lang, "button_my_new_booking"), CallbackData: "bookstart"},
		},
	}}
}

func myBookingActionKeyboard(lang string, items []BookingView, prefix string) *telegram.ReplyMarkup {
	limit := len(items)
	if limit > 20 {
		limit = 20
	}
	rows := make([][]telegram.InlineKeyboardButton, 0, limit)
	for i := 0; i < limit; i++ {
		item := items[i]
		rows = append(rows, []telegram.InlineKeyboardButton{{
			Text:         bookingButtonLabel(lang, item),
			CallbackData: fmt.Sprintf("%s:%d", prefix, item.ID),
		}})
	}
	return &telegram.ReplyMarkup{InlineKeyboard: rows}
}

func moveSlotKeyboard(lang string, bookingID int64, slots []AvailabilitySlot) *telegram.ReplyMarkup {
	limit := len(slots)
	if limit > 20 {
		limit = 20
	}
	rows := make([][]telegram.InlineKeyboardButton, 0, limit)
	for i := 0; i < limit; i++ {
		slot := slots[i]
		label := slot.StartAt.Format("02.01 15:04")
		if slot.DurationMin > 0 {
			label += fmt.Sprintf(" (%d %s)", slot.DurationMin, tr(lang, "minutes_short"))
		}
		rows = append(rows, []telegram.InlineKeyboardButton{{
			Text:         label,
			CallbackData: fmt.Sprintf("moveslot:%d:%d", bookingID, i+1),
		}})
	}
	return &telegram.ReplyMarkup{InlineKeyboard: rows}
}

func bookingButtonLabel(lang string, item BookingView) string {
	label := item.StartAt.Format("02.01 15:04")
	if item.AdminName != "" {
		label += " - @" + item.AdminName
	}
	if item.Username != "" {
		client := formatClientContact(item.Username)
		if client == "" {
			client = tr(lang, "unknown_user")
		}
		label += " - " + client
	}
	if len(item.ServiceNames) > 0 {
		label += " - " + strings.Join(item.ServiceNames, ", ")
	}
	return label
}

func roleChoiceKeyboard() *telegram.ReplyMarkup {
	return menuKeyboard([][]string{
		{"show", "admin"},
		{"user", "super_admin"},
	})
}
