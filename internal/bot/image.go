package bot

import (
	"context"
	"path/filepath"
	"strings"

	"time-table-bot/internal/nlu"
	"time-table-bot/internal/telegram"
)

const maxImageSourceBytes int64 = 7 * 1024 * 1024

func (b *Bot) handleImageMessage(ctx context.Context, msg *telegram.Message, user UserRecord) error {
	if b.imageRecognizer == nil {
		return b.sendText(ctx, msg.Chat.ID, tr(user.Language, "image_unavailable"))
	}
	fileClient, ok := b.tg.(TelegramFileClient)
	if !ok {
		return b.sendText(ctx, msg.Chat.ID, tr(user.Language, "image_unavailable"))
	}
	fileID, mimeType, fileSize := imageFileInfo(msg)
	if fileID == "" {
		return b.sendText(ctx, msg.Chat.ID, tr(user.Language, "image_failed"))
	}
	if fileSize > maxImageSourceBytes {
		return b.sendText(ctx, msg.Chat.ID, tr(user.Language, "image_too_large"))
	}
	if err := b.sendText(ctx, msg.Chat.ID, tr(user.Language, "image_processing")); err != nil {
		return err
	}
	file, err := fileClient.GetFile(ctx, fileID)
	if err != nil {
		b.logger.Printf("image: telegram get file failed user=%d: %v", user.TelegramID, err)
		return b.sendText(ctx, msg.Chat.ID, tr(user.Language, "image_failed"))
	}
	if file.FileSize > maxImageSourceBytes {
		return b.sendText(ctx, msg.Chat.ID, tr(user.Language, "image_too_large"))
	}
	data, err := fileClient.DownloadFile(ctx, file.FilePath, maxImageSourceBytes)
	if err != nil {
		b.logger.Printf("image: telegram download failed user=%d: %v", user.TelegramID, err)
		return b.sendText(ctx, msg.Chat.ID, tr(user.Language, "image_failed"))
	}
	recognized, err := b.imageRecognizer.RecognizeText(ctx, nlu.ImageTextRequest{
		Image:    data,
		MIMEType: mimeType,
		Language: user.Language,
	})
	if err != nil {
		b.logger.Printf("image: OCR failed user=%d: %v", user.TelegramID, err)
		if strings.TrimSpace(msg.Caption) == "" {
			return b.sendText(ctx, msg.Chat.ID, tr(user.Language, "image_failed"))
		}
		recognized = ""
	}
	text := joinImageText(msg.Caption, recognized)
	if text == "" {
		return b.sendText(ctx, msg.Chat.ID, tr(user.Language, "image_no_text"))
	}
	if err := b.sendText(ctx, msg.Chat.ID, tr(user.Language, "image_recognized", text)); err != nil {
		return err
	}
	return b.HandleMessage(ctx, &telegram.Message{
		From: msg.From,
		Chat: msg.Chat,
		Text: text,
	})
}

func imageFileInfo(msg *telegram.Message) (fileID, mimeType string, fileSize int64) {
	if msg == nil {
		return "", "", 0
	}
	if len(msg.Photo) > 0 {
		best := msg.Photo[0]
		for _, photo := range msg.Photo[1:] {
			if photo.Width*photo.Height > best.Width*best.Height ||
				(photo.Width*photo.Height == best.Width*best.Height && photo.FileSize > best.FileSize) {
				best = photo
			}
		}
		return best.FileID, "image/jpeg", best.FileSize
	}
	if isImageDocument(msg.Document) {
		mimeType := strings.ToLower(strings.TrimSpace(msg.Document.MIMEType))
		if mimeType == "" {
			mimeType = imageMIMETypeFromName(msg.Document.FileName)
		}
		return msg.Document.FileID, mimeType, msg.Document.FileSize
	}
	return "", "", 0
}

func isImageDocument(document *telegram.Document) bool {
	if document == nil {
		return false
	}
	mimeType := strings.ToLower(strings.TrimSpace(document.MIMEType))
	if strings.HasPrefix(mimeType, "image/") {
		return true
	}
	return imageMIMETypeFromName(document.FileName) != ""
}

func imageMIMETypeFromName(name string) string {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(name))) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".tif", ".tiff":
		return "image/tiff"
	default:
		return ""
	}
}

func joinImageText(caption, recognized string) string {
	caption = strings.TrimSpace(caption)
	recognized = strings.TrimSpace(recognized)
	if caption == "" {
		return recognized
	}
	if recognized == "" || strings.EqualFold(caption, recognized) {
		return caption
	}
	return caption + "\n" + recognized
}
