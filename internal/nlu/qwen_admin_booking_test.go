package nlu

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestQwenParserParsesAdminBookingIntent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req qwenChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.ResponseFormat == nil || req.ResponseFormat.Type != "json_object" {
			t.Fatalf("response format = %#v", req.ResponseFormat)
		}
		if req.EnableThinking == nil || *req.EnableThinking {
			t.Fatalf("enable_thinking = %#v, want false", req.EnableThinking)
		}
		if req.MaxCompletionTokens != 0 {
			t.Fatalf("max_completion_tokens = %d, want omitted", req.MaxCompletionTokens)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"is_create_booking\":true,\"contact_type\":\"telegram\",\"contact\":\"@client\",\"service_indexes\":[1],\"service_queries\":[\"эпиляция\"],\"duration_min\":90,\"start_at\":\"2026-08-16T18:00:00+03:00\",\"confidence\":0.96}"}}]}`))
	}))
	defer server.Close()
	parser, err := NewQwenParser(QwenConfig{APIKey: "test-key", BaseURL: server.URL, Model: "qwen-test"})
	if err != nil {
		t.Fatalf("NewQwenParser: %v", err)
	}
	intent, err := parser.ParseAdminBookingIntent(context.Background(), AdminBookingIntentRequest{
		Text: "запиши @client на эпиляцию завтра в 18:00",
		Now:  time.Date(2026, 8, 15, 10, 0, 0, 0, time.FixedZone("test", 3*60*60)),
		Services: []Service{{
			Index: 1,
			Name:  "Эпиляция",
		}},
	})
	if err != nil {
		t.Fatalf("ParseAdminBookingIntent: %v", err)
	}
	if !intent.IsCreateBooking || intent.Contact != "@client" || intent.StartAt != "2026-08-16T18:00:00+03:00" {
		t.Fatalf("intent = %#v", intent)
	}
}
