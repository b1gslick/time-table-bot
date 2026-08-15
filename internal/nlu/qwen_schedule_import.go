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

func (p *QwenParser) ParseAdminScheduleImport(ctx context.Context, req AdminScheduleImportRequest) (AdminScheduleImportIntent, error) {
	if p == nil {
		return AdminScheduleImportIntent{}, fmt.Errorf("qwen parser is nil")
	}
	if strings.TrimSpace(req.Text) == "" {
		return AdminScheduleImportIntent{}, fmt.Errorf("text is required")
	}
	payload := qwenChatRequest{
		Model: p.model,
		Messages: []qwenMessage{
			{Role: "system", Content: qwenScheduleImportSystemPrompt()},
			{Role: "user", Content: qwenScheduleImportUserPrompt(req)},
		},
		Temperature:         ptrFloat64(0),
		EnableThinking:      ptrBool(false),
		MaxCompletionTokens: 4000,
		ResponseFormat:      &qwenResponseFormat{Type: "json_object"},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return AdminScheduleImportIntent{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return AdminScheduleImportIntent{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return AdminScheduleImportIntent{}, fmt.Errorf("qwen schedule import request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return AdminScheduleImportIntent{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return AdminScheduleImportIntent{}, fmt.Errorf("qwen schedule import status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out qwenChatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return AdminScheduleImportIntent{}, fmt.Errorf("decode qwen schedule import response: %w", err)
	}
	if len(out.Choices) == 0 {
		return AdminScheduleImportIntent{}, fmt.Errorf("qwen schedule import response has no choices")
	}
	var intent AdminScheduleImportIntent
	if err := json.Unmarshal([]byte(trimJSONFence(out.Choices[0].Message.Content)), &intent); err != nil {
		return AdminScheduleImportIntent{}, fmt.Errorf("decode schedule import intent: %w", err)
	}
	if len(intent.Entries) > 50 {
		intent.Entries = intent.Entries[:50]
	}
	for i := range intent.Entries {
		entry := &intent.Entries[i]
		entry.Client = strings.TrimSpace(entry.Client)
		entry.ServiceIndexes = uniquePositiveInts(entry.ServiceIndexes)
		entry.ServiceQueries = compactStrings(entry.ServiceQueries)
		entry.StartAt = strings.TrimSpace(entry.StartAt)
		if entry.DurationMin < 0 {
			entry.DurationMin = 0
		}
		entry.Confidence = clampConfidence(entry.Confidence)
	}
	intent.Confidence = clampConfidence(intent.Confidence)
	return intent, nil
}

func clampConfidence(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func qwenScheduleImportSystemPrompt() string {
	return strings.Join([]string{
		"You extract appointments from OCR text of a weekly salon planner screenshot or photograph.",
		"Return only JSON. Never create, edit, or delete anything.",
		"Use this schema:",
		`{"is_schedule":true,"entries":[{"client":"visible client name","service_indexes":[1],"service_queries":["visible service"],"duration_min":60,"start_at":"RFC3339 datetime","confidence":0.0}],"confidence":0.0}`,
		"Ignore application chrome, navigation, weekday labels, page numbers, battery text, and menu labels.",
		"A line beginning with HH:MM starts an appointment. Following lines belong to it until the next HH:MM or planner section.",
		"Use the planner month, year, weekday and date labels to assign each section's date. Validate that weekday and calendar date agree.",
		"OCR may interleave left and right page date labels. Infer sections conservatively from reading order and repeated HH:MM groups.",
		"When OCR lines include [x=... y=... w=... h=...] coordinates, use spatial layout instead of text order.",
		"Date tabs near the left or right image edge label the planner panel beside them; appointments inside that panel share its date.",
		"Use x/y coordinates and page dimensions to associate each appointment with the nearest containing panel and edge date tab.",
		"When a timed line has [panel_date_day=N], N is authoritative for that appointment's calendar day.",
		"The selected day shown beside the month/week in the top app header is navigation state, not a planner panel date.",
		"Do not invent a client, date, time, service, or duration. Preserve visible client names so the bot can resolve aliases later.",
		"Keep start_at empty when the date or time is ambiguous. When present, include the supplied timezone offset.",
		"Use only service indexes from the supplied list. Keep uncertain visible service text in service_queries.",
		"Set duration_min only when explicitly visible. Include uncertain entries but lower their confidence.",
		"Set is_schedule=true only when the text contains at least two timed appointments or clearly represents a planner.",
	}, "\n")
}

func qwenScheduleImportUserPrompt(req AdminScheduleImportRequest) string {
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
		sb.WriteByte('\n')
	}
	sb.WriteString("\nOCR text:\n")
	sb.WriteString(annotatePlannerPanels(normalizeScheduleOCRText(req.Text)))
	return sb.String()
}

var scheduleOCRWaxRE = regexp.MustCompile(`(?i)\bBOCK\b`)

func normalizeScheduleOCRText(text string) string {
	return scheduleOCRWaxRE.ReplaceAllString(text, "воск")
}

var (
	plannerPageRE = regexp.MustCompile(`(?m)^\[page width=(\d+) height=(\d+)\]`)
	plannerLineRE = regexp.MustCompile(`^\[x=(\d+) y=(\d+) w=(\d+) h=(\d+)\]\s*(.*)$`)
	plannerDayRE  = regexp.MustCompile(`^(?:[1-9]|[12]\d|3[01])$`)
	plannerTimeRE = regexp.MustCompile(`(?:^|\s)(?:[01]?\d|2[0-3]):[0-5]\d(?:\s|$)`)
)

type plannerOCRLine struct {
	raw       string
	x, y      int
	text      string
	side      int
	isWeekday bool
}

type plannerDateMarker struct {
	side, y, day int
}

func annotatePlannerPanels(text string) string {
	pageMatch := plannerPageRE.FindStringSubmatch(text)
	if len(pageMatch) != 3 {
		return text
	}
	pageWidth, _ := strconv.Atoi(pageMatch[1])
	if pageWidth <= 0 {
		return text
	}
	lines := strings.Split(text, "\n")
	parsed := make([]plannerOCRLine, len(lines))
	for i, raw := range lines {
		parsed[i].raw = raw
		match := plannerLineRE.FindStringSubmatch(raw)
		if len(match) != 6 {
			continue
		}
		parsed[i].x, _ = strconv.Atoi(match[1])
		parsed[i].y, _ = strconv.Atoi(match[2])
		parsed[i].text = strings.TrimSpace(match[5])
		if parsed[i].x < pageWidth/2 {
			parsed[i].side = -1
		} else {
			parsed[i].side = 1
		}
		parsed[i].isWeekday = isPlannerWeekday(parsed[i].text)
	}
	edge := pageWidth / 10
	markers := make([]plannerDateMarker, 0, 7)
	for _, line := range parsed {
		if !plannerDayRE.MatchString(line.text) || line.x >= edge && line.x <= pageWidth-edge {
			continue
		}
		day, _ := strconv.Atoi(line.text)
		for _, weekday := range parsed {
			if !weekday.isWeekday || weekday.side != line.side || absInt(weekday.y-line.y) > 45 {
				continue
			}
			markers = append(markers, plannerDateMarker{side: line.side, y: (line.y + weekday.y) / 2, day: day})
			break
		}
	}
	if len(markers) == 0 {
		return text
	}
	for i, line := range parsed {
		if !plannerTimeRE.MatchString(" " + line.text + " ") {
			continue
		}
		bestDistance := int(^uint(0) >> 1)
		bestDay := 0
		for _, marker := range markers {
			if marker.side != line.side {
				continue
			}
			if distance := absInt(marker.y - line.y); distance < bestDistance {
				bestDistance, bestDay = distance, marker.day
			}
		}
		if bestDay > 0 {
			lines[i] = fmt.Sprintf("[panel_date_day=%d] %s", bestDay, line.raw)
		}
	}
	return strings.Join(lines, "\n")
}

func isPlannerWeekday(value string) bool {
	value = strings.ToLower(strings.Trim(value, " .,:;"))
	switch value {
	case "пн", "вт", "ср", "чт", "пт", "сб", "вс", "bt", "cp", "bc", "mon", "tue", "wed", "thu", "fri", "sat", "sun":
		return true
	default:
		return false
	}
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
