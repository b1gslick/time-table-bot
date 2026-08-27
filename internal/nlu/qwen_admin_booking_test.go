package nlu

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestQwenParserParsesAdminBookingIntent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req qwenChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.ResponseFormat == nil || req.ResponseFormat.Type != "json_object" {
			t.Fatalf("response format = %#v", req.ResponseFormat)
		}
		if req.EnableThinking == nil || *req.EnableThinking {
			t.Fatalf("enable_thinking = %#v, want false", req.EnableThinking)
		}
		if req.MaxCompletionTokens != 0 {
			t.Fatalf("max_completion_tokens = %d, want omitted", req.MaxCompletionTokens)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"is_create_booking\":true,\"contact_type\":\"telegram\",\"contact\":\"@client\",\"service_indexes\":[1],\"service_queries\":[\"эпиляция\"],\"duration_min\":90,\"start_at\":\"2026-08-16T18:00:00+03:00\",\"confidence\":0.96}"}}]}`))
	}))
	defer server.Close()
	parser, err := NewQwenParser(QwenConfig{APIKey: "test-key", BaseURL: server.URL, Model: "qwen-test"})
	if err != nil {
		t.Fatalf("NewQwenParser: %v", err)
	}
	intent, err := parser.ParseAdminBookingIntent(context.Background(), AdminBookingIntentRequest{
		Text: "запиши @client на эпиляцию завтра в 18:00",
		Now:  time.Date(2026, 8, 15, 10, 0, 0, 0, time.FixedZone("test", 3*60*60)),
		Services: []Service{{
			Index: 1,
			Name:  "Эпиляция",
		}},
	})
	if err != nil {
		t.Fatalf("ParseAdminBookingIntent: %v", err)
	}
	if !intent.IsCreateBooking || intent.Contact != "@client" || intent.StartAt != "2026-08-16T18:00:00+03:00" {
		t.Fatalf("intent = %#v", intent)
	}
}

func TestQwenParserParsesScheduleImport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req qwenChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.MaxCompletionTokens != 4000 {
			t.Fatalf("max_completion_tokens = %d", req.MaxCompletionTokens)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"is_schedule\":true,\"entries\":[{\"client\":\"Лиза\",\"service_indexes\":[1],\"service_queries\":[\"электро\"],\"duration_min\":60,\"start_at\":\"2026-08-17T09:30:00+03:00\",\"confidence\":0.95}],\"confidence\":0.93}"}}]}`))
	}))
	defer server.Close()
	parser, err := NewQwenParser(QwenConfig{APIKey: "test-key", BaseURL: server.URL, Model: "qwen-test"})
	if err != nil {
		t.Fatalf("NewQwenParser: %v", err)
	}
	intent, err := parser.ParseAdminScheduleImport(context.Background(), AdminScheduleImportRequest{
		Text: "Авг. 2026, неделя 34\nпн 17\n9:30 Лиза\nэлектро 1 час",
		Now:  time.Date(2026, 8, 15, 10, 0, 0, 0, time.FixedZone("test", 3*60*60)),
		Services: []Service{{
			Index: 1,
			Name:  "Эпиляция электро",
		}},
	})
	if err != nil {
		t.Fatalf("ParseAdminScheduleImport: %v", err)
	}
	if !intent.IsSchedule || len(intent.Entries) != 1 || intent.Entries[0].Client != "Лиза" {
		t.Fatalf("intent = %#v", intent)
	}
}

func TestQwenParserParsesScheduleEditAsPatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req qwenChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(req.Messages) != 2 || !strings.Contains(req.Messages[1].Content, "Current appointment:") || !strings.Contains(req.Messages[1].Content, "перенеси на пятницу") {
			t.Fatalf("schedule edit prompt = %#v", req.Messages)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"is_edit\":true,\"change_client\":false,\"client\":\"\",\"contact_type\":\"unknown\",\"contact\":\"\",\"change_service\":true,\"services\":[{\"service_indexes\":[1],\"service_queries\":[\"электро на час\"],\"duration_min\":60}],\"service_indexes\":[],\"service_queries\":[],\"duration_min\":0,\"change_start_at\":true,\"start_at\":\"2026-08-21T10:30:00+03:00\",\"confidence\":0.97}"}}]}`))
	}))
	defer server.Close()
	parser, err := NewQwenParser(QwenConfig{APIKey: "test-key", BaseURL: server.URL, Model: "qwen-test"})
	if err != nil {
		t.Fatalf("NewQwenParser: %v", err)
	}
	intent, err := parser.ParseAdminScheduleEdit(context.Background(), AdminScheduleEditRequest{
		Text:          "перенеси на пятницу в 10:30 и поставь электро на час",
		Now:           time.Date(2026, 8, 16, 12, 0, 0, 0, time.FixedZone("test", 3*60*60)),
		CurrentClient: "Катя", CurrentStartAt: "2026-08-18T12:00:00+03:00",
		CurrentServices: []string{"Бикини"},
		Services:        []Service{{Index: 1, Name: "1 час", Category: "Электроэпиляция", DurationMin: 60}},
	})
	if err != nil {
		t.Fatalf("ParseAdminScheduleEdit: %v", err)
	}
	if !intent.IsEdit || intent.ChangeClient || !intent.ChangeService || !intent.ChangeStartAt || intent.StartAt != "2026-08-21T10:30:00+03:00" || len(intent.Services) != 1 || len(intent.Services[0].ServiceIndexes) != 1 {
		t.Fatalf("intent = %#v", intent)
	}
}

func TestQwenParserParsesMonthlySchedulePlan(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req qwenChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if len(req.Messages) != 2 || !strings.Contains(req.Messages[0].Content, "Monday=1") || !strings.Contains(req.Messages[1].Content, "следующий месяц") {
			t.Fatalf("schedule plan prompt = %#v", req.Messages)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"is_schedule_plan\":true,\"target_month\":\"2026-09\",\"copy_from_month\":\"\",\"rules\":[{\"weekdays\":[1,2,3,4,5],\"start\":\"10:00\",\"end\":\"17:00\"}],\"extra_days\":[{\"date\":\"2026-09-05\",\"weekday\":6,\"start\":\"\",\"end\":\"\"}],\"closed_dates\":[],\"slot_duration_min\":0,\"confidence\":0.98}"}}]}`))
	}))
	defer server.Close()
	parser, err := NewQwenParser(QwenConfig{APIKey: "test-key", BaseURL: server.URL, Model: "qwen-test"})
	if err != nil {
		t.Fatal(err)
	}
	intent, err := parser.ParseAdminSchedulePlan(context.Background(), AdminSchedulePlanRequest{
		Text: "сделай следующий месяц по будням с 10 до 17 и субботу 5 рабочей",
		Now:  time.Date(2026, 8, 17, 10, 0, 0, 0, time.FixedZone("test", 3*60*60)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !intent.IsSchedulePlan || intent.TargetMonth != "2026-09" || len(intent.Rules) != 1 || len(intent.Rules[0].Weekdays) != 5 || len(intent.ExtraDays) != 1 {
		t.Fatalf("intent = %#v", intent)
	}
}

func TestNormalizeScheduleEditServiceDurationRepairsCombination(t *testing.T) {
	change := AdminScheduleEditService{
		ServiceIndexes: []int{1, 2}, ServiceQueries: []string{"электроэпиляция на полтора часа"}, DurationMin: 90,
	}
	normalizeScheduleEditServiceDuration(&change, []Service{
		{Index: 1, Category: "Электроэпиляция", Name: "До 30 мин", DurationMin: 30},
		{Index: 2, Category: "Электроэпиляция", Name: "30 мин", DurationMin: 30},
		{Index: 3, Category: "Электроэпиляция", Name: "1 час", DurationMin: 60},
		{Index: 4, Category: "Восковая депиляция", Name: "Бикини классика", DurationMin: 15},
	})
	if len(change.ServiceIndexes) != 2 || change.ServiceIndexes[0] != 1 || change.ServiceIndexes[1] != 3 {
		t.Fatalf("service combination = %v, want [1 3]", change.ServiceIndexes)
	}
}

func TestQwenScheduleEditUsesExplicitCombinedDuration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"is_edit\":true,\"change_service\":true,\"services\":[{\"service_indexes\":[1,2],\"service_queries\":[\"Электроэпиляция на 1 час 30 минут\"],\"duration_min\":60}],\"confidence\":0.98}"}}]}`))
	}))
	defer server.Close()
	parser, err := NewQwenParser(QwenConfig{APIKey: "test-key", BaseURL: server.URL, Model: "qwen-test"})
	if err != nil {
		t.Fatal(err)
	}
	intent, err := parser.ParseAdminScheduleEdit(context.Background(), AdminScheduleEditRequest{
		Text: "Услуги только, Электроэпиляция на 1 час 30 минут",
		Services: []Service{
			{Index: 1, Category: "Электроэпиляция", Name: "До 30 мин", DurationMin: 30},
			{Index: 2, Category: "Электроэпиляция", Name: "30 мин", DurationMin: 30},
			{Index: 3, Category: "Электроэпиляция", Name: "1 час", DurationMin: 60},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(intent.Services) != 1 || intent.Services[0].DurationMin != 90 || len(intent.Services[0].ServiceIndexes) != 2 || intent.Services[0].ServiceIndexes[1] != 3 || intent.Services[0].ServiceIndexes[0] < 1 || intent.Services[0].ServiceIndexes[0] > 2 {
		t.Fatalf("normalized services = %+v", intent.Services)
	}
}

func TestExplicitServiceDuration(t *testing.T) {
	for text, want := range map[string]int{
		"электро на полтора часа":            90,
		"электро на 1 час 30 минут":          90,
		"электро на 1,5 часа":                90,
		"электро на 90 минут":                90,
		"electrolysis for 1 hour 30 minutes": 90,
	} {
		got, ok := explicitServiceDuration(text)
		if !ok || got != want {
			t.Errorf("explicitServiceDuration(%q) = %d, %v; want %d", text, got, ok, want)
		}
	}
}

func TestQwenParserParsesServiceImport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req qwenChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.MaxCompletionTokens != 3000 {
			t.Fatalf("max_completion_tokens = %d", req.MaxCompletionTokens)
		}
		if len(req.Messages) != 2 || !strings.Contains(req.Messages[1].Content, "1. Восковая депиляция > Лицо > Усы") || !strings.Contains(req.Messages[0].Content, "change_price") {
			t.Fatalf("existing services missing from prompt: %#v", req.Messages)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"is_service_catalog\":true,\"entries\":[{\"category\":\"Электроэпиляция\",\"subcategory\":\"По времени\",\"name\":\"1,5 часа\",\"duration_min\":90,\"price_text\":\"60 €\",\"confidence\":0.97}],\"confidence\":0.96}"}}]}`))
	}))
	defer server.Close()
	parser, err := NewQwenParser(QwenConfig{APIKey: "test-key", BaseURL: server.URL, Model: "qwen-test"})
	if err != nil {
		t.Fatalf("NewQwenParser: %v", err)
	}
	intent, err := parser.ParseAdminServiceImport(context.Background(), AdminServiceImportRequest{
		Text:     "Добавь электроэпиляцию на полтора часа за 60 евро",
		Language: "ru",
		ExistingServices: []Service{{
			Category: "Восковая депиляция", Subcategory: "Лицо", Name: "Усы", DurationMin: 15,
		}},
	})
	if err != nil {
		t.Fatalf("ParseAdminServiceImport: %v", err)
	}
	if !intent.IsServiceCatalog || len(intent.Entries) != 1 || intent.Entries[0].DurationMin != 90 || intent.Entries[0].PriceText != "60 €" {
		t.Fatalf("intent = %#v", intent)
	}
}

func TestQwenParserParsesFinanceIntent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req qwenChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.MaxCompletionTokens != 2500 || !strings.Contains(req.Messages[1].Content, "Input source: image") {
			t.Fatalf("request = %#v", req)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"is_finance\":true,\"entries\":[{\"kind\":\"expense\",\"category\":\"supplies\",\"amount_cents\":8540,\"currency\":\"EUR\",\"occurred_at\":\"2026-08-15T12:00:00+03:00\",\"description\":\"receipt\",\"confidence\":0.97}],\"confidence\":0.96}"}}]}`))
	}))
	defer server.Close()
	parser, err := NewQwenParser(QwenConfig{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	intent, err := parser.ParseAdminFinanceIntent(context.Background(), AdminFinanceIntentRequest{
		Text: "ИТОГО 85.40 EUR", Language: "ru", Source: "image",
		Now: time.Date(2026, 8, 15, 12, 0, 0, 0, time.FixedZone("test", 3*60*60)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !intent.IsFinance || len(intent.Entries) != 1 || intent.Entries[0].AmountCents != 8540 || intent.Entries[0].Kind != "expense" {
		t.Fatalf("intent = %#v", intent)
	}
}

func TestAnnotatePlannerPanelsAssignsEdgeDatesToTimedLines(t *testing.T) {
	input := `[page width=591 height=1280]
[x=38 y=107 w=18 h=19] 15
[x=62 y=142 w=180 h=20] 9:30 Анастасия
[x=17 y=264 w=18 h=16] пн
[x=16 y=289 w=20 h=19] 17
[x=62 y=485 w=180 h=20] 9:30 Стефани
[x=19 y=607 w=17 h=16] BT
[x=15 y=631 w=20 h=19] 18
[x=62 y=828 w=180 h=20] 9:45 Марина
[x=17 y=950 w=18 h=16] cp
[x=15 y=974 w=20 h=19] 19`
	got := annotatePlannerPanels(input)
	for _, want := range []string{
		"[panel_date_day=17] [x=62 y=142",
		"[panel_date_day=18] [x=62 y=485",
		"[panel_date_day=19] [x=62 y=828",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("annotated text does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "panel_date_day=15") {
		t.Fatalf("header date was treated as panel date:\n%s", got)
	}
}

func TestNormalizeScheduleOCRTextCorrectsWax(t *testing.T) {
	got := normalizeScheduleOCRText("14:40 Кейт\nBOCK")
	if !strings.Contains(got, "воск") || strings.Contains(got, "BOCK") {
		t.Fatalf("normalized OCR = %q", got)
	}
}
