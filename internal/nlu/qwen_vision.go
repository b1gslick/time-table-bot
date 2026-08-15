package nlu

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func (p *QwenParser) RecognizeText(ctx context.Context, req ImageTextRequest) (string, error) {
	if p == nil {
		return "", fmt.Errorf("qwen parser is nil")
	}
	if len(req.Image) == 0 {
		return "", fmt.Errorf("image is required")
	}
	mimeType := normalizeImageMIMEType(req.MIMEType)
	dataURL := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(req.Image)
	payload := qwenVisionRequest{
		Model: p.model,
		Messages: []qwenVisionMessage{{
			Role: "user",
			Content: []qwenVisionContent{
				{Type: "image_url", ImageURL: &qwenImageURL{URL: dataURL}},
				{Type: "text", Text: qwenImageTextPrompt(req.Language)},
			},
		}},
		Temperature:         ptrFloat64(0),
		MaxCompletionTokens: 1200,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("qwen vision request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("qwen vision status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out qwenChatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("decode qwen vision response: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("qwen vision response has no choices")
	}
	text := strings.TrimSpace(out.Choices[0].Message.Content)
	if text == "" {
		return "", fmt.Errorf("qwen vision response is empty")
	}
	return text, nil
}

type qwenVisionRequest struct {
	Model               string              `json:"model"`
	Messages            []qwenVisionMessage `json:"messages"`
	Temperature         *float64            `json:"temperature,omitempty"`
	MaxCompletionTokens int                 `json:"max_completion_tokens,omitempty"`
}

type qwenVisionMessage struct {
	Role    string              `json:"role"`
	Content []qwenVisionContent `json:"content"`
}

type qwenVisionContent struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *qwenImageURL `json:"image_url,omitempty"`
}

type qwenImageURL struct {
	URL string `json:"url"`
}

func qwenImageTextPrompt(language string) string {
	return strings.Join([]string{
		"Extract all visible text from this image, including handwritten text.",
		"Preserve dates, times, names, and line order.",
		"Return only the extracted text without explanations, Markdown, or invented content.",
		"Expected user language: " + strings.TrimSpace(language),
	}, "\n")
}

func normalizeImageMIMEType(value string) string {
	switch strings.ToLower(strings.TrimSpace(strings.Split(value, ";")[0])) {
	case "image/png", "image/webp", "image/tiff":
		return strings.ToLower(strings.TrimSpace(strings.Split(value, ";")[0]))
	default:
		return "image/jpeg"
	}
}
