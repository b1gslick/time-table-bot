package nlu

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQwenParserParsesJSONContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %q, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization = %q", got)
		}
		var req qwenChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "qwen-test" {
			t.Fatalf("model = %q", req.Model)
		}
		if req.ResponseFormat == nil || req.ResponseFormat.Type != "json_object" {
			t.Fatalf("response_format = %#v", req.ResponseFormat)
		}
		if req.EnableThinking == nil || *req.EnableThinking {
			t.Fatalf("enable_thinking = %#v, want false", req.EnableThinking)
		}
		if req.MaxCompletionTokens != 0 {
			t.Fatalf("max_completion_tokens = %d, want omitted", req.MaxCompletionTokens)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "{\"is_booking\":true,\"service_indexes\":[1],\"service_queries\":[\"эпиляция\"],\"duration_min\":90,\"date_from\":\"2026-08-16\",\"date_to\":\"2026-08-17\",\"period\":\"evening\",\"confidence\":0.88}"
				}
			}]
		}`))
	}))
	defer server.Close()

	parser, err := NewQwenParser(QwenConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "qwen-test",
	})
	if err != nil {
		t.Fatalf("NewQwenParser: %v", err)
	}
	intent, err := parser.ParseBookingIntent(context.Background(), BookingIntentRequest{
		Text: "хочу эпиляцию завтра вечером",
		Services: []Service{{
			Index:       1,
			Name:        "Эпиляция",
			DurationMin: 90,
		}},
	})
	if err != nil {
		t.Fatalf("ParseBookingIntent: %v", err)
	}
	if !intent.IsBooking || intent.Period != "evening" || intent.DateFrom != "2026-08-16" {
		t.Fatalf("intent = %#v", intent)
	}
}

func TestQwenParserRejectsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad key", http.StatusUnauthorized)
	}))
	defer server.Close()

	parser, err := NewQwenParser(QwenConfig{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewQwenParser: %v", err)
	}
	if _, err := parser.ParseBookingIntent(context.Background(), BookingIntentRequest{Text: "book"}); err == nil {
		t.Fatal("expected error")
	}
}
