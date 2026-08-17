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

func (p *QwenParser) ParseAdminSchedulePlan(ctx context.Context, req AdminSchedulePlanRequest) (AdminSchedulePlanIntent, error) {
	if p == nil {
		return AdminSchedulePlanIntent{}, fmt.Errorf("qwen parser is nil")
	}
	if strings.TrimSpace(req.Text) == "" {
		return AdminSchedulePlanIntent{}, fmt.Errorf("text is required")
	}
	payload := qwenChatRequest{
		Model: p.model,
		Messages: []qwenMessage{
			{Role: "system", Content: qwenSchedulePlanSystemPrompt()},
			{Role: "user", Content: qwenSchedulePlanUserPrompt(req)},
		},
		Temperature:         ptrFloat64(0),
		EnableThinking:      ptrBool(false),
		MaxCompletionTokens: 1800,
		ResponseFormat:      &qwenResponseFormat{Type: "json_object"},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return AdminSchedulePlanIntent{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return AdminSchedulePlanIntent{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return AdminSchedulePlanIntent{}, fmt.Errorf("qwen schedule plan request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return AdminSchedulePlanIntent{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return AdminSchedulePlanIntent{}, fmt.Errorf("qwen schedule plan status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out qwenChatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return AdminSchedulePlanIntent{}, fmt.Errorf("decode qwen schedule plan response: %w", err)
	}
	if len(out.Choices) == 0 {
		return AdminSchedulePlanIntent{}, fmt.Errorf("qwen schedule plan response has no choices")
	}
	var intent AdminSchedulePlanIntent
	if err := json.Unmarshal([]byte(trimJSONFence(out.Choices[0].Message.Content)), &intent); err != nil {
		return AdminSchedulePlanIntent{}, fmt.Errorf("decode schedule plan intent: %w", err)
	}
	intent.TargetMonth = strings.TrimSpace(intent.TargetMonth)
	intent.CopyFromMonth = strings.TrimSpace(intent.CopyFromMonth)
	intent.ClosedDates = compactStrings(intent.ClosedDates)
	if intent.SlotDurationMin < 0 {
		intent.SlotDurationMin = 0
	}
	for i := range intent.Rules {
		intent.Rules[i].Weekdays = uniqueScheduleWeekdays(intent.Rules[i].Weekdays)
		intent.Rules[i].Start = strings.TrimSpace(intent.Rules[i].Start)
		intent.Rules[i].End = strings.TrimSpace(intent.Rules[i].End)
	}
	for i := range intent.ExtraDays {
		intent.ExtraDays[i].Date = strings.TrimSpace(intent.ExtraDays[i].Date)
		intent.ExtraDays[i].Start = strings.TrimSpace(intent.ExtraDays[i].Start)
		intent.ExtraDays[i].End = strings.TrimSpace(intent.ExtraDays[i].End)
	}
	intent.Confidence = clampConfidence(intent.Confidence)
	return intent, nil
}

func uniqueScheduleWeekdays(values []int) []int {
	out := make([]int, 0, len(values))
	seen := map[int]bool{}
	for _, value := range values {
		if value < 1 || value > 7 || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func qwenSchedulePlanSystemPrompt() string {
	return strings.Join([]string{
		"You extract a monthly work-schedule plan for a salon administrator.",
		"Return only JSON. Never create, edit, or delete data.",
		"Use this schema:",
		`{"is_schedule_plan":true,"target_month":"YYYY-MM","copy_from_month":"YYYY-MM or empty","rules":[{"weekdays":[1,2,3,4,5],"start":"10:00","end":"17:00"}],"extra_days":[{"date":"YYYY-MM-DD","weekday":6,"start":"10:00","end":"17:00"}],"closed_dates":["YYYY-MM-DD"],"slot_duration_min":0,"confidence":0.0}`,
		"Weekdays use ISO numbering: Monday=1 through Sunday=7.",
		"Resolve next month, this month, named months, and relative dates from the supplied current datetime.",
		"For 'same as this month', set target_month to next month and copy_from_month to the current month. Do not invent rules.",
		"For every weekday, put Monday-Friday in one rule. Separate rules may use different hours.",
		"Specific working dates belong in extra_days. If their hours are omitted, leave start and end empty so the application can inherit the main hours.",
		"When the administrator calls a specific date a weekday, preserve that claimed weekday in extra_days.weekday so the application can validate it.",
		"Specific non-working dates belong in closed_dates.",
		"slot_duration_min is zero unless the administrator explicitly specifies the appointment grid step.",
		"If Current plan is supplied, apply the correction to it and return the complete corrected plan.",
		"Set is_schedule_plan=false for requests to show a schedule, create a client booking, or import appointments from an image.",
	}, "\n")
}

func qwenSchedulePlanUserPrompt(req AdminSchedulePlanRequest) string {
	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	timezone := strings.TrimSpace(req.Timezone)
	if timezone == "" {
		timezone = now.Location().String()
	}
	current := strings.TrimSpace(req.CurrentPlan)
	if current == "" {
		current = "none"
	}
	return fmt.Sprintf(
		"Current datetime: %s\nTimezone: %s\nLanguage: %s\nCurrent plan: %s\n\nAdministrator request:\n%s",
		now.Format(time.RFC3339), timezone, req.Language, current, req.Text,
	)
}
