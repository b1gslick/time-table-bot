package nlu

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQwenParserRecognizesHandwrittenImage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req qwenVisionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "qwen3.7-plus" || len(req.Messages) != 1 || len(req.Messages[0].Content) != 2 {
			t.Fatalf("request = %#v", req)
		}
		imageURL := req.Messages[0].Content[0].ImageURL
		if imageURL == nil || !strings.HasPrefix(imageURL.URL, "data:image/png;base64,") {
			t.Fatalf("image URL = %#v", imageURL)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"эпиляция завтра в 18:00"}}]}`))
	}))
	defer server.Close()

	parser, err := NewQwenParser(QwenConfig{APIKey: "test-key", BaseURL: server.URL, Model: "qwen3.7-plus"})
	if err != nil {
		t.Fatalf("NewQwenParser: %v", err)
	}
	got, err := parser.RecognizeText(context.Background(), ImageTextRequest{
		Image:    []byte("image"),
		MIMEType: "image/png",
		Language: "ru",
	})
	if err != nil {
		t.Fatalf("RecognizeText: %v", err)
	}
	if got != "эпиляция завтра в 18:00" {
		t.Fatalf("recognized text = %q", got)
	}
}
