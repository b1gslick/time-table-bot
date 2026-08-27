package bot

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"time-table-bot/internal/store"
	"time-table-bot/internal/telegram"
)

const (
	serviceEditFieldPrice     = "price"
	serviceEditFieldDuration  = "duration"
	serviceEditFieldName      = "name"
	serviceEditFieldSections  = "sections"
	serviceEditFieldAll       = "all"
	serviceEditCallbackPrefix = "serviceedit:"
	serviceEditMaxButtonRunes = 48
)

var serviceEditDurationValueRE = regexp.MustCompile(`(?i)(\d+(?:[.,]\d+)?)\s*(мин(?:ут[а-я]*)?|minutes?|mins?|час(?:а|ов)?|hours?|hrs?)`)

func (b *Bot) handleServiceEditCallback(ctx context.Context, cb *telegram.CallbackQuery) error {
	user, err := b.userFromCallback(ctx, cb)
	if err != nil {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(LangRU, "register_failed"))
	}
	if !isAdmin(user.Role) {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(user.Language, "admin_only"))
	}
	action := strings.TrimPrefix(cb.Data, serviceEditCallbackPrefix)
	if action == "cancel" {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendTextWithKeyboard(ctx, cb.Message.Chat.ID, tr(user.Language, "menu_services_text"), servicesMenuKeyboard(user.Language))
	}
	if action == "list" {
		return b.askServiceEdit(ctx, cb.Message.Chat.ID, user)
	}
	state, err := b.store.GetConversationState(ctx, user.TelegramID)
	if err != nil || (state.Step != conversationStepEditSvc && state.Step != conversationStepEditSvcData) {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(user.Language, "service_edit_expired"))
	}
	if rawIndex, ok := strings.CutPrefix(action, "pick:"); ok {
		index, parseErr := strconv.Atoi(rawIndex)
		if parseErr != nil {
			return b.sendText(ctx, cb.Message.Chat.ID, tr(user.Language, "service_edit_bad_index"))
		}
		return b.selectServiceForEdit(ctx, cb.Message.Chat.ID, user, state, index)
	}
	if field, ok := strings.CutPrefix(action, "field:"); ok && validServiceEditField(field) {
		state.ServiceEditField = field
		state.Step = conversationStepEditSvcData
		if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
			return b.sendText(ctx, cb.Message.Chat.ID, tr(user.Language, "conversation_failed"))
		}
		services, listErr := b.store.ListServices(ctx, user.TelegramID)
		if listErr != nil || state.ServiceIndex <= 0 || state.ServiceIndex > len(services) {
			return b.sendText(ctx, cb.Message.Chat.ID, tr(user.Language, "service_edit_bad_index"))
		}
		return b.sendTextWithKeyboard(ctx, cb.Message.Chat.ID, serviceEditFieldPrompt(user.Language, field, services[state.ServiceIndex-1]), serviceEditInputKeyboard(user.Language))
	}
	return nil
}

func (b *Bot) selectServiceForEdit(ctx context.Context, chatID int64, user UserRecord, state ConversationState, index int) error {
	services, err := b.store.ListServices(ctx, user.TelegramID)
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "services_list_failed"))
	}
	if index <= 0 || index > len(services) {
		return b.sendText(ctx, chatID, tr(user.Language, "service_edit_bad_index"))
	}
	state.Step = conversationStepEditSvcData
	state.ServiceIndex = index
	state.ServiceEditField = ""
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	service := services[index-1]
	return b.sendTextWithKeyboard(ctx, chatID, formatServiceEditCard(user.Language, index, service), serviceEditFieldKeyboard(user.Language))
}

func (b *Bot) applyServiceEditInput(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	services, err := b.store.ListServices(ctx, user.TelegramID)
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "services_list_failed"))
	}
	if state.ServiceIndex <= 0 || state.ServiceIndex > len(services) {
		return b.sendText(ctx, chatID, tr(user.Language, "service_edit_bad_index"))
	}
	current := services[state.ServiceIndex-1]
	path := serviceViewPath(current)
	duration := current.DurationMin
	description := current.Description

	switch state.ServiceEditField {
	case serviceEditFieldPrice:
		description = normalizeOptionalText(text)
	case serviceEditFieldDuration:
		value, ok := parseServiceEditDuration(text)
		if !ok {
			return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "service_edit_duration_bad"), serviceEditInputKeyboard(user.Language))
		}
		duration = value
	case serviceEditFieldName:
		name := strings.TrimSpace(text)
		if name == "" || name == "-" || strings.Contains(name, ">") {
			return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "service_edit_name_bad"), serviceEditInputKeyboard(user.Language))
		}
		path = servicePath(current.Category, current.Subcategory, name)
	case serviceEditFieldSections:
		category, subcategory, ok := parseServiceEditSections(text)
		if !ok {
			return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "service_edit_sections_bad"), serviceEditInputKeyboard(user.Language))
		}
		path = servicePath(category, subcategory, current.Name)
	case serviceEditFieldAll, "":
		newDuration, newPath, newDescription, hasDescription, ok := parseServiceEditDataPatch(text)
		if !ok {
			return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "service_edit_ask_data"), serviceEditInputKeyboard(user.Language))
		}
		duration = newDuration
		path = newPath
		if hasDescription {
			description = newDescription
		}
	default:
		return b.sendTextWithKeyboard(ctx, chatID, formatServiceEditCard(user.Language, state.ServiceIndex, current), serviceEditFieldKeyboard(user.Language))
	}

	if err := b.store.EditServiceByIndex(ctx, user.TelegramID, state.ServiceIndex, path, duration, description); err != nil {
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalidArgument) {
			return b.sendText(ctx, chatID, tr(user.Language, "service_edit_bad_index"))
		}
		b.logger.Printf("interactive service edit failed admin=%d index=%d duration=%d name=%q: %v", user.TelegramID, state.ServiceIndex, duration, path, err)
		return b.sendText(ctx, chatID, tr(user.Language, "service_edit_failed"))
	}
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "service_edit_ok", state.ServiceIndex), serviceEditDoneKeyboard(user.Language))
}

func parseServiceEditDuration(text string) (int, bool) {
	normalized := strings.ToLower(strings.TrimSpace(text))
	if normalized == "" {
		return 0, false
	}
	if strings.Contains(normalized, "полтора") || strings.Contains(normalized, "one and a half") {
		return 90, true
	}
	if value, err := strconv.Atoi(normalized); err == nil && value > 0 {
		return value, true
	}
	matches := serviceEditDurationValueRE.FindAllStringSubmatch(normalized, -1)
	if len(matches) == 0 {
		return 0, false
	}
	total := 0.0
	for _, match := range matches {
		value, err := strconv.ParseFloat(strings.ReplaceAll(match[1], ",", "."), 64)
		if err != nil || value <= 0 {
			return 0, false
		}
		if strings.HasPrefix(match[2], "час") || strings.HasPrefix(match[2], "hour") || strings.HasPrefix(match[2], "hr") {
			value *= 60
		}
		total += value
	}
	minutes := int(total + 0.5)
	return minutes, minutes > 0
}

func parseServiceEditSections(text string) (string, string, bool) {
	text = strings.TrimSpace(text)
	if text == "-" {
		return "", "", true
	}
	parts := strings.Split(text, ">")
	if len(parts) == 0 || len(parts) > 2 {
		return "", "", false
	}
	category := strings.TrimSpace(parts[0])
	if category == "" || category == "-" {
		category = ""
	}
	subcategory := ""
	if len(parts) == 2 {
		subcategory = strings.TrimSpace(parts[1])
		if subcategory == "-" {
			subcategory = ""
		}
	}
	if category == "" && subcategory != "" {
		return "", "", false
	}
	return category, subcategory, true
}

func servicePath(category, subcategory, name string) string {
	parts := make([]string, 0, 3)
	for _, part := range []string{category, subcategory, name} {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, " > ")
}

func validServiceEditField(field string) bool {
	switch field {
	case serviceEditFieldPrice, serviceEditFieldDuration, serviceEditFieldName, serviceEditFieldSections, serviceEditFieldAll:
		return true
	default:
		return false
	}
}

func formatServiceEditCard(lang string, index int, service ServiceView) string {
	description := strings.TrimSpace(service.Description)
	if description == "" {
		description = tr(lang, "service_edit_empty")
	}
	return tr(lang, "service_edit_card", index, serviceViewPath(service), service.DurationMin, description)
}

func serviceEditFieldPrompt(lang, field string, service ServiceView) string {
	switch field {
	case serviceEditFieldPrice:
		current := strings.TrimSpace(service.Description)
		if current == "" {
			current = tr(lang, "service_edit_empty")
		}
		return tr(lang, "service_edit_price_ask", current)
	case serviceEditFieldDuration:
		return tr(lang, "service_edit_duration_ask", service.DurationMin)
	case serviceEditFieldName:
		return tr(lang, "service_edit_name_ask", service.Name)
	case serviceEditFieldSections:
		sections := servicePath(service.Category, service.Subcategory, "")
		if sections == "" {
			sections = tr(lang, "service_edit_empty")
		}
		return tr(lang, "service_edit_sections_ask", sections)
	default:
		return tr(lang, "service_edit_ask_data")
	}
}

func serviceEditServiceKeyboard(lang string, services []ServiceView) *telegram.ReplyMarkup {
	rows := make([][]telegram.InlineKeyboardButton, 0, len(services)+1)
	for index, service := range services {
		label := fmt.Sprintf("%d. %s — %d %s", index+1, serviceViewPath(service), service.DurationMin, tr(lang, "minutes_short"))
		runes := []rune(label)
		if len(runes) > serviceEditMaxButtonRunes {
			label = string(runes[:serviceEditMaxButtonRunes-1]) + "…"
		}
		rows = append(rows, []telegram.InlineKeyboardButton{{Text: label, CallbackData: fmt.Sprintf("serviceedit:pick:%d", index+1)}})
	}
	rows = append(rows, []telegram.InlineKeyboardButton{{Text: tr(lang, "button_back"), CallbackData: "serviceedit:cancel"}})
	return &telegram.ReplyMarkup{InlineKeyboard: rows}
}

func serviceEditFieldKeyboard(lang string) *telegram.ReplyMarkup {
	return &telegram.ReplyMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{
			{Text: tr(lang, "service_edit_field_price"), CallbackData: "serviceedit:field:price"},
			{Text: tr(lang, "service_edit_field_duration"), CallbackData: "serviceedit:field:duration"},
		},
		{
			{Text: tr(lang, "service_edit_field_name"), CallbackData: "serviceedit:field:name"},
			{Text: tr(lang, "service_edit_field_sections"), CallbackData: "serviceedit:field:sections"},
		},
		{{Text: tr(lang, "service_edit_field_all"), CallbackData: "serviceedit:field:all"}},
		{{Text: tr(lang, "button_back"), CallbackData: "serviceedit:list"}},
	}}
}

func serviceEditInputKeyboard(lang string) *telegram.ReplyMarkup {
	return &telegram.ReplyMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: tr(lang, "button_back"), CallbackData: "serviceedit:list"}},
	}}
}

func serviceEditDoneKeyboard(lang string) *telegram.ReplyMarkup {
	return &telegram.ReplyMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: tr(lang, "service_edit_another"), CallbackData: "serviceedit:list"}},
		{{Text: tr(lang, "service_edit_done"), CallbackData: "serviceedit:cancel"}},
	}}
}
