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

func (p *QwenParser) ParseAdminBookingIntent(ctx context.Context, req AdminBookingIntentRequest) (AdminBookingIntent, error) {
	if p == nil {
		return AdminBookingIntent{}, fmt.Errorf("qwen parser is nil")
	}
	if strings.TrimSpace(req.Text) == "" {
		return AdminBookingIntent{}, fmt.Errorf("text is required")
	}
	payload := qwenChatRequest{
		Model: p.model,
		Messages: []qwenMessage{
			{Role: "system", Content: qwenAdminBookingSystemPrompt()},
			{Role: "user", Content: qwenAdminBookingUserPrompt(req)},
		},
		Temperature:    ptrFloat64(0),
		EnableThinking: ptrBool(false),
		ResponseFormat: &qwenResponseFormat{Type: "json_object"},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return AdminBookingIntent{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return AdminBookingIntent{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return AdminBookingIntent{}, fmt.Errorf("qwen admin booking request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return AdminBookingIntent{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return AdminBookingIntent{}, fmt.Errorf("qwen admin booking status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out qwenChatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return AdminBookingIntent{}, fmt.Errorf("decode qwen admin booking response: %w", err)
	}
	if len(out.Choices) == 0 {
		return AdminBookingIntent{}, fmt.Errorf("qwen admin booking response has no choices")
	}
	content := trimJSONFence(out.Choices[0].Message.Content)
	var intent AdminBookingIntent
	if err := json.Unmarshal([]byte(content), &intent); err != nil {
		return AdminBookingIntent{}, fmt.Errorf("decode admin booking intent: %w", err)
	}
	intent.ContactType = normalizeContactType(intent.ContactType)
	intent.Contact = strings.TrimSpace(intent.Contact)
	intent.ServiceQueries = compactStrings(intent.ServiceQueries)
	intent.ServiceIndexes = uniquePositiveInts(intent.ServiceIndexes)
	if intent.DurationMin < 0 {
		intent.DurationMin = 0
	}
	if intent.Confidence < 0 {
		intent.Confidence = 0
	}
	if intent.Confidence > 1 {
		intent.Confidence = 1
	}
	return intent, nil
}

func qwenAdminBookingSystemPrompt() string {
	return strings.Join([]string{
		"You extract a create-booking request written by a salon administrator.",
		"Return only JSON. Never create, edit, or delete anything.",
		"Use this schema:",
		`{"is_create_booking":true,"contact_type":"telegram|phone|unknown","contact":"@username or phone","service_indexes":[1],"service_queries":["service name"],"duration_min":90,"start_at":"RFC3339 datetime","confidence":0.0}`,
		"Set is_create_booking=true only for an explicit request to book a client, not for listing, cancelling, or moving bookings.",
		"Resolve relative dates from the supplied current datetime and timezone.",
		"Keep start_at empty unless both date and time are clear. When present, include the timezone offset.",
		"Use contact_type=telegram for @username and contact_type=phone for a phone number. Otherwise use unknown.",
		"Use only service indexes from the supplied list. If unsure, leave them empty and fill service_queries.",
	}, "\n")
}

func qwenAdminBookingUserPrompt(req AdminBookingIntentRequest) string {
	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	timezone := strings.TrimSpace(req.Timezone)
	if timezone == "" {
		timezone = now.Location().String()
	}
	var sb strings.Builder
	sb.WriteString("Current datetime: ")
	sb.WriteString(now.Format(time.RFC3339))
	sb.WriteString("\nTimezone: ")
	sb.WriteString(timezone)
	sb.WriteString("\nLanguage: ")
	sb.WriteString(req.Language)
	sb.WriteString("\nServices:\n")
	for _, svc := range req.Services {
		if svc.Index <= 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("%d. %s", svc.Index, svc.Name))
		if svc.Category != "" {
			sb.WriteString(" [")
			sb.WriteString(svc.Category)
			if svc.Subcategory != "" {
				sb.WriteString(" > ")
				sb.WriteString(svc.Subcategory)
			}
			sb.WriteString("]")
		}
		if svc.DurationMin > 0 {
			sb.WriteString(fmt.Sprintf(" (%d min)", svc.DurationMin))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\nAdministrator message:\n")
	sb.WriteString(req.Text)
	return sb.String()
}

func normalizeContactType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "telegram", "phone":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "unknown"
	}
}
