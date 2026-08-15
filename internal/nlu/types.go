package nlu

import (
	"context"
	"time"
)

type Service struct {
	Index       int
	Name        string
	Category    string
	Subcategory string
	Description string
	AdminName   string
	DurationMin int
}

type BookingIntentRequest struct {
	Text     string
	Language string
	Now      time.Time
	Timezone string
	Services []Service
}

type BookingIntent struct {
	IsBooking      bool     `json:"is_booking"`
	ServiceIndexes []int    `json:"service_indexes"`
	ServiceQueries []string `json:"service_queries"`
	DurationMin    int      `json:"duration_min"`
	DateFrom       string   `json:"date_from"`
	DateTo         string   `json:"date_to"`
	Period         string   `json:"period"`
	Confidence     float64  `json:"confidence"`
}

type BookingIntentParser interface {
	ParseBookingIntent(ctx context.Context, req BookingIntentRequest) (BookingIntent, error)
}

type SpeechRequest struct {
	Audio    []byte
	MIMEType string
	Language string
}

type SpeechRecognizer interface {
	Transcribe(ctx context.Context, req SpeechRequest) (string, error)
}

type ImageTextRequest struct {
	Image    []byte
	MIMEType string
	Language string
}

type ImageTextRecognizer interface {
	RecognizeText(ctx context.Context, req ImageTextRequest) (string, error)
}
