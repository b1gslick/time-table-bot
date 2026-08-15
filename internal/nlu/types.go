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

type AdminBookingIntentRequest struct {
	Text     string
	Language string
	Now      time.Time
	Timezone string
	Services []Service
}

type AdminBookingIntent struct {
	IsCreateBooking bool     `json:"is_create_booking"`
	ContactType     string   `json:"contact_type"`
	Contact         string   `json:"contact"`
	ServiceIndexes  []int    `json:"service_indexes"`
	ServiceQueries  []string `json:"service_queries"`
	DurationMin     int      `json:"duration_min"`
	StartAt         string   `json:"start_at"`
	Confidence      float64  `json:"confidence"`
}

type AdminBookingIntentParser interface {
	ParseAdminBookingIntent(ctx context.Context, req AdminBookingIntentRequest) (AdminBookingIntent, error)
}

type AdminScheduleImportRequest struct {
	Text     string
	Language string
	Now      time.Time
	Timezone string
	Services []Service
}

type AdminScheduleImportEntry struct {
	Client         string   `json:"client"`
	ServiceIndexes []int    `json:"service_indexes"`
	ServiceQueries []string `json:"service_queries"`
	DurationMin    int      `json:"duration_min"`
	StartAt        string   `json:"start_at"`
	Confidence     float64  `json:"confidence"`
}

type AdminScheduleImportIntent struct {
	IsSchedule bool                       `json:"is_schedule"`
	Entries    []AdminScheduleImportEntry `json:"entries"`
	Confidence float64                    `json:"confidence"`
}

type AdminScheduleImportParser interface {
	ParseAdminScheduleImport(ctx context.Context, req AdminScheduleImportRequest) (AdminScheduleImportIntent, error)
}

type AdminServiceImportRequest struct {
	Text             string
	Language         string
	ExistingServices []Service
}

type AdminServiceImportEntry struct {
	Category    string  `json:"category"`
	Subcategory string  `json:"subcategory"`
	Name        string  `json:"name"`
	DurationMin int     `json:"duration_min"`
	PriceText   string  `json:"price_text"`
	Confidence  float64 `json:"confidence"`
}

type AdminServiceImportIntent struct {
	IsServiceCatalog bool                      `json:"is_service_catalog"`
	Entries          []AdminServiceImportEntry `json:"entries"`
	Confidence       float64                   `json:"confidence"`
}

type AdminServiceImportParser interface {
	ParseAdminServiceImport(ctx context.Context, req AdminServiceImportRequest) (AdminServiceImportIntent, error)
}

type AdminFinanceIntentRequest struct {
	Text       string
	Language   string
	Now        time.Time
	Timezone   string
	Source     string
	ForcedKind string
}

type AdminFinanceIntentEntry struct {
	Kind        string  `json:"kind"`
	Category    string  `json:"category"`
	AmountCents int64   `json:"amount_cents"`
	Currency    string  `json:"currency"`
	OccurredAt  string  `json:"occurred_at"`
	Description string  `json:"description"`
	Confidence  float64 `json:"confidence"`
}

type AdminFinanceIntent struct {
	IsFinance  bool                      `json:"is_finance"`
	Entries    []AdminFinanceIntentEntry `json:"entries"`
	Confidence float64                   `json:"confidence"`
}

type AdminFinanceIntentParser interface {
	ParseAdminFinanceIntent(ctx context.Context, req AdminFinanceIntentRequest) (AdminFinanceIntent, error)
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

// ImageLayoutTextRecognizer returns both readable text and OCR lines annotated
// with their image coordinates. Schedule imports use the layout form.
type ImageLayoutTextRecognizer interface {
	RecognizeTextWithLayout(ctx context.Context, req ImageTextRequest) (text, layoutText string, err error)
}
