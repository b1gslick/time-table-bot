package telegram

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientGetsAndDownloadsFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/getFile":
			if got := r.URL.Query().Get("file_id"); got != "voice-id" {
				t.Fatalf("file_id = %q", got)
			}
			_, _ = fmt.Fprint(w, `{"ok":true,"result":{"file_id":"voice-id","file_size":3,"file_path":"voice/file.oga"}}`)
		case "/file/voice/file.oga":
			_, _ = w.Write([]byte{1, 2, 3})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := &Client{
		baseURL:     server.URL,
		fileBaseURL: server.URL + "/file",
		httpClient:  &http.Client{Timeout: time.Second},
	}
	file, err := client.GetFile(context.Background(), "voice-id")
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	data, err := client.DownloadFile(context.Background(), file.FilePath, 3)
	if err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	if len(data) != 3 || data[0] != 1 || data[2] != 3 {
		t.Fatalf("data = %#v", data)
	}
}

func TestClientRejectsOversizedDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte{1, 2, 3, 4})
	}))
	defer server.Close()

	client := &Client{fileBaseURL: server.URL, httpClient: server.Client()}
	if _, err := client.DownloadFile(context.Background(), "voice.oga", 3); err == nil {
		t.Fatal("expected oversized download error")
	}
}
