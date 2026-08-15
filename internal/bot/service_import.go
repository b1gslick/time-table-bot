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
	normalized := normalizeMatchText(text)
	for _, marker := range []string{
		"добавь услуг", "добавить услуг", "создай услуг", "создать услуг", "новые услуги",
		"импорт услуг", "загрузи услуг", "добавь в прайс", "обнови прайс", "вот услуги",
		"add service", "import service", "new service", "update price list", "price list",
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

func (b *Bot) handleAdminNaturalServiceImport(ctx context.Context, chatID int64, user UserRecord, text string) (bool, error) {
	parser, ok := b.adminBookingParser.(nlu.AdminServiceImportParser)
	if !ok || !isAdmin(user.Role) || !looksLikeAdminServiceImport(text) {
		return false, nil
	}
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > serviceImportMaxTextRunes {
		return true, b.sendText(ctx, chatID, tr(user.Language, "service_import_parse_failed"))
	}
	services, err := b.store.ListServices(ctx, user.TelegramID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return true, b.sendText(ctx, chatID, tr(user.Language, "service_import_failed"))
	}
	if err := b.sendText(ctx, chatID, tr(user.Language, "service_import_processing")); err != nil {
		return true, err
	}
	intent, err := parser.ParseAdminServiceImport(ctx, nlu.AdminServiceImportRequest{
		Text: text, Language: user.Language, ExistingServices: nluServices(services),
	})
	if err != nil {
		b.logger.Printf("service import: parser failed admin=%d: %v", user.TelegramID, err)
		return true, b.sendText(ctx, chatID, tr(user.Language, "service_import_parse_failed"))
	}
	if !intent.IsServiceCatalog || intent.Confidence < serviceImportMinConfidence || len(intent.Entries) == 0 {
		return true, b.sendText(ctx, chatID, tr(user.Language, "service_import_no_entries"))
	}
	entries := make([]ServiceImportDraft, 0, len(intent.Entries))
	for _, entry := range intent.Entries {
		entries = append(entries, ServiceImportDraft{
			Category: entry.Category, Subcategory: entry.Subcategory, Name: entry.Name,
			DurationMin: entry.DurationMin, PriceText: entry.PriceText, Confidence: entry.Confidence,
		})
	}
	state := ConversationState{Step: conversationStepServiceImport, ServiceImportEntries: entries}
	return true, b.showServiceImportPreview(ctx, chatID, user, state)
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
	return b.sendTextWithKeyboard(ctx, chatID, formatServiceImportPreview(user.Language, items), serviceImportKeyboard(user.Language, ready > 0))
}

func evaluateServiceImport(lang string, drafts []ServiceImportDraft) []evaluatedServiceImport {
	out := make([]evaluatedServiceImport, 0, len(drafts))
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
			item.Ready = true
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
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	return b.sendTextWithKeyboard(ctx, cb.Message.Chat.ID, tr(user.Language, "service_import_cancelled"), keyboardForUser(user))
}

func formatServiceImportPreview(lang string, items []evaluatedServiceImport) string {
	var sb strings.Builder
	sb.WriteString(tr(lang, "service_import_preview"))
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
	sb.WriteString(tr(lang, "service_import_preview_footer"))
	return sb.String()
}

func serviceImportKeyboard(lang string, canConfirm bool) *telegram.ReplyMarkup {
	rows := make([][]telegram.InlineKeyboardButton, 0, 2)
	if canConfirm {
		rows = append(rows, []telegram.InlineKeyboardButton{{Text: tr(lang, "service_import_confirm"), CallbackData: "serviceimport:yes"}})
	}
	rows = append(rows, []telegram.InlineKeyboardButton{{Text: tr(lang, "no"), CallbackData: "serviceimport:no"}})
	return &telegram.ReplyMarkup{InlineKeyboard: rows}
}
