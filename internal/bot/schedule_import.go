package bot

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
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
	state = ConversationState{Step: conversationStepScheduleImport}
	state.ScheduleImportEntries = make([]ScheduleImportDraft, 0, len(evaluated))
	for _, item := range evaluated {
		state.ScheduleImportEntries = append(state.ScheduleImportEntries, item.Draft)
	}
	return b.showScheduleImportCurrent(ctx, chatID, user, state)
}

func (b *Bot) showScheduleImportCurrent(ctx context.Context, chatID int64, user UserRecord, state ConversationState) error {
	if state.ScheduleImportIndex < 0 {
		state.ScheduleImportIndex = 0
	}
	if state.ScheduleImportIndex >= len(state.ScheduleImportEntries) {
		return b.finishScheduleImport(ctx, chatID, user, state)
	}
	items, err := b.evaluateScheduleImport(ctx, user, []ScheduleImportDraft{state.ScheduleImportEntries[state.ScheduleImportIndex]}, true)
	if err != nil || len(items) != 1 {
		b.logger.Printf("schedule import: current evaluation failed admin=%d: %v", user.TelegramID, err)
		return b.sendText(ctx, chatID, tr(user.Language, "schedule_import_failed"))
	}
	item := items[0]
	state.Step = conversationStepScheduleImport
	state.ScheduleImportEdit = ""
	state.ScheduleImportEntries[state.ScheduleImportIndex] = item.Draft
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	text := formatScheduleImportCurrent(user.Language, item, state.ScheduleImportIndex, len(state.ScheduleImportEntries), state.ScheduleImportCreated, state.ScheduleImportSkipped)
	return b.sendTextWithKeyboard(ctx, chatID, text, scheduleImportItemKeyboard(user.Language, state.ScheduleImportIndex, item.Ready))
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

func (b *Bot) handleScheduleImportCallback(ctx context.Context, cb *telegram.CallbackQuery) error {
	user, err := b.userFromCallback(ctx, cb)
	if err != nil {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(LangRU, "register_failed"))
	}
	if !isAdmin(user.Role) {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(user.Language, "admin_only"))
	}
	state, err := b.store.GetConversationState(ctx, user.TelegramID)
	if err != nil || (state.Step != conversationStepScheduleImport && state.Step != conversationStepScheduleEdit) {
		return b.sendText(ctx, cb.Message.Chat.ID, tr(user.Language, "schedule_import_expired"))
	}
	parts := strings.Split(strings.TrimPrefix(cb.Data, "scheduleimport:"), ":")
	if len(parts) < 2 {
		return b.showScheduleImportCurrent(ctx, cb.Message.Chat.ID, user, state)
	}
	index, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil || index != state.ScheduleImportIndex || index < 0 || index >= len(state.ScheduleImportEntries) {
		return b.showScheduleImportCurrent(ctx, cb.Message.Chat.ID, user, state)
	}
	switch parts[0] {
	case "save":
		return b.saveScheduleImportCurrent(ctx, cb.Message.Chat.ID, user, state)
	case "skip":
		state.ScheduleImportSkipped++
		state.ScheduleImportIndex++
		return b.advanceScheduleImport(ctx, cb.Message.Chat.ID, user, state)
	case "edit":
		state.Step = conversationStepScheduleEdit
		state.ScheduleImportEdit = ""
		if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
			return b.sendText(ctx, cb.Message.Chat.ID, tr(user.Language, "conversation_failed"))
		}
		return b.sendTextWithKeyboard(ctx, cb.Message.Chat.ID, tr(user.Language, "schedule_import_edit_choose"), scheduleImportEditKeyboard(user.Language, index))
	case "field":
		if len(parts) != 3 {
			return b.showScheduleImportCurrent(ctx, cb.Message.Chat.ID, user, state)
		}
		return b.beginScheduleImportFieldEdit(ctx, cb.Message.Chat.ID, user, state, parts[1])
	case "back":
		return b.showScheduleImportCurrent(ctx, cb.Message.Chat.ID, user, state)
	case "stop":
		remaining := len(state.ScheduleImportEntries) - state.ScheduleImportIndex
		_ = b.store.ClearConversationState(ctx, user.TelegramID)
		return b.sendTextWithKeyboard(ctx, cb.Message.Chat.ID, tr(user.Language, "schedule_import_stopped", state.ScheduleImportCreated, state.ScheduleImportSkipped, remaining), keyboardForUser(user))
	default:
		return b.showScheduleImportCurrent(ctx, cb.Message.Chat.ID, user, state)
	}
}

func (b *Bot) saveScheduleImportCurrent(ctx context.Context, chatID int64, user UserRecord, state ConversationState) error {
	items, err := b.evaluateScheduleImport(ctx, user, []ScheduleImportDraft{state.ScheduleImportEntries[state.ScheduleImportIndex]}, true)
	if err != nil || len(items) != 1 {
		return b.sendText(ctx, chatID, tr(user.Language, "schedule_import_failed"))
	}
	item := items[0]
	state.ScheduleImportEntries[state.ScheduleImportIndex] = item.Draft
	if !item.Ready {
		return b.showScheduleImportCurrent(ctx, chatID, user, state)
	}
	result, err := b.store.AddImportedBooking(ctx, user.TelegramID, item.Draft.ContactType, item.Draft.Contact, item.Draft.ServiceIndexes, item.Start)
	if err != nil {
		b.logger.Printf("schedule import: create failed admin=%d client=%q start=%s: %v", user.TelegramID, item.Draft.Client, item.Start.Format(time.RFC3339), err)
		return b.showScheduleImportCurrent(ctx, chatID, user, state)
	}
	b.notifyBookingChange(ctx, "created", result, chatID)
	state.ScheduleImportCreated++
	state.ScheduleImportIndex++
	return b.advanceScheduleImport(ctx, chatID, user, state)
}

func (b *Bot) advanceScheduleImport(ctx context.Context, chatID int64, user UserRecord, state ConversationState) error {
	state.Step = conversationStepScheduleImport
	state.ScheduleImportEdit = ""
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	return b.showScheduleImportCurrent(ctx, chatID, user, state)
}

func (b *Bot) finishScheduleImport(ctx context.Context, chatID int64, user UserRecord, state ConversationState) error {
	_ = b.store.ClearConversationState(ctx, user.TelegramID)
	return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "schedule_import_done", state.ScheduleImportCreated, state.ScheduleImportSkipped), keyboardForUser(user))
}

func (b *Bot) beginScheduleImportFieldEdit(ctx context.Context, chatID int64, user UserRecord, state ConversationState, field string) error {
	if field != "client" && field != "service" && field != "time" {
		return b.showScheduleImportCurrent(ctx, chatID, user, state)
	}
	state.Step = conversationStepScheduleEdit
	state.ScheduleImportEdit = field
	if err := b.store.SetConversationState(ctx, user.TelegramID, state); err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "conversation_failed"))
	}
	prompt := tr(user.Language, "schedule_import_edit_"+field)
	if field == "service" {
		services, err := b.store.ListServices(ctx, user.TelegramID)
		if err != nil {
			return b.sendText(ctx, chatID, tr(user.Language, "services_list_failed"))
		}
		prompt += "\n\n" + formatScheduleImportServiceChoices(user.Language, services)
	}
	return b.sendTextWithKeyboard(ctx, chatID, prompt, scheduleImportEditBackKeyboard(user.Language, state.ScheduleImportIndex))
}

func (b *Bot) conversationScheduleImportEdit(ctx context.Context, chatID int64, user UserRecord, state ConversationState, text string) error {
	if state.ScheduleImportIndex < 0 || state.ScheduleImportIndex >= len(state.ScheduleImportEntries) {
		return b.finishScheduleImport(ctx, chatID, user, state)
	}
	draft := state.ScheduleImportEntries[state.ScheduleImportIndex]
	switch state.ScheduleImportEdit {
	case "client":
		client, contactType, contact, ok := parseScheduleImportClientEdit(text, draft.Client)
		if !ok {
			return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "schedule_import_edit_client"), scheduleImportEditBackKeyboard(user.Language, state.ScheduleImportIndex))
		}
		draft.Client, draft.ContactType, draft.Contact = client, contactType, contact
	case "service":
		services, err := b.store.ListServices(ctx, user.TelegramID)
		if err != nil {
			return b.sendText(ctx, chatID, tr(user.Language, "services_list_failed"))
		}
		indexes := resolveScheduleImportEditServices(text, services)
		if len(indexes) == 0 {
			return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "schedule_import_service_bad")+"\n\n"+formatScheduleImportServiceChoices(user.Language, services), scheduleImportEditBackKeyboard(user.Language, state.ScheduleImportIndex))
		}
		draft.ServiceIndexes = indexes
		draft.ServiceQueries = scheduleImportQueriesForIndexes(indexes, services)
		draft.DurationMin = scheduleImportDurationForIndexes(indexes, services)
	case "time":
		start, err := parseScheduleImportEditDateTime(text, time.Now().In(time.Local))
		if err != nil {
			return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "schedule_import_edit_time"), scheduleImportEditBackKeyboard(user.Language, state.ScheduleImportIndex))
		}
		draft.StartAt = start.Format(time.RFC3339)
	default:
		return b.sendTextWithKeyboard(ctx, chatID, tr(user.Language, "schedule_import_edit_choose"), scheduleImportEditKeyboard(user.Language, state.ScheduleImportIndex))
	}
	draft.Confidence = 1
	state.ScheduleImportEntries[state.ScheduleImportIndex] = draft
	state.Step = conversationStepScheduleImport
	state.ScheduleImportEdit = ""
	return b.showScheduleImportCurrent(ctx, chatID, user, state)
}

func parseScheduleImportClientEdit(text, existingClient string) (client, contactType, contact string, ok bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", "", "", false
	}
	fields := strings.Fields(text)
	for i, field := range fields {
		if strings.HasPrefix(field, "@") {
			contact = normalizeUsername(field)
			if contact == "" {
				return "", "", "", false
			}
			client = strings.TrimSpace(strings.Join(append(append([]string{}, fields[:i]...), fields[i+1:]...), " "))
			if client == "" {
				client = strings.TrimSpace(existingClient)
			}
			if client == "" {
				client = "@" + contact
			}
			return client, "telegram", contact, true
		}
	}
	if phone := normalizePhone(text); phone != "" {
		client = strings.TrimSpace(existingClient)
		if client == "" {
			client = phone
		}
		return client, "phone", phone, true
	}
	return text, "name", text, true
}

func resolveScheduleImportEditServices(text string, services []ServiceView) []int {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
	})
	numeric := len(fields) > 0
	indexes := make([]int, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSuffix(strings.TrimSpace(field), ".")
		index, err := strconv.Atoi(field)
		if err != nil {
			numeric = false
			break
		}
		if index <= 0 || index > len(services) || intInSlice(indexes, index) {
			if index <= 0 || index > len(services) {
				return nil
			}
			continue
		}
		indexes = append(indexes, index)
	}
	if numeric {
		return indexes
	}
	normalized := strings.NewReplacer(" + ", ",", " и ", ",", " and ", ",").Replace(text)
	queries := strings.FieldsFunc(normalized, func(r rune) bool { return r == ',' || r == ';' || r == '\n' })
	intent := nlu.BookingIntent{ServiceQueries: queries}
	return resolveNaturalBookingServices(intent, text, services)
}

func scheduleImportQueriesForIndexes(indexes []int, services []ServiceView) []string {
	queries := make([]string, 0, len(indexes))
	for _, index := range indexes {
		if index <= 0 || index > len(services) {
			continue
		}
		service := services[index-1]
		queries = append(queries, strings.TrimSpace(strings.Join([]string{service.Category, service.Subcategory, service.Name}, " ")))
	}
	return queries
}

func scheduleImportDurationForIndexes(indexes []int, services []ServiceView) int {
	total := 0
	for _, index := range indexes {
		if index > 0 && index <= len(services) {
			total += services[index-1].DurationMin
		}
	}
	return total
}

func formatScheduleImportServiceChoices(lang string, services []ServiceView) string {
	var sb strings.Builder
	for index, service := range services {
		parts := make([]string, 0, 3)
		for _, part := range []string{service.Category, service.Subcategory, service.Name} {
			if part = strings.TrimSpace(part); part != "" {
				parts = append(parts, part)
			}
		}
		line := fmt.Sprintf("%d. %s — %d %s\n", index+1, strings.Join(parts, " / "), service.DurationMin, tr(lang, "minutes_short"))
		if len([]rune(sb.String()+line)) > 3000 {
			sb.WriteString("…")
			break
		}
		sb.WriteString(line)
	}
	return strings.TrimSpace(sb.String())
}

func parseScheduleImportEditDateTime(text string, now time.Time) (time.Time, error) {
	text = strings.TrimSpace(text)
	if parsed, err := parseAdminBookingStart(text, now.Location()); err == nil {
		return parsed, nil
	}
	text = strings.NewReplacer(" в ", " ", " at ", " ").Replace(" " + text + " ")
	text = strings.TrimSpace(text)
	fields := strings.Fields(text)
	if len(fields) != 2 {
		return time.Time{}, fmt.Errorf("date and time are required")
	}
	var date time.Time
	switch normalizeMatchText(fields[0]) {
	case "сегодня", "today":
		date = dateOnly(now)
	case "завтра", "tomorrow":
		date = dateOnly(now).AddDate(0, 0, 1)
	case "послезавтра":
		date = dateOnly(now).AddDate(0, 0, 2)
	default:
		parsed, err := parseUserDate(fields[0], now)
		if err != nil {
			return time.Time{}, err
		}
		date = parsed
	}
	clock, err := time.ParseInLocation("15:04", fields[1], now.Location())
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(date.Year(), date.Month(), date.Day(), clock.Hour(), clock.Minute(), 0, 0, now.Location()), nil
}

func formatScheduleImportCurrent(lang string, item evaluatedScheduleImport, index, total, created, skipped int) string {
	var sb strings.Builder
	sb.WriteString(tr(lang, "schedule_import_item_header", index+1, total))
	sb.WriteString("\n")
	if !item.Start.IsZero() {
		sb.WriteString(tr(lang, "schedule_import_item_time", item.Start.Format("02.01.2006 15:04")))
	} else {
		sb.WriteString(tr(lang, "schedule_import_item_time", "??.??.???? ??:??"))
	}
	client := item.Draft.Client
	if client == "" {
		client = tr(lang, "schedule_import_unknown_client")
	}
	if item.Draft.Contact != "" {
		if item.Draft.ContactType == "name" {
			client += " " + tr(lang, "schedule_import_name_pending")
		} else {
			client += " (" + formatContactAlias(item.Draft.ContactType, item.Draft.Contact) + ")"
		}
	}
	sb.WriteString("\n")
	sb.WriteString(tr(lang, "schedule_import_item_client", client))
	serviceText := item.ServiceText
	if serviceText == "" {
		serviceText = tr(lang, "schedule_import_issue_service")
	}
	sb.WriteString("\n")
	sb.WriteString(tr(lang, "schedule_import_item_service", serviceText))
	if item.Ready {
		sb.WriteString("\n\n")
		sb.WriteString(tr(lang, "schedule_import_item_ready"))
	} else {
		sb.WriteString("\n\n")
		sb.WriteString(tr(lang, "schedule_import_item_issue", item.Issue))
	}
	sb.WriteString("\n")
	sb.WriteString(tr(lang, "schedule_import_progress", created, skipped))
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

func scheduleImportItemKeyboard(lang string, index int, canSave bool) *telegram.ReplyMarkup {
	rows := make([][]telegram.InlineKeyboardButton, 0, 3)
	first := make([]telegram.InlineKeyboardButton, 0, 2)
	if canSave {
		first = append(first, telegram.InlineKeyboardButton{Text: tr(lang, "schedule_import_save"), CallbackData: fmt.Sprintf("scheduleimport:save:%d", index)})
	}
	first = append(first, telegram.InlineKeyboardButton{Text: tr(lang, "booking_edit"), CallbackData: fmt.Sprintf("scheduleimport:edit:%d", index)})
	rows = append(rows, first)
	rows = append(rows, []telegram.InlineKeyboardButton{{Text: tr(lang, "schedule_import_skip"), CallbackData: fmt.Sprintf("scheduleimport:skip:%d", index)}})
	rows = append(rows, []telegram.InlineKeyboardButton{{Text: tr(lang, "schedule_import_stop"), CallbackData: fmt.Sprintf("scheduleimport:stop:%d", index)}})
	return &telegram.ReplyMarkup{InlineKeyboard: rows}
}

func scheduleImportEditKeyboard(lang string, index int) *telegram.ReplyMarkup {
	return &telegram.ReplyMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{
			{Text: tr(lang, "schedule_import_edit_client_btn"), CallbackData: fmt.Sprintf("scheduleimport:field:client:%d", index)},
			{Text: tr(lang, "schedule_import_edit_service_btn"), CallbackData: fmt.Sprintf("scheduleimport:field:service:%d", index)},
		},
		{{Text: tr(lang, "schedule_import_edit_time_btn"), CallbackData: fmt.Sprintf("scheduleimport:field:time:%d", index)}},
		{{Text: tr(lang, "button_back"), CallbackData: fmt.Sprintf("scheduleimport:back:%d", index)}},
	}}
}

func scheduleImportEditBackKeyboard(lang string, index int) *telegram.ReplyMarkup {
	rows := [][]telegram.InlineKeyboardButton{{{Text: tr(lang, "button_back"), CallbackData: fmt.Sprintf("scheduleimport:edit:%d", index)}}}
	return &telegram.ReplyMarkup{InlineKeyboard: rows}
}
