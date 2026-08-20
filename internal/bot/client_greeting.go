package bot

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"time-table-bot/internal/nlu"
)

const (
	clientGreetingTimeout  = 12 * time.Second
	clientGreetingMaxRunes = 900
)

func (b *Bot) clientStartGreeting(ctx context.Context, user UserRecord, clientName string, services []ServiceView) string {
	fallback := tr(user.Language, "start_client", clientName)
	if b.greetingGenerator == nil {
		return fallback
	}
	masterDescription, err := b.store.MasterIntro(ctx, user.TelegramID)
	if err != nil {
		b.logger.Printf("client greeting: master profile failed user=%d: %v", user.TelegramID, err)
	}
	generationCtx, cancel := context.WithTimeout(ctx, clientGreetingTimeout)
	defer cancel()
	generated, err := b.greetingGenerator.GenerateClientGreeting(generationCtx, nlu.ClientGreetingRequest{
		ClientName:        clientName,
		Language:          user.Language,
		MasterDescription: masterDescription,
		Services:          nluServices(services),
	})
	generated = strings.TrimSpace(generated)
	if err != nil || generated == "" || utf8.RuneCountInString(generated) > clientGreetingMaxRunes {
		if err != nil {
			b.logger.Printf("client greeting: generation failed user=%d: %v", user.TelegramID, err)
		} else {
			b.logger.Printf("client greeting: invalid response user=%d", user.TelegramID)
		}
		return fallback
	}
	return generated
}
