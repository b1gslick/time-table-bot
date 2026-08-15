package nlu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func (p *QwenParser) ParseAdminFinanceIntent(ctx context.Context, req AdminFinanceIntentRequest) (AdminFinanceIntent, error) {
	if p == nil {
		return AdminFinanceIntent{}, fmt.Errorf("qwen parser is nil")
	}
	if strings.TrimSpace(req.Text) == "" {
		return AdminFinanceIntent{}, fmt.Errorf("text is required")
	}
	payload := qwenChatRequest{
		Model: p.model,
		Messages: []qwenMessage{
			{Role: "system", Content: qwenFinanceSystemPrompt()},
			{Role: "user", Content: qwenFinanceUserPrompt(req)},
		},
		Temperature:         ptrFloat64(0),
		EnableThinking:      ptrBool(false),
		MaxCompletionTokens: 2500,
		ResponseFormat:      &qwenResponseFormat{Type: "json_object"},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return AdminFinanceIntent{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return AdminFinanceIntent{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return AdminFinanceIntent{}, fmt.Errorf("qwen finance request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return AdminFinanceIntent{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return AdminFinanceIntent{}, fmt.Errorf("qwen finance status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out qwenChatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return AdminFinanceIntent{}, fmt.Errorf("decode qwen finance response: %w", err)
	}
	if len(out.Choices) == 0 {
		return AdminFinanceIntent{}, fmt.Errorf("qwen finance response has no choices")
	}
	var intent AdminFinanceIntent
	if err := json.Unmarshal([]byte(trimJSONFence(out.Choices[0].Message.Content)), &intent); err != nil {
		return AdminFinanceIntent{}, fmt.Errorf("decode finance intent: %w", err)
	}
	if len(intent.Entries) > 30 {
		intent.Entries = intent.Entries[:30]
	}
	for i := range intent.Entries {
		entry := &intent.Entries[i]
		entry.Kind = strings.ToLower(strings.TrimSpace(entry.Kind))
		entry.Category = strings.TrimSpace(entry.Category)
		entry.Currency = strings.ToUpper(strings.TrimSpace(entry.Currency))
		entry.OccurredAt = strings.TrimSpace(entry.OccurredAt)
		entry.Description = strings.TrimSpace(entry.Description)
		if entry.AmountCents < 0 {
			entry.AmountCents = 0
		}
		entry.Confidence = clampConfidence(entry.Confidence)
	}
	intent.Confidence = clampConfidence(intent.Confidence)
	return intent, nil
}

func qwenFinanceSystemPrompt() string {
	return strings.Join([]string{
		"You extract income and expense entries for a salon administrator's private financial ledger.",
		"Return only JSON. Never create, edit, or delete data.",
		"Use this schema:",
		`{"is_finance":true,"entries":[{"kind":"income|expense","category":"rent|supplies|services|other","amount_cents":1250,"currency":"EUR","occurred_at":"RFC3339 datetime","description":"short source description","confidence":0.0}],"confidence":0.0}`,
		"amount_cents is the exact monetary amount multiplied by 100. Never confuse receipt item quantities, tax IDs, dates, or card numbers with the total.",
		"For receipts, use the final paid total and classify it as expense. Prefer labels such as TOTAL, ИТОГО, К ОПЛАТЕ, or paid amount.",
		"Split a message into multiple entries only when it explicitly contains separate financial operations.",
		"Use the forced kind when supplied. Otherwise infer income or expense conservatively.",
		"Use EUR for euro symbols and words. Do not convert currencies. Leave currency empty when it is not stated and cannot be inferred.",
		"Use the current date when no operation date is stated. Preserve an explicitly stated date in the supplied timezone.",
		"Set is_finance=false for appointment booking requests, service catalog updates, schedules, and financial report requests.",
	}, "\n")
}

func qwenFinanceUserPrompt(req AdminFinanceIntentRequest) string {
	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	timezone := strings.TrimSpace(req.Timezone)
	if timezone == "" {
		timezone = now.Location().String()
	}
	return fmt.Sprintf(
		"Current datetime: %s\nTimezone: %s\nLanguage: %s\nInput source: %s\nForced kind: %s\n\nAdministrator input:\n%s",
		now.Format(time.RFC3339), timezone, req.Language, req.Source, req.ForcedKind, req.Text,
	)
}
