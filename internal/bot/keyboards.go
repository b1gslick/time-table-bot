package bot

import (
	"fmt"
	"strings"

	"time-table-bot/internal/telegram"
)

func keyboardForRole(role Role, lang string) *telegram.ReplyMarkup {
	switch role {
	case RoleSuperAdmin:
		return menuKeyboard([][]string{
			{tr(lang, "button_menu_calendar"), tr(lang, "button_menu_bookings")},
			{tr(lang, "button_menu_services"), tr(lang, "button_menu_schedule")},
			{tr(lang, "button_menu_settings"), tr(lang, "button_menu_admins")},
			{"/help"},
		})
	case RoleAdmin:
		return menuKeyboard([][]string{
			{tr(lang, "button_menu_calendar"), tr(lang, "button_menu_bookings")},
			{tr(lang, "button_menu_services"), tr(lang, "button_menu_schedule")},
			{tr(lang, "button_menu_settings"), "/help"},
		})
	default:
		return menuKeyboard([][]string{
			{tr(lang, "button_start_booking")},
			{"/help"},
		})
	}
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

func calendarMenuKeyboard(lang string) *telegram.ReplyMarkup {
	return menuKeyboard([][]string{
		{tr(lang, "button_action_calendar"), tr(lang, "button_action_free")},
		{tr(lang, "button_back")},
	})
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
		{tr(lang, "button_action_set_hours"), tr(lang, "button_action_set_duration")},
		{tr(lang, "button_action_generate"), tr(lang, "button_action_calendar")},
		{tr(lang, "button_action_block_date"), tr(lang, "button_action_delete_month")},
		{tr(lang, "button_back")},
	})
}

func settingsMenuKeyboard(lang string) *telegram.ReplyMarkup {
	return menuKeyboard([][]string{
		{tr(lang, "button_action_set_profile")},
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
