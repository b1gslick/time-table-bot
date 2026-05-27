package bot

import "time-table-bot/internal/telegram"

func keyboardForRole(role Role) *telegram.ReplyMarkup {
	switch role {
	case RoleSuperAdmin:
		return &telegram.ReplyMarkup{
			ResizeKeyboard: true,
			Keyboard: [][]telegram.KeyboardButton{
				{{Text: "/help"}, {Text: "/lang ru"}, {Text: "/lang en"}},
				{{Text: "/schedule"}, {Text: "/free"}},
				{{Text: "/admin_add @username"}, {Text: "/admin_remove @username"}},
				{{Text: "/role @username"}, {Text: "/role @username admin"}},
				{{Text: "/services"}, {Text: "/service_add 30 Услуга"}},
				{{Text: "/sethours пн-пт 10:00-18:00"}, {Text: "/generate 2026-06"}},
			},
		}
	case RoleAdmin:
		return &telegram.ReplyMarkup{
			ResizeKeyboard: true,
			Keyboard: [][]telegram.KeyboardButton{
				{{Text: "/help"}, {Text: "/lang ru"}, {Text: "/lang en"}},
				{{Text: "/schedule"}, {Text: "/free"}},
				{{Text: "/services"}, {Text: "/service_add 30 Услуга"}},
				{{Text: "/sethours Пн-Пт 10:00-19:00"}, {Text: "/setduration 60"}},
				{{Text: "/generate 2026-06"}, {Text: "/appoint @username 2026-06-01 14:00"}},
			},
		}
	default:
		return &telegram.ReplyMarkup{
			ResizeKeyboard: true,
			Keyboard: [][]telegram.KeyboardButton{
				{{Text: "/help"}, {Text: "/lang ru"}, {Text: "/lang en"}},
				{{Text: "/services"}, {Text: "/schedule 1"}},
				{{Text: "/my"}, {Text: "/book 1"}},
				{{Text: "/move 1 2"}, {Text: "/settravel 30"}},
			},
		}
	}
}
