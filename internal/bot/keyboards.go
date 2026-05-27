package bot

import "time-table-bot/internal/telegram"

func keyboardForRole(role Role) *telegram.ReplyMarkup {
	switch role {
	case RoleSuperAdmin:
		return &telegram.ReplyMarkup{
			ResizeKeyboard: true,
			Keyboard: [][]telegram.KeyboardButton{
				{{Text: "/help"}, {Text: "/lang en"}},
				{{Text: "/schedule"}, {Text: "/free"}},
				{{Text: "/admin_add @username"}, {Text: "/admin_remove @username"}},
				{{Text: "/role @username"}, {Text: "/role @username admin"}},
				{{Text: "/setprofile текст"}, {Text: "/setservices список"}},
			},
		}
	case RoleAdmin:
		return &telegram.ReplyMarkup{
			ResizeKeyboard: true,
			Keyboard: [][]telegram.KeyboardButton{
				{{Text: "/help"}, {Text: "/lang en"}},
				{{Text: "/schedule"}, {Text: "/free"}},
				{{Text: "/sethours Пн-Пт 10:00-19:00"}, {Text: "/setduration 60"}},
				{{Text: "/appoint @username 2026-06-01 14:00"}},
			},
		}
	default:
		return &telegram.ReplyMarkup{
			ResizeKeyboard: true,
			Keyboard: [][]telegram.KeyboardButton{
				{{Text: "/help"}, {Text: "/lang en"}},
				{{Text: "/schedule"}, {Text: "/free"}},
				{{Text: "/book 2026-06-01 14:00"}, {Text: "/move 2026-06-01 14:00 2026-06-02 14:00"}},
				{{Text: "/settravel 30"}},
			},
		}
	}
}
