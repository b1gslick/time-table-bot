package nlu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func (p *QwenParser) ParseAdminScheduleEdit(ctx context.Context, req AdminScheduleEditRequest) (AdminScheduleEditIntent, error) {
	if p == nil {
		return AdminScheduleEditIntent{}, fmt.Errorf("qwen parser is nil")
	}
	if strings.TrimSpace(req.Text) == "" {
		return AdminScheduleEditIntent{}, fmt.Errorf("text is required")
	}
	payload := qwenChatRequest{
		Model: p.model,
		Messages: []qwenMessage{
			{Role: "system", Content: qwenScheduleEditSystemPrompt()},
			{Role: "user", Content: qwenScheduleEditUserPrompt(req)},
		},
		Temperature:    ptrFloat64(0),
		EnableThinking: ptrBool(false),
		ResponseFormat: &qwenResponseFormat{Type: "json_object"},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return AdminScheduleEditIntent{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return AdminScheduleEditIntent{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return AdminScheduleEditIntent{}, fmt.Errorf("qwen schedule edit request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return AdminScheduleEditIntent{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return AdminScheduleEditIntent{}, fmt.Errorf("qwen schedule edit status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out qwenChatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return AdminScheduleEditIntent{}, fmt.Errorf("decode qwen schedule edit response: %w", err)
	}
	if len(out.Choices) == 0 {
		return AdminScheduleEditIntent{}, fmt.Errorf("qwen schedule edit response has no choices")
	}
	var intent AdminScheduleEditIntent
	if err := json.Unmarshal([]byte(trimJSONFence(out.Choices[0].Message.Content)), &intent); err != nil {
		return AdminScheduleEditIntent{}, fmt.Errorf("decode schedule edit intent: %w", err)
	}
	intent.Client = strings.TrimSpace(intent.Client)
	intent.ContactType = normalizeContactType(intent.ContactType)
	intent.Contact = strings.TrimSpace(intent.Contact)
	intent.ServiceIndexes = uniquePositiveInts(intent.ServiceIndexes)
	intent.ServiceQueries = compactStrings(intent.ServiceQueries)
	for i := range intent.Services {
		service := &intent.Services[i]
		service.ServiceIndexes = uniquePositiveInts(service.ServiceIndexes)
		service.ServiceQueries = compactStrings(service.ServiceQueries)
		if service.DurationMin < 0 {
			service.DurationMin = 0
		}
		if duration, ok := explicitServiceDuration(strings.Join(service.ServiceQueries, " ")); ok {
			service.DurationMin = duration
		}
		normalizeScheduleEditServiceDuration(service, req.Services)
	}
	intent.StartAt = strings.TrimSpace(intent.StartAt)
	if intent.DurationMin < 0 {
		intent.DurationMin = 0
	}
	intent.Confidence = clampConfidence(intent.Confidence)
	return intent, nil
}

var (
	explicitHoursRE   = regexp.MustCompile(`(?i)(?:(\d+(?:[.,]\d+)?)\s*)?(?:час(?:а|ов)?|hours?|hrs?)`)
	explicitMinutesRE = regexp.MustCompile(`(?i)(\d+)\s*(?:мин(?:ут(?:а|ы)?)?|minutes?|mins?)`)
)

func explicitServiceDuration(text string) (int, bool) {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(text), "ё", "е"))
	if normalized == "" {
		return 0, false
	}
	if strings.Contains(normalized, "полтора час") || strings.Contains(normalized, "полторы час") {
		return 90, true
	}
	hours := 0.0
	foundHours := false
	if match := explicitHoursRE.FindStringSubmatch(normalized); len(match) > 0 {
		foundHours = true
		hours = 1
		if match[1] != "" {
			parsed, err := strconv.ParseFloat(strings.ReplaceAll(match[1], ",", "."), 64)
			if err == nil {
				hours = parsed
			}
		}
	}
	minutes := 0
	foundMinutes := false
	if match := explicitMinutesRE.FindStringSubmatch(normalized); len(match) > 1 {
		parsed, err := strconv.Atoi(match[1])
		if err == nil {
			minutes, foundMinutes = parsed, true
		}
	}
	if !foundHours && !foundMinutes {
		return 0, false
	}
	total := int(hours*60+0.5) + minutes
	return total, total > 0
}

func normalizeScheduleEditServiceDuration(change *AdminScheduleEditService, available []Service) {
	if change == nil || change.DurationMin <= 0 || len(available) == 0 {
		return
	}
	byIndex := make(map[int]Service, len(available))
	for _, service := range available {
		if service.Index > 0 {
			byIndex[service.Index] = service
		}
	}
	total := 0
	category := ""
	for _, index := range change.ServiceIndexes {
		service, ok := byIndex[index]
		if !ok {
			continue
		}
		total += service.DurationMin
		if category == "" {
			category = service.Category
		}
	}
	if total == change.DurationMin || category == "" {
		return
	}
	type candidate struct {
		index    int
		duration int
		score    int
	}
	query := strings.Join(change.ServiceQueries, " ")
	candidates := make([]candidate, 0)
	for _, service := range available {
		if !strings.EqualFold(strings.TrimSpace(service.Category), strings.TrimSpace(category)) || service.DurationMin <= 0 || service.DurationMin > change.DurationMin {
			continue
		}
		candidates = append(candidates, candidate{
			index: service.Index, duration: service.DurationMin,
			score: scheduleEditServiceScore(query, service),
		})
	}
	type combination struct {
		indexes []int
		score   int
	}
	best := map[int]combination{0: {}}
	for _, item := range candidates {
		next := make(map[int]combination, len(best)*2)
		for sum, current := range best {
			next[sum] = current
		}
		for sum, current := range best {
			newSum := sum + item.duration
			if newSum > change.DurationMin {
				continue
			}
			proposed := combination{indexes: append(append([]int{}, current.indexes...), item.index), score: current.score + item.score}
			existing, exists := next[newSum]
			if !exists || len(proposed.indexes) < len(existing.indexes) || (len(proposed.indexes) == len(existing.indexes) && proposed.score > existing.score) {
				next[newSum] = proposed
			}
		}
		best = next
	}
	if resolved, ok := best[change.DurationMin]; ok && len(resolved.indexes) > 0 {
		change.ServiceIndexes = resolved.indexes
	}
}

func scheduleEditServiceScore(query string, service Service) int {
	query = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(query), "ё", "е"))
	target := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(strings.Join([]string{service.Category, service.Subcategory, service.Name}, " ")), "ё", "е"))
	if query == "" || target == "" {
		return 0
	}
	score := 0
	if strings.Contains(query, strings.ToLower(strings.TrimSpace(service.Name))) {
		score += 20
	}
	for _, token := range strings.Fields(query) {
		token = strings.Trim(token, " ,.;:!?()-")
		if len([]rune(token)) >= 3 && strings.Contains(target, token) {
			score += 2
		}
	}
	return score
}

func qwenScheduleEditSystemPrompt() string {
	return strings.Join([]string{
		"You extract changes to one salon appointment currently being reviewed by a client or an administrator.",
		"Return only JSON. Never create, save, edit, or delete anything.",
		"Use this schema:",
		`{"is_edit":true,"change_client":false,"client":"","contact_type":"unknown|telegram|phone","contact":"","change_service":false,"services":[{"service_indexes":[1],"service_queries":["service wording"],"duration_min":60}],"service_indexes":[],"service_queries":[],"duration_min":0,"change_start_at":false,"start_at":"","confidence":0.0}`,
		"Set a change_* flag only when the person explicitly changes that field or clearly implies a replacement.",
		"Leave every unchanged field empty. Do not repeat current values as changes.",
		"Resolve relative dates and phrases such as later, tomorrow, Friday, or move by one day using the current appointment and supplied current datetime.",
		"When change_start_at=true, return an RFC3339 datetime with the supplied timezone offset. Preserve the current date when only time changes, and preserve current time when only date changes.",
		"When change_client=true, preserve a spoken client name in client. Extract @username or phone into contact and set contact_type. A client-facing caller may not change the client, but still extract only explicitly requested changes.",
		"When services change, put each separately requested service or category in its own services item. A duration modifies only the service it is spoken next to, not the combined appointment.",
		"Add hours and minutes in one phrase: 1 hour 30 minutes and one-and-a-half hours both mean duration_min=90, never 60.",
		"Use only service indexes from the supplied list. If a timed category duration requires multiple catalog items, select a combination from that category whose durations sum to the requested duration.",
		"For example, electrolysis for 90 minutes plus wax bikini must contain one 90-minute electrolysis services item and a separate wax bikini item.",
		"For unqualified bikini, choose classic bikini; choose deep bikini only when deep is explicit.",
		"If uncertain, keep the administrator's wording in service_queries. Keep aggregate service_indexes/service_queries empty when services is populated.",
		"Set is_edit=false if the message is not a correction to the current appointment.",
	}, "\n")
}

func qwenScheduleEditUserPrompt(req AdminScheduleEditRequest) string {
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
	sb.WriteString("\nCurrent appointment:\nClient: ")
	sb.WriteString(req.CurrentClient)
	if strings.TrimSpace(req.CurrentContact) != "" {
		sb.WriteString(" (")
		sb.WriteString(req.CurrentContact)
		sb.WriteString(")")
	}
	sb.WriteString("\nStart: ")
	sb.WriteString(req.CurrentStartAt)
	sb.WriteString("\nServices: ")
	sb.WriteString(strings.Join(req.CurrentServices, ", "))
	sb.WriteString("\n\nAvailable services:\n")
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
		sb.WriteByte('\n')
	}
	sb.WriteString("\nCorrection:\n")
	sb.WriteString(req.Text)
	return sb.String()
}
