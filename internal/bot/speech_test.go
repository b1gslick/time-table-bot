package bot

import (
	"testing"

	"time-table-bot/internal/telegram"
)

func TestSpeechFileInfo(t *testing.T) {
	fileID, mimeType, size, duration := speechFileInfo(&telegram.Message{Voice: &telegram.Voice{
		FileID:   "voice-id",
		FileSize: 123,
		Duration: 45,
	}})
	if fileID != "voice-id" || mimeType != "audio/ogg" || size != 123 || duration != 45 {
		t.Fatalf("speechFileInfo = %q, %q, %d, %d", fileID, mimeType, size, duration)
	}
}
