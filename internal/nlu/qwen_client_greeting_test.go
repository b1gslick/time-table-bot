package nlu

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQwenParserGeneratesClientGreetingFromProfileAndServices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req qwenChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.MaxCompletionTokens != 500 || req.ResponseFormat == nil || req.ResponseFormat.Type != "json_object" {
			t.Fatalf("request settings = %#v", req)
		}
		if len(req.Messages) != 2 {
			t.Fatalf("messages = %#v", req.Messages)
		}
		for _, want := range []string{"Client name: Анна", "Master description:", "Работаю в центре", "Эпиляция > Ноги > Голени", "45 min", "35 EUR"} {
			if !strings.Contains(req.Messages[1].Content, want) {
				t.Fatalf("user prompt = %q, missing %q", req.Messages[1].Content, want)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"text\":\"Анна, добро пожаловать! Здесь можно подобрать подходящую процедуру и записаться в удобное время.\"}"}}]}`))
	}))
	defer server.Close()
	parser, err := NewQwenParser(QwenConfig{APIKey: "test-key", BaseURL: server.URL, Model: "qwen-test"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := parser.GenerateClientGreeting(context.Background(), ClientGreetingRequest{
		ClientName: "Анна", Language: "ru", MasterDescription: "Работаю в центре",
		Services: []Service{{Category: "Эпиляция", Subcategory: "Ноги", Name: "Голени", DurationMin: 45, Description: "35 EUR"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "Анна, добро пожаловать! Здесь можно подобрать подходящую процедуру и записаться в удобное время." {
		t.Fatalf("greeting = %q", got)
	}
}
