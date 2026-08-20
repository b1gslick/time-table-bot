package bot

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"time-table-bot/internal/nlu"
	"time-table-bot/internal/store"
	"time-table-bot/internal/telegram"
)

const (
	serviceImportMinConfidence = 0.45
	serviceImportMaxTextRunes  = 8000
)

var (
	serviceImportDurationRE = regexp.MustCompile(`(?i)(?:\d+(?:[.,]\d+)?\s*(?:мин(?:ут[а-я]*)?|час(?:а|ов)?|minutes?|hours?|hrs?)|полтора\s+час)`)
	serviceImportPriceRE    = regexp.MustCompile(`(?i)(?:€|eur|евро|₽|руб(?:лей|ля)?|\$|usd|доллар)`)
)

type evaluatedServiceImport struct {
	Draft ServiceImportDraft
	Path  string
	Ready bool
	Issue string
}

func looksLikeAdminServiceImport(text string) bool {
	if looksLikeServiceCatalogReplace(text) {
		return true
	}
	normalized := normalizeMatchText(text)
	for _, marker := range []string{
		"добавь услуг", "добавить услуг", "создай услуг", "создать услуг", "новые услуги",
		"импорт услуг", "загрузи услуг", "добавь в прайс", "обнови прайс", "вот услуги",
		"измени список услуг", "замени список услуг", "обнови список услуг",
		"add service", "import service", "new service", "update price list", "price list",
		"change service list", "replace service list", "update service list",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	if !serviceImportDurationRE.MatchString(text) || !serviceImportPriceRE.MatchString(text) {
		return false
	}
	for _, bookingMarker := range []string{"запиши", "записать", "клиент", "свобод", "окно", "завтра", "сегодня", "booking", "appointment"} {
		if strings.Contains(normalized, bookingMarker) {
			return false
		}
	}
	return true
}

func looksLikeServiceCatalogReplace(text string) bool {
	normalized := normalizeMatchText(text)
	for _, marker := range []string{
		"измени список услуг", "замени список услуг", "обнови список услуг", "новый полный список услуг", "полный новый список услуг",
		"замени весь прайс", "полностью обнови прайс", "полный новый прайс",
		"change service list", "replace service list", "update service list", "replace the price list",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func (b *Bot) beginServiceCatalogReplace(ctx context.Context, chatID int64, user UserRecord) error {
	if !isAdmin(user.Role) {
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	if _, ok := b.adminBookingParser.(nlu.AdminServiceImportParser); !ok {
		return b.sendText(ctx, chatID, tr(user.Language, "service_replace_unavailable"))
	}
	if err := b.store.SetConversationState(ctx, user.TelegramID, ConversationState{Step: conversationStepServiceReplace, ServiceImportReplace: true}); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "service_replace_ask"), serviceReplaceInputKeyboard(user.Language))
}

func (b *Bot) handleServiceCatalogReplaceCommand(ctx context.Context, chatID int64, user UserRecord, text string) error {
	if !isAdmin(user.Role) {
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	value := strings.TrimSpace(strings.TrimPrefix(text, "/setservices"))
	if value == "" {
		return b.beginServiceCatalogReplace(ctx, chatID, user)
	}
	return b.handleServiceCatalogText(ctx, chatID, user, value, true)
}

func (b *Bot) handleAdminNaturalServiceImport(ctx context.Context, chatID int64, user UserRecord, text string) (bool, error) {
	parser, ok := b.adminBookingParser.(nlu.AdminServiceImportParser)
	if !ok || !isAdmin(user.Role) || !looksLikeAdminServiceImport(text) {
		return false, nil
	}
	return true, b.parseServiceCatalog(ctx, chatID, user, text, looksLikeServiceCatalogReplace(text), parser)
}

func (b *Bot) handleServiceCatalogText(ctx context.Context, chatID int64, user UserRecord, text string, replace bool) error {
	parser, ok := b.adminBookingParser.(nlu.AdminServiceImportParser)
	if !ok {
		return b.sendText(ctx, chatID, tr(user.Language, "service_replace_unavailable"))
	}
	return b.parseServiceCatalog(ctx, chatID, user, text, replace, parser)
}

func (b *Bot) parseServiceCatalog(ctx context.Context, chatID int64, user UserRecord, text string, replace bool, parser nlu.AdminServiceImportParser) error {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > serviceImportMaxTextRunes {
		return b.sendText(ctx, chatID, tr(user.Language, "service_import_parse_failed"))
	}
	services, err := b.store.ListServices(ctx, user.TelegramID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return b.sendText(ctx, chatID, tr(user.Language, "service_import_failed"))
	}
	if err := b.sendText(ctx, chatID, tr(user.Language, "service_import_processing")); err != nil {
		return err
	}
	intent, err := parser.ParseAdminServiceImport(ctx, nlu.AdminServiceImportRequest{
		Text: text, Language: user.Language, ExistingServices: nluServices(services),
	})
	if err != nil {
		b.logger.Printf("service import: parser failed admin=%d: %v", user.TelegramID, err)
		return b.sendText(ctx, chatID, tr(user.Language, "service_import_parse_failed"))
	}
	if !intent.IsServiceCatalog || intent.Confidence < serviceImportMinConfidence || len(intent.Entries) == 0 {
		return b.sendText(ctx, chatID, tr(user.Language, "service_import_no_entries"))
	}
	entries := make([]ServiceImportDraft, 0, len(intent.Entries))
	for _, entry := range intent.Entries {
		entries = append(entries, ServiceImportDraft{
			Category: entry.Category, Subcategory: entry.Subcategory, Name: entry.Name,
			DurationMin: entry.DurationMin, PriceText: entry.PriceText, Confidence: entry.Confidence,
		})
	}
	state := ConversationState{Step: conversationStepServiceImport, ServiceImportEntries: entries, ServiceImportReplace: replace}
	return b.showServiceImportPreview(ctx, chatID, user, state)
}

func (b *Bot) showServiceImportPreview(ctx context.Context, chatID int64, user UserRecord, state ConversationState) error {
	items := evaluateServiceImport(user.Language, state.ServiceImportEntries)
	state.Step = conversationStepServiceImport
	state.ServiceImportEntries = make([]ServiceImportDraft, 0, len(items))
	ready := 0
	for _, item := range items {
		state.ServiceImportEntries = append(state.ServiceImportEntries, item.Draft)
		if item.Ready {
			ready++
		}
	}
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	canConfirm := ready > 0
	if state.ServiceImportReplace {
		canConfirm = ready == len(items)
	}
	return b.sendTextWithKeyboard(ctx, chatID, formatServiceImportPreview(user.Language, items, state.ServiceImportReplace), serviceImportKeyboard(user.Language, canConfirm, state.ServiceImportReplace))
}

func evaluateServiceImport(lang string, drafts []ServiceImportDraft) []evaluatedServiceImport {
	out := make([]evaluatedServiceImport, 0, len(drafts))
	seen := make(map[string]struct{})
	for _, draft := range drafts {
		draft.Category = cleanServiceImportPart(draft.Category)
		draft.Subcategory = cleanServiceImportPart(draft.Subcategory)
		draft.Name = cleanServiceImportPart(draft.Name)
		draft.PriceText = strings.TrimSpace(draft.PriceText)
		item := evaluatedServiceImport{Draft: draft}
		switch {
		case draft.Name == "":
			item.Issue = tr(lang, "service_import_issue_name")
		case draft.DurationMin <= 0:
			item.Issue = tr(lang, "service_import_issue_duration")
		case draft.Confidence > 0 && draft.Confidence < serviceImportMinConfidence:
			item.Issue = tr(lang, "service_import_issue_uncertain")
		default:
			key := strings.ToLower(serviceImportPath(draft)) + fmt.Sprintf("\x00%d", draft.DurationMin)
			if _, exists := seen[key]; exists {
				item.Issue = tr(lang, "service_import_issue_duplicate")
			} else {
				seen[key] = struct{}{}
				item.Ready = true
			}
		}
		item.Path = serviceImportPath(draft)
		out = append(out, item)
	}
	return out
}

func cleanServiceImportPart(value string) string {
	value = strings.ReplaceAll(value, ">", " ")
	return strings.Join(strings.Fields(value), " ")
}

func serviceImportPath(draft ServiceImportDraft) string {
	parts := make([]string, 0, 3)
	for _, value := range []string{draft.Category, draft.Subcategory, draft.Name} {
		if value = strings.TrimSpace(value); value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, " > ")
}

func (b *Bot) confirmServiceImport(ctx context.Context, chatID int64, user UserRecord, state ConversationState) error {
	items := evaluateServiceImport(user.Language, state.ServiceImportEntries)
	if state.ServiceImportReplace {
		catalog := make([]ServiceCatalogEntry, 0, len(items))
		for _, item := range items {
			if !item.Ready {
				return b.showServiceImportPreview(ctx, chatID, user, state)
			}
			catalog = append(catalog, ServiceCatalogEntry{Path: item.Path, DurationMin: item.Draft.DurationMin, PriceText: item.Draft.PriceText})
		}
		if err := b.store.ReplaceServices(ctx, user.TelegramID, catalog); err != nil {
			b.logger.Printf("service replace: save failed admin=%d: %v", user.TelegramID, err)
			return b.sendText(ctx, chatID, tr(user.Language, "service_replace_failed"))
		}
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "service_replace_done", len(catalog)), keyboardForUser(user))
	}
	added, skipped := 0, 0
	for _, item := range items {
		if !item.Ready {
			skipped++
			continue
		}
		if err := b.store.AddService(ctx, user.TelegramID, item.Path, item.Draft.DurationMin, item.Draft.PriceText); err != nil {
			b.logger.Printf("service import: add failed admin=%d service=%q: %v", user.TelegramID, item.Path, err)
			skipped++
			continue
		}
		added++
	}
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "service_import_done", added, skipped), keyboardForUser(user))
}

func (b *Bot) handleServiceImportCallback(ctx context.Context, cb *telegram.CallbackQuery) error {
	user, err := b.userFromCallback(ctx, cb)
	if err != nil {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(LangRU, "register_failed"))
	}
	state, err := b.store.GetConversationState(ctx, user.TelegramID)
	if err != nil || state.Step != conversationStepServiceImport {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(user.Language, "service_import_expired"))
	}
	if strings.TrimPrefix(cb.Data, "serviceimport:") == "yes" {
		return b.confirmServiceImport(ctx, cb.Message.Chat.ID, user, state)
	}
	if strings.TrimPrefix(cb.Data, "serviceimport:") == "edit" {
		if state.ServiceImportReplace {
			state.Step = conversationStepServiceReplace
			state.ServiceImportEntries = nil
			if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
				return b.sendText(ctx, cb.Message.Chat.ID, tr(user.Language, "conversation_failed"))
			}
			return b.sendTextWithKeyboard(ctx, cb.Message.Chat.ID, tr(user.Language, "service_replace_ask"), serviceReplaceInputKeyboard(user.Language))
		}
	}
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	cancelKey := "service_import_cancelled"
	if state.ServiceImportReplace {
		cancelKey = "service_replace_cancelled"
	}
	return b.sendTextWithKeyboard(ctx, cb.Message.Chat.ID, tr(user.Language, cancelKey), keyboardForUser(user))
}

func formatServiceImportPreview(lang string, items []evaluatedServiceImport, replace bool) string {
	var sb strings.Builder
	headerKey := "service_import_preview"
	footerKey := "service_import_preview_footer"
	if replace {
		headerKey = "service_replace_preview"
		footerKey = "service_replace_preview_footer"
	}
	sb.WriteString(tr(lang, headerKey))
	for _, item := range items {
		line := "\n+ "
		if !item.Ready {
			line = "\n! "
		}
		line += item.Path
		if item.Draft.DurationMin > 0 {
			line += fmt.Sprintf(" — %d %s", item.Draft.DurationMin, tr(lang, "minutes_short"))
		}
		if item.Draft.PriceText != "" {
			line += " — " + item.Draft.PriceText
		}
		if item.Issue != "" {
			line += ": " + item.Issue
		}
		if utf8.RuneCountInString(sb.String()+line) > 3600 {
			sb.WriteString("\n…")
			break
		}
		sb.WriteString(line)
	}
	sb.WriteString("\n\n")
	sb.WriteString(tr(lang, footerKey))
	return sb.String()
}

func serviceImportKeyboard(lang string, canConfirm, replace bool) *telegram.ReplyMarkup {
	rows := make([][]telegram.InlineKeyboardButton, 0, 3)
	if canConfirm {
		key := "service_import_confirm"
		if replace {
			key = "service_replace_confirm"
		}
		rows = append(rows, []telegram.InlineKeyboardButton{{Text: tr(lang, key), CallbackData: "serviceimport:yes"}})
	}
	if replace {
		rows = append(rows, []telegram.InlineKeyboardButton{{Text: tr(lang, "booking_edit"), CallbackData: "serviceimport:edit"}})
	}
	rows = append(rows, []telegram.InlineKeyboardButton{{Text: tr(lang, "no"), CallbackData: "serviceimport:no"}})
	return &telegram.ReplyMarkup{InlineKeyboard: rows}
}

func serviceReplaceInputKeyboard(lang string) *telegram.ReplyMarkup {
	return menuKeyboard([][]string{{tr(lang, "button_back")}})
}
