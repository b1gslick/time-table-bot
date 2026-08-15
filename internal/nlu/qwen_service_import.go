package nlu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func (p *QwenParser) ParseAdminServiceImport(ctx context.Context, req AdminServiceImportRequest) (AdminServiceImportIntent, error) {
	if p == nil {
		return AdminServiceImportIntent{}, fmt.Errorf("qwen parser is nil")
	}
	if strings.TrimSpace(req.Text) == "" {
		return AdminServiceImportIntent{}, fmt.Errorf("text is required")
	}
	payload := qwenChatRequest{
		Model: p.model,
		Messages: []qwenMessage{
			{Role: "system", Content: qwenServiceImportSystemPrompt()},
			{Role: "user", Content: qwenServiceImportUserPrompt(req)},
		},
		Temperature:         ptrFloat64(0),
		EnableThinking:      ptrBool(false),
		MaxCompletionTokens: 3000,
		ResponseFormat:      &qwenResponseFormat{Type: "json_object"},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return AdminServiceImportIntent{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return AdminServiceImportIntent{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return AdminServiceImportIntent{}, fmt.Errorf("qwen service import request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return AdminServiceImportIntent{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return AdminServiceImportIntent{}, fmt.Errorf("qwen service import status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out qwenChatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return AdminServiceImportIntent{}, fmt.Errorf("decode qwen service import response: %w", err)
	}
	if len(out.Choices) == 0 {
		return AdminServiceImportIntent{}, fmt.Errorf("qwen service import response has no choices")
	}
	var intent AdminServiceImportIntent
	if err := json.Unmarshal([]byte(trimJSONFence(out.Choices[0].Message.Content)), &intent); err != nil {
		return AdminServiceImportIntent{}, fmt.Errorf("decode service import intent: %w", err)
	}
	if len(intent.Entries) > 50 {
		intent.Entries = intent.Entries[:50]
	}
	for i := range intent.Entries {
		entry := &intent.Entries[i]
		entry.Category = strings.TrimSpace(entry.Category)
		entry.Subcategory = strings.TrimSpace(entry.Subcategory)
		entry.Name = strings.TrimSpace(entry.Name)
		entry.PriceText = strings.TrimSpace(entry.PriceText)
		if entry.DurationMin < 0 {
			entry.DurationMin = 0
		}
		entry.Confidence = clampConfidence(entry.Confidence)
	}
	intent.Confidence = clampConfidence(intent.Confidence)
	return intent, nil
}

func qwenServiceImportSystemPrompt() string {
	return strings.Join([]string{
		"You structure a salon administrator's spoken or written service catalog.",
		"Return only JSON. Never create, edit, or delete data.",
		"Use this schema:",
		`{"is_service_catalog":true,"entries":[{"category":"category","subcategory":"subcategory or empty","name":"service name","duration_min":60,"price_text":"45 EUR","confidence":0.0}],"confidence":0.0}`,
		"Each distinct duration or price variant is a separate service entry.",
		"Convert hours to minutes, including phrases such as one and a half hours. Do not invent a missing duration or price.",
		"Put services into concise semantic categories and subcategories. Reuse exact category and subcategory spelling from existing services when appropriate.",
		"Do not include the category or subcategory again in the service name unless it is naturally part of that name.",
		"Preserve the stated price, currency, and useful pricing notes in price_text. Keep it empty if no price was stated.",
		"Set is_service_catalog=true only when the administrator asks to add, import, create, or update one or more services or clearly dictates a price list.",
		"A client appointment request, a request to show services, or ordinary conversation is not a service catalog.",
	}, "\n")
}

func qwenServiceImportUserPrompt(req AdminServiceImportRequest) string {
	var sb strings.Builder
	sb.WriteString("Language: ")
	sb.WriteString(req.Language)
	sb.WriteString("\nExisting services:\n")
	for _, svc := range req.ExistingServices {
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
		if svc.Description != "" {
			sb.WriteString(" - ")
			sb.WriteString(svc.Description)
		}
		sb.WriteByte('\n')
	}
	sb.WriteString("\nAdministrator message:\n")
	sb.WriteString(req.Text)
	return sb.String()
}
