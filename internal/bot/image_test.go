package bot

import (
	"testing"

	"time-table-bot/internal/telegram"
)

func TestImageFileInfoSelectsLargestTelegramPhoto(t *testing.T) {
	fileID, mimeType, size := imageFileInfo(&telegram.Message{Photo: []telegram.PhotoSize{
		{FileID: "small", Width: 320, Height: 240, FileSize: 100},
		{FileID: "large", Width: 1280, Height: 960, FileSize: 900},
		{FileID: "medium", Width: 800, Height: 600, FileSize: 500},
	}})
	if fileID != "large" || mimeType != "image/jpeg" || size != 900 {
		t.Fatalf("imageFileInfo = %q, %q, %d", fileID, mimeType, size)
	}
}

func TestImageFileInfoAcceptsScreenshotDocument(t *testing.T) {
	fileID, mimeType, size := imageFileInfo(&telegram.Message{Document: &telegram.Document{
		FileID:   "screenshot",
		FileName: "booking.PNG",
		FileSize: 1234,
	}})
	if fileID != "screenshot" || mimeType != "image/png" || size != 1234 {
		t.Fatalf("imageFileInfo = %q, %q, %d", fileID, mimeType, size)
	}
}

func TestIsImageDocumentRejectsNonImage(t *testing.T) {
	if isImageDocument(&telegram.Document{FileName: "booking.pdf", MIMEType: "application/pdf"}) {
		t.Fatal("PDF must not be treated as an image")
	}
}

func TestJoinImageTextKeepsCaptionAndOCR(t *testing.T) {
	got := joinImageText("Запиши клиента", "эпиляция завтра в 18:00")
	if got != "Запиши клиента\nэпиляция завтра в 18:00" {
		t.Fatalf("joinImageText = %q", got)
	}
}
