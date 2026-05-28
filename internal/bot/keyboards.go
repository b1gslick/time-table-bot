package bot

import "time-table-bot/internal/telegram"

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
		{tr(lang, "button_action_book"), tr(lang, "button_action_my")},
		{tr(lang, "button_action_appoint"), tr(lang, "button_action_cancel")},
		{tr(lang, "button_action_reschedule"), tr(lang, "button_action_block")},
		{tr(lang, "button_back")},
	})
}

func servicesMenuKeyboard(lang string) *telegram.ReplyMarkup {
	return menuKeyboard([][]string{
		{tr(lang, "button_action_service_list"), tr(lang, "button_action_service_add")},
		{tr(lang, "button_action_service_delete"), tr(lang, "button_action_services_text")},
		{tr(lang, "button_back")},
	})
}

func scheduleMenuKeyboard(lang string) *telegram.ReplyMarkup {
	return menuKeyboard([][]string{
		{tr(lang, "button_action_set_hours"), tr(lang, "button_action_set_duration")},
		{tr(lang, "button_action_generate"), tr(lang, "button_action_calendar")},
		{tr(lang, "button_action_delete_month")},
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
		{tr(lang, "button_action_admin_add"), tr(lang, "button_action_admin_remove")},
		{tr(lang, "button_action_role")},
		{tr(lang, "button_back")},
	})
}

func roleChoiceKeyboard() *telegram.ReplyMarkup {
	return menuKeyboard([][]string{
		{"show", "admin"},
		{"user", "super_admin"},
	})
}
