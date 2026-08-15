package bot

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"time-table-bot/internal/nlu"
	"time-table-bot/internal/store"
	"time-table-bot/internal/telegram"
)

const (
	scheduleImportMinConfidence = 0.45
	scheduleImportMaxTextRunes  = 12000
)

var scheduleImportTimeRE = regexp.MustCompile(`(?m)(?:^|\s)(?:[01]?\d|2[0-3]):[0-5]\d(?:\s|$)`)

type evaluatedScheduleImport struct {
	Draft       ScheduleImportDraft
	Start       time.Time
	ServiceText string
	Ready       bool
	Issue       string
}

func looksLikeScheduleImport(text string) bool {
	if len(scheduleImportTimeRE.FindAllStringIndex(text, 3)) < 2 {
		return false
	}
	normalized := normalizeMatchText(text)
	for _, marker := range []string{"неделя", "авг", "пн", "вт", "ср", "чт", "пт", "сб", "вс", "week", "mon", "tue", "wed"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func (b *Bot) handleAdminScheduleImport(ctx context.Context, chatID int64, user UserRecord, text string) (bool, error) {
	parser, ok := b.adminBookingParser.(nlu.AdminScheduleImportParser)
	if !ok || !isAdmin(user.Role) || !looksLikeScheduleImport(text) {
		return false, nil
	}
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > scheduleImportMaxTextRunes {
		return true, b.sendText(ctx, chatID, tr(user.Language, "schedule_import_parse_failed"))
	}
	services, err := b.store.ListServices(ctx, user.TelegramID)
	if err != nil || len(services) == 0 {
		return true, b.sendText(ctx, chatID, tr(user.Language, "schedule_import_no_services"))
	}
	if err := b.sendText(ctx, chatID, tr(user.Language, "schedule_import_processing")); err != nil {
		return true, err
	}
	now := time.Now().In(time.Local)
	intent, err := parser.ParseAdminScheduleImport(ctx, nlu.AdminScheduleImportRequest{
		Text: text, Language: user.Language, Now: now,
		Timezone: now.Location().String(), Services: nluServices(services),
	})
	if err != nil {
		b.logger.Printf("schedule import: parser failed admin=%d: %v", user.TelegramID, err)
		return true, b.sendText(ctx, chatID, tr(user.Language, "schedule_import_parse_failed"))
	}
	if !intent.IsSchedule || intent.Confidence < scheduleImportMinConfidence || len(intent.Entries) == 0 {
		return true, b.sendText(ctx, chatID, tr(user.Language, "schedule_import_no_entries"))
	}
	entries := make([]ScheduleImportDraft, 0, len(intent.Entries))
	for _, entry := range intent.Entries {
		entries = append(entries, ScheduleImportDraft{
			Client: entry.Client, ServiceIndexes: entry.ServiceIndexes,
			ServiceQueries: entry.ServiceQueries, DurationMin: entry.DurationMin,
			StartAt: entry.StartAt, Confidence: entry.Confidence,
		})
	}
	state := ConversationState{Step: conversationStepScheduleImport, ScheduleImportEntries: entries}
	return true, b.showScheduleImportPreview(ctx, chatID, user, state)
}

func (b *Bot) showScheduleImportPreview(ctx context.Context, chatID int64, user UserRecord, state ConversationState) error {
	evaluated, err := b.evaluateScheduleImport(ctx, user, state.ScheduleImportEntries, true)
	if err != nil {
		b.logger.Printf("schedule import: evaluate failed admin=%d: %v", user.TelegramID, err)
		return b.sendText(ctx, chatID, tr(user.Language, "schedule_import_failed"))
	}
	state.Step = conversationStepScheduleImport
	state.ScheduleImportEntries = make([]ScheduleImportDraft, 0, len(evaluated))
	ready := 0
	for _, item := range evaluated {
		state.ScheduleImportEntries = append(state.ScheduleImportEntries, item.Draft)
		if item.Ready {
			ready++
		}
	}
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	return b.sendTextWithKeyboard(ctx, chatID, formatScheduleImportPreview(user.Language, evaluated), scheduleImportKeyboard(user.Language, ready > 0))
}

func (b *Bot) evaluateScheduleImport(ctx context.Context, user UserRecord, drafts []ScheduleImportDraft, checkAvailability bool) ([]evaluatedScheduleImport, error) {
	services, err := b.store.ListServices(ctx, user.TelegramID)
	if err != nil {
		return nil, err
	}
	aliases, err := b.store.ListContactAliases(ctx, user.TelegramID)
	if err != nil {
		return nil, err
	}
	now := time.Now().In(time.Local)
	out := make([]evaluatedScheduleImport, 0, len(drafts))
	for _, draft := range drafts {
		item := evaluatedScheduleImport{Draft: draft}
		item.Draft.Client = strings.TrimSpace(item.Draft.Client)
		item.Draft.ContactType, item.Draft.Contact = normalizeAdminBookingContact(item.Draft.ContactType, item.Draft.Contact)
		if item.Draft.Contact == "" {
			item.Draft.ContactType, item.Draft.Contact = normalizeAdminBookingContact("", item.Draft.Client)
		}
		if item.Draft.Contact == "" || item.Draft.ContactType == "name" {
			if alias, ok := resolveContactAlias(item.Draft.Client, aliases); ok {
				item.Draft.ContactType, item.Draft.Contact = alias.ContactType, alias.Contact
			} else {
				item.Draft.ContactType, item.Draft.Contact = "name", item.Draft.Client
				if item.Draft.Contact == "" {
					item.Issue = tr(user.Language, "schedule_import_issue_alias")
				}
			}
		}
		intent := nlu.BookingIntent{
			ServiceIndexes: item.Draft.ServiceIndexes,
			ServiceQueries: item.Draft.ServiceQueries,
			DurationMin:    item.Draft.DurationMin,
		}
		item.Draft.ServiceIndexes = resolveNaturalBookingServices(intent, strings.Join(item.Draft.ServiceQueries, " "), services)
		if len(item.Draft.ServiceIndexes) == 0 && item.Issue == "" {
			item.Issue = tr(user.Language, "schedule_import_issue_service")
		}
		if !adminBookingDurationMatches(item.Draft.ServiceIndexes, item.Draft.DurationMin, services) && item.Issue == "" {
			item.Issue = tr(user.Language, "schedule_import_issue_duration")
		}
		servicesCovered := scheduleImportServicesCoverQuery(item.Draft.ServiceQueries, item.Draft.ServiceIndexes, services)
		if item.Issue == "" && !servicesCovered {
			item.Issue = tr(user.Language, "schedule_import_issue_service")
		}
		if servicesCovered {
			item.ServiceText = scheduleImportServiceNames(item.Draft.ServiceIndexes, item.Draft.ServiceQueries, services)
		} else {
			item.ServiceText = scheduleImportUnresolvedServiceText(user.Language, item.Draft.ServiceQueries)
		}
		item.Start, err = parseAdminBookingStart(item.Draft.StartAt, now.Location())
		if (err != nil || item.Start.Before(now)) && item.Issue == "" {
			item.Issue = tr(user.Language, "schedule_import_issue_time")
		}
		if item.Draft.Confidence > 0 && item.Draft.Confidence < scheduleImportMinConfidence && item.Issue == "" {
			item.Issue = tr(user.Language, "schedule_import_issue_uncertain")
		}
		if item.Issue == "" && checkAvailability {
			if err := b.store.CanImportBooking(ctx, user.TelegramID, item.Draft.ServiceIndexes, item.Start); err != nil {
				if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalidArgument) {
					item.Issue = tr(user.Language, "schedule_import_issue_service")
				} else if errors.Is(err, store.ErrSlotUnavailable) {
					item.Issue = tr(user.Language, "schedule_import_issue_unavailable")
				} else {
					return nil, err
				}
			}
		}
		item.Ready = item.Issue == ""
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Start.IsZero() {
			return false
		}
		if out[j].Start.IsZero() {
			return true
		}
		return out[i].Start.Before(out[j].Start)
	})
	return out, nil
}

func (b *Bot) confirmScheduleImport(ctx context.Context, chatID int64, user UserRecord, state ConversationState) error {
	evaluated, err := b.evaluateScheduleImport(ctx, user, state.ScheduleImportEntries, false)
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "schedule_import_failed"))
	}
	created, skipped := 0, 0
	for _, item := range evaluated {
		if !item.Ready {
			skipped++
			continue
		}
		result, err := b.store.AddImportedBooking(ctx, user.TelegramID, item.Draft.ContactType, item.Draft.Contact, item.Draft.ServiceIndexes, item.Start)
		if err != nil {
			b.logger.Printf("schedule import: create failed admin=%d client=%q start=%s: %v", user.TelegramID, item.Draft.Client, item.Start.Format(time.RFC3339), err)
			skipped++
			continue
		}
		created++
		b.notifyBookingChange(ctx, "created", result, chatID)
	}
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "schedule_import_done", created, skipped), keyboardForUser(user))
}

func (b *Bot) handleScheduleImportCallback(ctx context.Context, cb *telegram.CallbackQuery) error {
	user, err := b.userFromCallback(ctx, cb)
	if err != nil {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(LangRU, "register_failed"))
	}
	state, err := b.store.GetConversationState(ctx, user.TelegramID)
	if err != nil || state.Step != conversationStepScheduleImport {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(user.Language, "schedule_import_expired"))
	}
	switch strings.TrimPrefix(cb.Data, "scheduleimport:") {
	case "yes":
		return b.confirmScheduleImport(ctx, cb.Message.Chat.ID, user, state)
	case "retry":
		return b.showScheduleImportPreview(ctx, cb.Message.Chat.ID, user, state)
	default:
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendTextWithKeyboard(ctx, cb.Message.Chat.ID, tr(user.Language, "schedule_import_cancelled"), keyboardForUser(user))
	}
}

func formatScheduleImportPreview(lang string, items []evaluatedScheduleImport) string {
	var sb strings.Builder
	sb.WriteString(tr(lang, "schedule_import_preview"))
	for _, item := range items {
		sb.WriteString("\n")
		if item.Ready {
			sb.WriteString("+ ")
		} else {
			sb.WriteString("! ")
		}
		if !item.Start.IsZero() {
			sb.WriteString(item.Start.Format("02.01 15:04"))
		} else {
			sb.WriteString("??.?? ??:??")
		}
		sb.WriteString(" - ")
		client := item.Draft.Client
		if client == "" {
			client = tr(lang, "schedule_import_unknown_client")
		}
		sb.WriteString(client)
		if item.Draft.Contact != "" {
			if item.Draft.ContactType == "name" {
				sb.WriteString(" ")
				sb.WriteString(tr(lang, "schedule_import_name_pending"))
			} else {
				sb.WriteString(" (")
				sb.WriteString(formatContactAlias(item.Draft.ContactType, item.Draft.Contact))
				sb.WriteString(")")
			}
		}
		if item.ServiceText != "" {
			sb.WriteString(" - ")
			sb.WriteString(item.ServiceText)
		}
		if item.Issue != "" {
			sb.WriteString(": ")
			sb.WriteString(item.Issue)
		}
	}
	sb.WriteString("\n\n")
	sb.WriteString(tr(lang, "schedule_import_preview_footer"))
	return sb.String()
}

func scheduleImportServiceNames(indexes []int, queries []string, services []ServiceView) string {
	names := make([]string, 0, len(indexes))
	for _, index := range indexes {
		if index > 0 && index <= len(services) {
			service := services[index-1]
			name := service.Name
			if service.Category != "" {
				name = service.Category + " — " + name
			}
			names = append(names, name)
		}
	}
	if len(names) > 0 {
		return strings.Join(names, ", ")
	}
	return strings.Join(queries, ", ")
}

func scheduleImportUnresolvedServiceText(lang string, queries []string) string {
	raw := strings.TrimSpace(strings.Join(queries, ", "))
	normalized := normalizeMatchText(raw)
	if normalized == "bock" || normalized == "воск" {
		return tr(lang, "schedule_import_wax_zone_missing")
	}
	return strings.NewReplacer("BOCK", "воск", "bock", "воск").Replace(raw)
}

func scheduleImportServicesCoverQuery(queries []string, indexes []int, services []ServiceView) bool {
	query := normalizeMatchText(strings.Join(queries, " "))
	query = strings.NewReplacer("bock", "воск", "воск.", "воск").Replace(query)
	selected := make([]string, 0, len(indexes))
	for _, index := range indexes {
		if index > 0 && index <= len(services) {
			service := services[index-1]
			selected = append(selected, normalizeMatchText(strings.Join([]string{service.Category, service.Subcategory, service.Name}, " ")))
		}
	}
	joined := strings.Join(selected, " ")
	if strings.Contains(query, "электро") && !strings.Contains(joined, "электро") {
		return false
	}
	if strings.Contains(query, "воск") {
		if !strings.Contains(joined, "воск") {
			return false
		}
		hasWaxZone := false
		for _, zone := range []string{"бикини", "лицо", "ног", "рук", "ягод", "усы"} {
			if strings.Contains(query, zone) {
				hasWaxZone = true
				break
			}
		}
		if !hasWaxZone {
			return false
		}
	}
	if strings.Contains(query, "бикини") && !strings.Contains(joined, "бикини") {
		return false
	}
	if strings.Contains(query, "эндо") && !strings.Contains(joined, "эндо") {
		return false
	}
	if strings.Contains(query, "подмыш") && !strings.Contains(query, "электро") && !strings.Contains(joined, "подмыш") {
		return false
	}
	return true
}

func scheduleImportKeyboard(lang string, canConfirm bool) *telegram.ReplyMarkup {
	rows := make([][]telegram.InlineKeyboardButton, 0, 2)
	if canConfirm {
		rows = append(rows, []telegram.InlineKeyboardButton{{Text: tr(lang, "schedule_import_confirm"), CallbackData: "scheduleimport:yes"}})
	}
	rows = append(rows, []telegram.InlineKeyboardButton{
		{Text: tr(lang, "schedule_import_retry"), CallbackData: "scheduleimport:retry"},
		{Text: tr(lang, "no"), CallbackData: "scheduleimport:no"},
	})
	return &telegram.ReplyMarkup{InlineKeyboard: rows}
}
