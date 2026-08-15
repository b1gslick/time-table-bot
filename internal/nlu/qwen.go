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

const (
	DefaultQwenModel   = "qwen3.7-plus"
	DefaultQwenBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
)

type QwenConfig struct {
	APIKey  string
	BaseURL string
	Model   string
	Timeout time.Duration
}

type QwenParser struct {
	apiKey  string
	baseURL string
	model   string
	client  *http.Client
}

func NewQwenParser(cfg QwenConfig) (*QwenParser, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("qwen api key is required")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultQwenBaseURL
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = DefaultQwenModel
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 12 * time.Second
	}
	return &QwenParser{
		apiKey:  apiKey,
		baseURL: baseURL,
		model:   model,
		client:  &http.Client{Timeout: timeout},
	}, nil
}

func (p *QwenParser) ParseBookingIntent(ctx context.Context, req BookingIntentRequest) (BookingIntent, error) {
	if p == nil {
		return BookingIntent{}, fmt.Errorf("qwen parser is nil")
	}
	if strings.TrimSpace(req.Text) == "" {
		return BookingIntent{}, fmt.Errorf("text is required")
	}

	payload := qwenChatRequest{
		Model: p.model,
		Messages: []qwenMessage{
			{Role: "system", Content: qwenBookingSystemPrompt()},
			{Role: "user", Content: qwenBookingUserPrompt(req)},
		},
		Temperature:    ptrFloat64(0),
		EnableThinking: ptrBool(false),
		ResponseFormat: &qwenResponseFormat{Type: "json_object"},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return BookingIntent{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return BookingIntent{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return BookingIntent{}, fmt.Errorf("qwen request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return BookingIntent{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return BookingIntent{}, fmt.Errorf("qwen status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var out qwenChatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return BookingIntent{}, fmt.Errorf("decode qwen response: %w", err)
	}
	if len(out.Choices) == 0 {
		return BookingIntent{}, fmt.Errorf("qwen response has no choices")
	}
	content := strings.TrimSpace(out.Choices[0].Message.Content)
	if content == "" {
		return BookingIntent{}, fmt.Errorf("qwen response content is empty")
	}
	content = trimJSONFence(content)

	var intent BookingIntent
	if err := json.Unmarshal([]byte(content), &intent); err != nil {
		return BookingIntent{}, fmt.Errorf("decode booking intent: %w", err)
	}
	intent.Period = normalizePeriod(intent.Period)
	intent.ServiceQueries = compactStrings(intent.ServiceQueries)
	intent.ServiceIndexes = uniquePositiveInts(intent.ServiceIndexes)
	if intent.Confidence < 0 {
		intent.Confidence = 0
	}
	if intent.Confidence > 1 {
		intent.Confidence = 1
	}
	return intent, nil
}

type qwenChatRequest struct {
	Model               string              `json:"model"`
	Messages            []qwenMessage       `json:"messages"`
	Temperature         *float64            `json:"temperature,omitempty"`
	EnableThinking      *bool               `json:"enable_thinking,omitempty"`
	MaxCompletionTokens int                 `json:"max_completion_tokens,omitempty"`
	ResponseFormat      *qwenResponseFormat `json:"response_format,omitempty"`
}

type qwenMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type qwenResponseFormat struct {
	Type string `json:"type"`
}

type qwenChatResponse struct {
	Choices []struct {
		Message qwenMessage `json:"message"`
	} `json:"choices"`
}

func qwenBookingSystemPrompt() string {
	return strings.Join([]string{
		"You extract booking-search intent for a Telegram appointment bot.",
		"Return only JSON. Do not write prose.",
		"Never create, edit, or delete bookings. Only extract search constraints.",
		"Use this schema:",
		`{"is_booking":true,"service_indexes":[1],"service_queries":["service name"],"duration_min":90,"date_from":"YYYY-MM-DD","date_to":"YYYY-MM-DD","period":"all|morning|day|evening","confidence":0.0}`,
		"date_to is exclusive. Use empty strings when the date is unclear.",
		"Use period morning for before 12:00, day for 12:00-16:59, evening for 17:00 or later.",
		"If the message is not a request to find appointment time, set is_booking=false and confidence<=0.3.",
		"Use only service indexes from the supplied service list. If unsure, leave service_indexes empty and fill service_queries.",
	}, "\n")
}

func qwenBookingUserPrompt(req BookingIntentRequest) string {
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
		sb.WriteString(fmt.Sprintf("%d. ", svc.Index))
		if svc.Category != "" {
			sb.WriteString(svc.Category)
			sb.WriteString(" > ")
		}
		if svc.Subcategory != "" {
			sb.WriteString(svc.Subcategory)
			sb.WriteString(" > ")
		}
		sb.WriteString(svc.Name)
		if svc.DurationMin > 0 {
			sb.WriteString(fmt.Sprintf(" (%d min)", svc.DurationMin))
		}
		if svc.AdminName != "" {
			sb.WriteString(" @")
			sb.WriteString(svc.AdminName)
		}
		if svc.Description != "" {
			sb.WriteString(" - ")
			sb.WriteString(svc.Description)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\nUser message:\n")
	sb.WriteString(req.Text)
	return sb.String()
}

func ptrFloat64(value float64) *float64 {
	return &value
}

func ptrBool(value bool) *bool {
	return &value
}

func trimJSONFence(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "```") {
		return value
	}
	value = strings.TrimPrefix(value, "```")
	value = strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(value), "json") {
		value = strings.TrimSpace(value[4:])
	}
	if idx := strings.LastIndex(value, "```"); idx >= 0 {
		value = value[:idx]
	}
	return strings.TrimSpace(value)
}

func normalizePeriod(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "morning", "day", "evening":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "all"
	}
}

func compactStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[strings.ToLower(value)] {
			continue
		}
		seen[strings.ToLower(value)] = true
		out = append(out, value)
	}
	return out
}

func uniquePositiveInts(values []int) []int {
	seen := map[int]bool{}
	out := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
