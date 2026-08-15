package bot

import (
	"context"
	"strings"

	"time-table-bot/internal/nlu"
	"time-table-bot/internal/telegram"
)

const (
	maxSpeechSourceBytes     int64 = 7 * 1024 * 1024
	maxSpeechDurationSeconds       = 120
)

func (b *Bot) handleSpeechMessage(ctx context.Context, msg *telegram.Message, user UserRecord) error {
	if b.speechRecognizer == nil {
		return b.sendText(ctx, msg.Chat.ID, tr(user.Language, "speech_unavailable"))
	}
	fileClient, ok := b.tg.(TelegramFileClient)
	if !ok {
		return b.sendText(ctx, msg.Chat.ID, tr(user.Language, "speech_unavailable"))
	}
	fileID, mimeType, fileSize, duration := speechFileInfo(msg)
	if fileID == "" {
		return b.sendText(ctx, msg.Chat.ID, tr(user.Language, "speech_failed"))
	}
	if fileSize > maxSpeechSourceBytes || duration > maxSpeechDurationSeconds {
		return b.sendText(ctx, msg.Chat.ID, tr(user.Language, "speech_too_large"))
	}
	if err := b.sendText(ctx, msg.Chat.ID, tr(user.Language, "speech_processing")); err != nil {
		return err
	}
	file, err := fileClient.GetFile(ctx, fileID)
	if err != nil {
		b.logger.Printf("speech: telegram get file failed user=%d: %v", user.TelegramID, err)
		return b.sendText(ctx, msg.Chat.ID, tr(user.Language, "speech_failed"))
	}
	if file.FileSize > maxSpeechSourceBytes {
		return b.sendText(ctx, msg.Chat.ID, tr(user.Language, "speech_too_large"))
	}
	audio, err := fileClient.DownloadFile(ctx, file.FilePath, maxSpeechSourceBytes)
	if err != nil {
		b.logger.Printf("speech: telegram download failed user=%d: %v", user.TelegramID, err)
		return b.sendText(ctx, msg.Chat.ID, tr(user.Language, "speech_failed"))
	}
	text, err := b.speechRecognizer.Transcribe(ctx, nlu.SpeechRequest{
		Audio:    audio,
		MIMEType: mimeType,
		Language: user.Language,
	})
	if err != nil {
		b.logger.Printf("speech: transcription failed user=%d: %v", user.TelegramID, err)
		return b.sendText(ctx, msg.Chat.ID, tr(user.Language, "speech_failed"))
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return b.sendText(ctx, msg.Chat.ID, tr(user.Language, "speech_failed"))
	}
	if err := b.sendText(ctx, msg.Chat.ID, tr(user.Language, "speech_recognized", text)); err != nil {
		return err
	}
	return b.HandleMessage(ctx, &telegram.Message{
		From: msg.From,
		Chat: msg.Chat,
		Text: text,
	})
}

func speechFileInfo(msg *telegram.Message) (fileID, mimeType string, fileSize int64, duration int) {
	if msg == nil {
		return "", "", 0, 0
	}
	if msg.Voice != nil {
		mimeType = strings.TrimSpace(msg.Voice.MIMEType)
		if mimeType == "" {
			mimeType = "audio/ogg"
		}
		return msg.Voice.FileID, mimeType, msg.Voice.FileSize, msg.Voice.Duration
	}
	if msg.Audio != nil {
		mimeType = strings.TrimSpace(msg.Audio.MIMEType)
		if mimeType == "" {
			mimeType = "audio/mpeg"
		}
		return msg.Audio.FileID, mimeType, msg.Audio.FileSize, msg.Audio.Duration
	}
	return "", "", 0, 0
}
