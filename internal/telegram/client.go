package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(token string) *Client {
	return &Client{
		baseURL: fmt.Sprintf("https://api.telegram.org/bot%s", token),
		httpClient: &http.Client{
			Timeout: 70 * time.Second,
		},
	}
}

type APIResponse[T any] struct {
	OK          bool   `json:"ok"`
	Result      T      `json:"result"`
	ErrorCode   int    `json:"error_code,omitempty"`
	Description string `json:"description,omitempty"`
}

type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message,omitempty"`
	CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
}

type Message struct {
	MessageID int64  `json:"message_id"`
	From      User   `json:"from"`
	Chat      Chat   `json:"chat"`
	Date      int64  `json:"date"`
	Text      string `json:"text"`
}

type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name,omitempty"`
	Username  string `json:"username,omitempty"`
}

type Chat struct {
	ID int64 `json:"id"`
}

type ReplyMarkup struct {
	Keyboard        [][]KeyboardButton       `json:"keyboard,omitempty"`
	InlineKeyboard  [][]InlineKeyboardButton `json:"inline_keyboard,omitempty"`
	ResizeKeyboard  bool                     `json:"resize_keyboard,omitempty"`
	OneTimeKeyboard bool                     `json:"one_time_keyboard,omitempty"`
}

type KeyboardButton struct {
	Text string `json:"text"`
}

type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message,omitempty"`
	Data    string   `json:"data,omitempty"`
}

type SendMessageRequest struct {
	ChatID      int64        `json:"chat_id"`
	Text        string       `json:"text"`
	ReplyMarkup *ReplyMarkup `json:"reply_markup,omitempty"`
}

type SendPhotoRequest struct {
	ChatID      int64        `json:"chat_id"`
	PhotoName   string       `json:"photo_name,omitempty"`
	Photo       []byte       `json:"-"`
	Caption     string       `json:"caption,omitempty"`
	ReplyMarkup *ReplyMarkup `json:"reply_markup,omitempty"`
}

type AnswerCallbackQueryRequest struct {
	CallbackQueryID string `json:"callback_query_id"`
	Text            string `json:"text,omitempty"`
	ShowAlert       bool   `json:"show_alert,omitempty"`
}

func (c *Client) GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]Update, error) {
	params := url.Values{}
	params.Set("offset", fmt.Sprintf("%d", offset))
	params.Set("timeout", fmt.Sprintf("%d", timeoutSec))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/getUpdates?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var apiResp APIResponse[[]Update]
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}
	if !apiResp.OK {
		return nil, fmt.Errorf("telegram getUpdates error: %d %s", apiResp.ErrorCode, apiResp.Description)
	}

	return apiResp.Result, nil
}

func (c *Client) SendMessage(ctx context.Context, reqBody SendMessageRequest) error {
	data, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/sendMessage", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var apiResp APIResponse[json.RawMessage]
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return err
	}
	if !apiResp.OK {
		return fmt.Errorf("telegram sendMessage error: %d %s", apiResp.ErrorCode, apiResp.Description)
	}

	return nil
}

func (c *Client) SendPhoto(ctx context.Context, reqBody SendPhotoRequest) error {
	if len(reqBody.Photo) == 0 {
		return fmt.Errorf("telegram sendPhoto error: empty photo")
	}
	if reqBody.PhotoName == "" {
		reqBody.PhotoName = "schedule.png"
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("chat_id", fmt.Sprintf("%d", reqBody.ChatID)); err != nil {
		return err
	}
	if reqBody.Caption != "" {
		if err := writer.WriteField("caption", reqBody.Caption); err != nil {
			return err
		}
	}
	if reqBody.ReplyMarkup != nil {
		data, err := json.Marshal(reqBody.ReplyMarkup)
		if err != nil {
			return err
		}
		if err := writer.WriteField("reply_markup", string(data)); err != nil {
			return err
		}
	}
	file, err := writer.CreateFormFile("photo", reqBody.PhotoName)
	if err != nil {
		return err
	}
	if _, err := file.Write(reqBody.Photo); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/sendPhoto", &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var apiResp APIResponse[json.RawMessage]
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return err
	}
	if !apiResp.OK {
		return fmt.Errorf("telegram sendPhoto error: %d %s", apiResp.ErrorCode, apiResp.Description)
	}
	return nil
}

func (c *Client) AnswerCallbackQuery(ctx context.Context, reqBody AnswerCallbackQueryRequest) error {
	data, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/answerCallbackQuery", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var apiResp APIResponse[json.RawMessage]
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return err
	}
	if !apiResp.OK {
		return fmt.Errorf("telegram answerCallbackQuery error: %d %s", apiResp.ErrorCode, apiResp.Description)
	}
	return nil
}
