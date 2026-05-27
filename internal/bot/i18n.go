package bot

import "fmt"

const (
	LangRU = "ru"
	LangEN = "en"
)

var messages = map[string]map[string]string{
	LangRU: {
		"register_failed":      "Не удалось зарегистрировать пользователя.",
		"unknown_command":      "Неизвестная команда. Используйте /help",
		"start":                "Привет, @%s. Ваша роль: %s.\nЯзык: %s.\nИспользуйте /help для списка команд.",
		"help_base":            "Команды:\n/start - регистрация и показ роли\n/help - это сообщение\n/lang ru|en - выбрать язык\n/schedule - показать свободные места с номерами\n/my - мои будущие записи\n/book 3 - записаться на номер из /schedule\n/move 1 3 - перенести запись #1 из /my на место #3 из /schedule\n/settravel 30 - время в пути по умолчанию\n\nТакже работают полные форматы: /schedule 2026-06, /book YYYY-MM-DD HH:MM, /move FROM_DATE FROM_TIME TO_DATE TO_TIME.\n",
		"help_admin":           "/setprofile текст - профиль мастера\n/setservices текст - услуги и стоимость\n/sethours пн-пт 10:00-18:00 - рабочие дни/часы\n/setduration 60 - длительность сеанса\n/generate 2026-06 - создать слоты на месяц из /sethours и /setduration\n/appoint @username YYYY-MM-DD HH:MM - запись за пользователя\n/cancel @username YYYY-MM-DD HH:MM - удалить запись\n/reschedule @username YYYY-MM-DD HH:MM YYYY-MM-DD HH:MM - перенос\n/block YYYY-MM-DD HH:MM - блокировка слота\n",
		"help_super":           "/admin_add @username - назначить админа\n/admin_remove @username - снять админа\n/role @username [user|admin|super_admin] - показать или изменить роль\n",
		"admin_only":           "Команда доступна только админу.",
		"super_only_add":       "Только супер админ может назначать админов.",
		"super_only_remove":    "Только супер админ может снимать админов.",
		"super_only_role":      "Только супер админ может смотреть и менять роли.",
		"bad_username":         "Некорректный username.",
		"admin_add_usage":      "Формат: /admin_add @username",
		"admin_add_failed":     "Не удалось назначить админа.",
		"admin_added":          "Админ назначен: @%s",
		"admin_remove_usage":   "Формат: /admin_remove @username",
		"admin_remove_failed":  "Не удалось снять админа.",
		"admin_removed":        "Админ снят: @%s",
		"role_usage":           "Формат: /role @username [user|admin|super_admin]",
		"role_show_failed":     "Не удалось получить роль.",
		"role_current":         "Роль @%s: %s",
		"role_bad":             "Роль должна быть user, admin или super_admin.",
		"role_set_failed":      "Не удалось изменить роль.",
		"role_set":             "Роль @%s изменена на %s.",
		"lang_usage":           "Формат: /lang ru или /lang en",
		"lang_failed":          "Не удалось сохранить язык.",
		"lang_set":             "Язык переключен на русский.",
		"profile_usage":        "Формат: /setprofile текст",
		"profile_failed":       "Не удалось обновить профиль.",
		"profile_ok":           "Профиль обновлен.",
		"services_usage":       "Формат: /setservices текст",
		"services_failed":      "Не удалось обновить услуги.",
		"services_ok":          "Услуги обновлены.",
		"hours_usage":          "Формат: /sethours текст",
		"hours_failed":         "Не удалось обновить график.",
		"hours_ok":             "График обновлен.",
		"duration_usage":       "Формат: /setduration 60",
		"duration_bad":         "Длительность должна быть положительным числом.",
		"duration_failed":      "Не удалось обновить длительность.",
		"duration_ok":          "Длительность обновлена.",
		"generate_usage":       "Формат: /generate 2026-06. Сначала задайте /sethours пн-пт 10:00-18:00 и /setduration 60.",
		"generate_bad_days":    "Дни недели должны быть mon,tue,wed,thu,fri,sat,sun или 1-7.",
		"generate_bad_time":    "Время должно быть в формате HH:MM-HH:MM.",
		"generate_bad_range":   "Конец рабочего дня должен быть позже начала.",
		"generate_failed":      "Не удалось создать расписание.",
		"generate_ok":          "Расписание создано. Новых слотов: %d, уже существовало: %d.",
		"appoint_usage":        "Формат: /appoint @username YYYY-MM-DD HH:MM [минуты_в_пути]",
		"datetime_bad_example": "Некорректная дата/время. Пример: 2026-06-01 14:00",
		"travel_bad":           "Некорректные минуты в пути.",
		"appoint_failed":       "Не удалось добавить запись.",
		"appoint_ok":           "Запись добавлена для @%s",
		"cancel_usage":         "Формат: /cancel @username YYYY-MM-DD HH:MM",
		"datetime_bad":         "Некорректная дата/время.",
		"cancel_failed":        "Не удалось удалить запись.",
		"cancel_ok":            "Запись удалена для @%s",
		"reschedule_usage":     "Формат: /reschedule @username YYYY-MM-DD HH:MM YYYY-MM-DD HH:MM",
		"from_datetime_bad":    "Некорректная исходная дата/время.",
		"to_datetime_bad":      "Некорректная новая дата/время.",
		"reschedule_failed":    "Не удалось перенести запись.",
		"reschedule_ok":        "Запись перенесена для @%s",
		"block_usage":          "Формат: /block YYYY-MM-DD HH:MM",
		"block_failed":         "Не удалось заблокировать слот.",
		"block_ok":             "Слот заблокирован.",
		"free_usage":           "Формат: /schedule или /schedule YYYY-MM",
		"free_failed":          "Не удалось получить свободные слоты.",
		"free_empty":           "Свободных слотов нет.",
		"free_header":          "Свободные места. Для записи отправьте /book номер:\n",
		"free_more":            "... и еще %d слотов",
		"my_failed":            "Не удалось получить ваши записи.",
		"my_empty":             "Будущих записей нет.",
		"my_header":            "Ваши будущие записи. Для переноса сначала откройте /schedule, потом отправьте /move номер_записи номер_места:\n",
		"book_usage":           "Формат: /book 3 после /schedule или /book YYYY-MM-DD HH:MM [минуты_в_пути]",
		"book_need_schedule":   "Сначала откройте /schedule и выберите номер из списка. Если список устарел, откройте /schedule еще раз.",
		"book_failed":          "Не удалось выполнить запись.",
		"book_ok":              "Вы записаны на %s. Время в пути: %d мин.",
		"move_usage":           "Формат: /move 1 3 после /my и /schedule или /move FROM_DATE FROM_TIME TO_DATE TO_TIME",
		"move_need_lists":      "Сначала откройте /my и /schedule, потом отправьте /move номер_записи номер_места.",
		"move_failed":          "Не удалось перенести запись на выбранное свободное время.",
		"move_ok":              "Запись перенесена с %s на %s. Админ уведомлен.",
		"move_admin_notice":    "Клиент @%s перенес запись с %s на %s.",
		"settravel_usage":      "Формат: /settravel 30",
		"settravel_bad":        "Введите целое число >= 0.",
		"settravel_failed":     "Не удалось сохранить значение.",
		"settravel_ok":         "Время в пути по умолчанию: %d мин.",
		"role_user":            "клиент",
		"role_admin":           "админ",
		"role_super_admin":     "супер админ",
	},
	LangEN: {
		"register_failed":      "Failed to register the user.",
		"unknown_command":      "Unknown command. Use /help",
		"start":                "Hello, @%s. Your role: %s.\nLanguage: %s.\nUse /help to see commands.",
		"help_base":            "Commands:\n/start - register and show your role\n/help - this message\n/lang ru|en - choose language\n/schedule - show free slots with numbers\n/my - my upcoming bookings\n/book 3 - book a numbered slot from /schedule\n/move 1 3 - move booking #1 from /my to slot #3 from /schedule\n/settravel 30 - default travel time\n\nFull formats also work: /schedule 2026-06, /book YYYY-MM-DD HH:MM, /move FROM_DATE FROM_TIME TO_DATE TO_TIME.\n",
		"help_admin":           "/setprofile text - master profile\n/setservices text - services and prices\n/sethours mon-fri 10:00-18:00 - work days/hours\n/setduration 60 - session duration\n/generate 2026-06 - generate monthly slots from /sethours and /setduration\n/appoint @username YYYY-MM-DD HH:MM - book for a user\n/cancel @username YYYY-MM-DD HH:MM - cancel booking\n/reschedule @username YYYY-MM-DD HH:MM YYYY-MM-DD HH:MM - reschedule\n/block YYYY-MM-DD HH:MM - block a slot\n",
		"help_super":           "/admin_add @username - assign admin\n/admin_remove @username - remove admin\n/role @username [user|admin|super_admin] - show or change role\n",
		"admin_only":           "Admins only.",
		"super_only_add":       "Only super admin can assign admins.",
		"super_only_remove":    "Only super admin can remove admins.",
		"super_only_role":      "Only super admin can view and change roles.",
		"bad_username":         "Invalid username.",
		"admin_add_usage":      "Usage: /admin_add @username",
		"admin_add_failed":     "Failed to assign admin.",
		"admin_added":          "Admin assigned: @%s",
		"admin_remove_usage":   "Usage: /admin_remove @username",
		"admin_remove_failed":  "Failed to remove admin.",
		"admin_removed":        "Admin removed: @%s",
		"role_usage":           "Usage: /role @username [user|admin|super_admin]",
		"role_show_failed":     "Failed to get role.",
		"role_current":         "Role for @%s: %s",
		"role_bad":             "Role must be user, admin, or super_admin.",
		"role_set_failed":      "Failed to change role.",
		"role_set":             "Role for @%s changed to %s.",
		"lang_usage":           "Usage: /lang ru or /lang en",
		"lang_failed":          "Failed to save language.",
		"lang_set":             "Language switched to English.",
		"profile_usage":        "Usage: /setprofile text",
		"profile_failed":       "Failed to update profile.",
		"profile_ok":           "Profile updated.",
		"services_usage":       "Usage: /setservices text",
		"services_failed":      "Failed to update services.",
		"services_ok":          "Services updated.",
		"hours_usage":          "Usage: /sethours text",
		"hours_failed":         "Failed to update schedule.",
		"hours_ok":             "Schedule updated.",
		"duration_usage":       "Usage: /setduration 60",
		"duration_bad":         "Duration must be a positive number.",
		"duration_failed":      "Failed to update duration.",
		"duration_ok":          "Duration updated.",
		"generate_usage":       "Usage: /generate 2026-06. First set /sethours mon-fri 10:00-18:00 and /setduration 60.",
		"generate_bad_days":    "Weekdays must be mon,tue,wed,thu,fri,sat,sun or 1-7.",
		"generate_bad_time":    "Time must use HH:MM-HH:MM format.",
		"generate_bad_range":   "Workday end must be after start.",
		"generate_failed":      "Failed to generate schedule.",
		"generate_ok":          "Schedule generated. New slots: %d, already existed: %d.",
		"appoint_usage":        "Usage: /appoint @username YYYY-MM-DD HH:MM [travel_minutes]",
		"datetime_bad_example": "Invalid date/time. Example: 2026-06-01 14:00",
		"travel_bad":           "Invalid travel minutes.",
		"appoint_failed":       "Failed to add booking.",
		"appoint_ok":           "Booking added for @%s",
		"cancel_usage":         "Usage: /cancel @username YYYY-MM-DD HH:MM",
		"datetime_bad":         "Invalid date/time.",
		"cancel_failed":        "Failed to cancel booking.",
		"cancel_ok":            "Booking cancelled for @%s",
		"reschedule_usage":     "Usage: /reschedule @username YYYY-MM-DD HH:MM YYYY-MM-DD HH:MM",
		"from_datetime_bad":    "Invalid source date/time.",
		"to_datetime_bad":      "Invalid new date/time.",
		"reschedule_failed":    "Failed to reschedule booking.",
		"reschedule_ok":        "Booking rescheduled for @%s",
		"block_usage":          "Usage: /block YYYY-MM-DD HH:MM",
		"block_failed":         "Failed to block slot.",
		"block_ok":             "Slot blocked.",
		"free_usage":           "Usage: /schedule or /schedule YYYY-MM",
		"free_failed":          "Failed to get free slots.",
		"free_empty":           "No free slots.",
		"free_header":          "Free slots. To book, send /book number:\n",
		"free_more":            "... and %d more slots",
		"my_failed":            "Failed to get your bookings.",
		"my_empty":             "No upcoming bookings.",
		"my_header":            "Your upcoming bookings. To move, open /schedule, then send /move booking_number slot_number:\n",
		"book_usage":           "Usage: /book 3 after /schedule or /book YYYY-MM-DD HH:MM [travel_minutes]",
		"book_need_schedule":   "Open /schedule first and choose a number from the list. If the list is old, open /schedule again.",
		"book_failed":          "Failed to book.",
		"book_ok":              "You are booked for %s. Travel time: %d min.",
		"move_usage":           "Usage: /move 1 3 after /my and /schedule or /move FROM_DATE FROM_TIME TO_DATE TO_TIME",
		"move_need_lists":      "Open /my and /schedule first, then send /move booking_number slot_number.",
		"move_failed":          "Failed to move booking to the selected free slot.",
		"move_ok":              "Booking moved from %s to %s. Admin was notified.",
		"move_admin_notice":    "Client @%s moved booking from %s to %s.",
		"settravel_usage":      "Usage: /settravel 30",
		"settravel_bad":        "Enter an integer >= 0.",
		"settravel_failed":     "Failed to save value.",
		"settravel_ok":         "Default travel time: %d min.",
		"role_user":            "user",
		"role_admin":           "admin",
		"role_super_admin":     "super admin",
	},
}

func normalizeLanguage(lang string) string {
	switch lang {
	case LangEN:
		return LangEN
	default:
		return LangRU
	}
}

func tr(lang, key string, args ...any) string {
	lang = normalizeLanguage(lang)
	value := messages[lang][key]
	if value == "" {
		value = messages[LangRU][key]
	}
	if len(args) == 0 {
		return value
	}
	return fmt.Sprintf(value, args...)
}

func roleLabel(lang string, role Role) string {
	switch role {
	case RoleAdmin:
		return tr(lang, "role_admin")
	case RoleSuperAdmin:
		return tr(lang, "role_super_admin")
	default:
		return tr(lang, "role_user")
	}
}
