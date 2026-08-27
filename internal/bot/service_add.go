package bot

import (
	"context"
	"fmt"
	"strings"

	"time-table-bot/internal/telegram"
)

const serviceAddCallbackPrefix = "serviceadd:"

func (b *Bot) handleServiceAddCallback(ctx context.Context, cb *telegram.CallbackQuery) error {
	user, err := b.userFromCallback(ctx, cb)
	if err != nil {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(LangRU, "register_failed"))
	}
	if !isAdmin(user.Role) {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(user.Language, "admin_only"))
	}
	action := strings.TrimPrefix(cb.Data, serviceAddCallbackPrefix)
	if action == "cancel" {
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendTextWithKeyboard(ctx, cb.Message.Chat.ID, tr(user.Language, "menu_services_text"), servicesMenuKeyboard(user.Language))
	}
	state, err := b.store.GetConversationState(ctx, user.TelegramID)
	if err != nil || !isServiceAddStep(state.Step) {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(user.Language, "service_add_expired"))
	}
	if raw, ok := strings.CutPrefix(action, "category:"); ok {
		if state.Step != conversationStepAddSvcCat {
			return b.sendText(ctx, cb.Message.Chat.ID, tr(user.Language, "service_add_expired"))
		}
		if raw == "0" {
			raw = "-"
		}
		return b.conversationAddServiceCategory(ctx, cb.Message.Chat.ID, user, state, raw)
	}
	if raw, ok := strings.CutPrefix(action, "subcategory:"); ok {
		if state.Step != conversationStepAddSvcSub {
			return b.sendText(ctx, cb.Message.Chat.ID, tr(user.Language, "service_add_expired"))
		}
		if raw == "0" {
			raw = "-"
		}
		return b.conversationAddServiceSubcategory(ctx, cb.Message.Chat.ID, user, state, raw)
	}
	if action == "save" && state.Step == conversationStepAddSvcConfirm {
		return b.saveServiceAdd(ctx, cb.Message.Chat.ID, user, state)
	}
	if field, ok := strings.CutPrefix(action, "edit:"); ok && state.Step == conversationStepAddSvcConfirm {
		return b.beginServiceAddReviewEdit(ctx, cb.Message.Chat.ID, user, state, field)
	}
	return b.sendText(ctx, cb.Message.Chat.ID, tr(user.Language, "service_add_expired"))
}

func isServiceAddStep(step string) bool {
	switch step {
	case conversationStepAddSvcCat, conversationStepAddSvcSub, conversationStepAddSvcName,
		conversationStepAddSvcDur, conversationStepAddSvcPrice, conversationStepAddSvcDesc,
		conversationStepAddSvcConfirm:
		return true
	default:
		return false
	}
}

func (b *Bot) showServiceAddReview(ctx context.Context, chatID int64, user UserRecord, state ConversationState) error {
	state.Step = conversationStepAddSvcConfirm
	state.ServiceEditField = ""
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	price := normalizeServicePrice(state.ServicePriceText)
	if price == "" {
		price = tr(user.Language, "service_edit_empty")
	}
	description := strings.TrimSpace(state.ServiceDescription)
	if description == "" {
		description = tr(user.Language, "service_edit_empty")
	}
	text := tr(user.Language, "service_add_review", servicePathFromState(state), state.ServiceIndex, price, description)
	return b.sendTextWithKeyboard(ctx, chatID, text, serviceAddReviewKeyboard(user.Language))
}

func (b *Bot) beginServiceAddReviewEdit(ctx context.Context, chatID int64, user UserRecord, state ConversationState, field string) error {
	state.ServiceEditField = "add_review"
	switch field {
	case serviceEditFieldCategory:
		return b.askServiceAddCategory(ctx, chatID, user, state)
	case serviceEditFieldSubcategory:
		return b.askServiceAddSubcategory(ctx, chatID, user, state)
	case serviceEditFieldName:
		state.Step = conversationStepAddSvcName
	case serviceEditFieldDuration:
		state.Step = conversationStepAddSvcDur
	case serviceEditFieldPrice:
		state.Step = conversationStepAddSvcPrice
	case serviceEditFieldDescription:
		state.Step = conversationStepAddSvcDesc
	default:
		return b.showServiceAddReview(ctx, chatID, user, state)
	}
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	key := map[string]string{
		serviceEditFieldName:        "service_add_ask_name",
		serviceEditFieldDuration:    "service_add_ask_duration",
		serviceEditFieldPrice:       "service_add_ask_price",
		serviceEditFieldDescription: "service_add_ask_description",
	}[field]
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, key), serviceAddInputKeyboard(user.Language))
}

func serviceAddCategoryKeyboard(lang string, categories []string) *telegram.ReplyMarkup {
	rows := serviceAddChoiceRows("category", categories)
	rows = append(rows, []telegram.InlineKeyboardButton{{Text: tr(lang, "service_add_without_category"), CallbackData: "serviceadd:category:0"}})
	rows = append(rows, []telegram.InlineKeyboardButton{{Text: tr(lang, "button_cancel"), CallbackData: "serviceadd:cancel"}})
	return &telegram.ReplyMarkup{InlineKeyboard: rows}
}

func serviceAddSubcategoryKeyboard(lang string, subcategories []string) *telegram.ReplyMarkup {
	rows := serviceAddChoiceRows("subcategory", subcategories)
	rows = append(rows, []telegram.InlineKeyboardButton{{Text: tr(lang, "service_add_without_subcategory"), CallbackData: "serviceadd:subcategory:0"}})
	rows = append(rows, []telegram.InlineKeyboardButton{{Text: tr(lang, "button_cancel"), CallbackData: "serviceadd:cancel"}})
	return &telegram.ReplyMarkup{InlineKeyboard: rows}
}

func serviceAddChoiceRows(kind string, values []string) [][]telegram.InlineKeyboardButton {
	rows := make([][]telegram.InlineKeyboardButton, 0, len(values))
	for sourceIndex, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		rows = append(rows, []telegram.InlineKeyboardButton{{Text: value, CallbackData: fmt.Sprintf("serviceadd:%s:%d", kind, sourceIndex+1)}})
	}
	return rows
}

func serviceAddInputKeyboard(lang string) *telegram.ReplyMarkup {
	return &telegram.ReplyMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: tr(lang, "button_cancel"), CallbackData: "serviceadd:cancel"}},
	}}
}

func serviceAddReviewKeyboard(lang string) *telegram.ReplyMarkup {
	return &telegram.ReplyMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: tr(lang, "service_add_save"), CallbackData: "serviceadd:save"}},
		{
			{Text: tr(lang, "service_edit_field_price"), CallbackData: "serviceadd:edit:price"},
			{Text: tr(lang, "service_edit_field_description"), CallbackData: "serviceadd:edit:description"},
		},
		{
			{Text: tr(lang, "service_edit_field_duration"), CallbackData: "serviceadd:edit:duration"},
			{Text: tr(lang, "service_edit_field_name"), CallbackData: "serviceadd:edit:name"},
		},
		{
			{Text: tr(lang, "service_edit_field_category"), CallbackData: "serviceadd:edit:category"},
			{Text: tr(lang, "service_edit_field_subcategory"), CallbackData: "serviceadd:edit:subcategory"},
		},
		{{Text: tr(lang, "button_cancel"), CallbackData: "serviceadd:cancel"}},
	}}
}
