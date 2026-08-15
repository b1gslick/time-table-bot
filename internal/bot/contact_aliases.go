package bot

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"time-table-bot/internal/store"
)

func (b *Bot) handleAdminContactAlias(ctx context.Context, chatID int64, user UserRecord, text string) (bool, error) {
	if !isAdmin(user.Role) {
		return false, nil
	}
	raw := strings.TrimSpace(text)
	if raw == "" {
		return false, nil
	}
	normalized := normalizeMatchText(raw)
	if normalized == "покажи алиасы" || normalized == "мои алиасы" || normalized == "алиасы" || normalized == "aliases" || normalized == "list aliases" {
		return true, b.sendContactAliases(ctx, chatID, user)
	}
	for _, prefix := range []string{"удали алиас ", "удалить алиас ", "delete alias "} {
		if strings.HasPrefix(strings.ToLower(raw), prefix) {
			alias := normalizeContactAlias(raw[len(prefix):])
			if alias == "" {
				return true, b.sendText(ctx, chatID, tr(user.Language, "contact_alias_delete_usage"))
			}
			if err := b.store.DeleteContactAlias(ctx, user.TelegramID, alias); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return true, b.sendText(ctx, chatID, tr(user.Language, "contact_alias_not_found", alias))
				}
				return true, b.sendText(ctx, chatID, tr(user.Language, "contact_alias_failed"))
			}
			return true, b.sendText(ctx, chatID, tr(user.Language, "contact_alias_deleted", alias))
		}
	}

	alias, contactType, contact, ok := parseContactAliasDefinition(raw)
	if !ok {
		return false, nil
	}
	updated, err := b.store.UpsertContactAlias(ctx, user.TelegramID, alias, contactType, contact)
	if err != nil {
		return true, b.sendText(ctx, chatID, tr(user.Language, "contact_alias_failed"))
	}
	message := tr(user.Language, "contact_alias_saved", alias, formatContactAlias(contactType, contact), alias)
	if updated > 0 {
		message += "\n" + tr(user.Language, "contact_alias_bookings_updated", updated)
	}
	return true, b.sendText(ctx, chatID, message)
}

func (b *Bot) sendContactAliases(ctx context.Context, chatID int64, user UserRecord) error {
	if !isAdmin(user.Role) {
		return b.sendText(ctx, chatID, tr(user.Language, "admin_only"))
	}
	aliases, err := b.store.ListContactAliases(ctx, user.TelegramID)
	if err != nil {
		return b.sendText(ctx, chatID, tr(user.Language, "contact_alias_failed"))
	}
	if len(aliases) == 0 {
		return b.sendText(ctx, chatID, tr(user.Language, "contact_alias_empty"))
	}
	var sb strings.Builder
	sb.WriteString(tr(user.Language, "contact_alias_header"))
	for _, item := range aliases {
		sb.WriteString("\n")
		sb.WriteString(item.Alias)
		sb.WriteString(" = ")
		sb.WriteString(formatContactAlias(item.ContactType, item.Contact))
	}
	return b.sendText(ctx, chatID, sb.String())
}

func parseContactAliasDefinition(text string) (alias, contactType, contact string, ok bool) {
	text = strings.TrimSpace(text)
	lower := strings.ToLower(text)
	for _, prefix := range []string{"/alias ", "запомни ", "добавь алиас ", "создай алиас ", "alias "} {
		if strings.HasPrefix(lower, prefix) {
			text = strings.TrimSpace(text[len(prefix):])
			lower = strings.ToLower(text)
			break
		}
	}
	var right string
	if index := strings.Index(lower, " это "); index >= 0 {
		alias, right = text[:index], text[index+len(" это "):]
	} else if index := strings.Index(lower, " is "); index >= 0 {
		alias, right = text[:index], text[index+len(" is "):]
	} else if index := strings.Index(text, "="); index >= 0 {
		alias, right = text[:index], text[index+1:]
	} else {
		return "", "", "", false
	}
	alias = normalizeContactAlias(alias)
	right = strings.TrimSpace(right)
	switch {
	case strings.HasPrefix(right, "@"):
		contactType, contact = "telegram", normalizeUsername(right)
	default:
		contactType, contact = "phone", normalizePhone(right)
	}
	if alias == "" || utf8.RuneCountInString(alias) > 60 || contact == "" {
		return "", "", "", false
	}
	return alias, contactType, contact, true
}

func normalizeContactAlias(value string) string {
	value = strings.Trim(value, " \t\r\n\"'«».,:;")
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func formatContactAlias(contactType, contact string) string {
	if contactType == "telegram" {
		return "@" + normalizeUsername(contact)
	}
	return strings.TrimSpace(contact)
}

func resolveContactAlias(text string, aliases []ContactAlias) (ContactAlias, bool) {
	text = normalizeMatchText(text)
	if text == "" {
		return ContactAlias{}, false
	}
	textTokens := matchTokens(text)
	bestScore := 0
	var best ContactAlias
	for _, item := range aliases {
		alias := normalizeMatchText(item.Alias)
		if alias == "" {
			continue
		}
		score := 0
		if strings.Contains(" "+text+" ", " "+alias+" ") {
			score = 100 + utf8.RuneCountInString(alias)
		} else {
			aliasTokens := matchTokens(alias)
			if len(aliasTokens) == 1 {
				for _, token := range textTokens {
					aliasLen := utf8.RuneCountInString(aliasTokens[0])
					prefix := commonPrefixRuneLen(token, aliasTokens[0])
					if aliasLen >= 3 && prefix >= 3 && prefix >= aliasLen-1 {
						score = 50 + prefix
					}
				}
			}
		}
		if score > bestScore {
			bestScore = score
			best = item
		}
	}
	return best, bestScore > 0
}
