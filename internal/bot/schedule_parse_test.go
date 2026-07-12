package bot

import (
	"reflect"
	"testing"
	"time"
)

func TestParseWeekdays(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []time.Weekday
	}{
		{
			name: "russian range",
			raw:  "пн-пт",
			want: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
		},
		{
			name: "english list deduplicates",
			raw:  "mon,wed,mon",
			want: []time.Weekday{time.Monday, time.Wednesday},
		},
		{
			name: "numeric weekend range",
			raw:  "6-7",
			want: []time.Weekday{time.Saturday, time.Sunday},
		},
		{
			name: "russian full plural",
			raw:  "четверги",
			want: []time.Weekday{time.Thursday},
		},
		{
			name: "russian full list",
			raw:  "среда,пятницы",
			want: []time.Weekday{time.Wednesday, time.Friday},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseWeekdays(tt.raw)
			if err != nil {
				t.Fatalf("parseWeekdays(%q) error: %v", tt.raw, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseWeekdays(%q) = %#v, want %#v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestParseWeekdaysRejectsBadValue(t *testing.T) {
	if _, err := parseWeekdays("пн,xxx"); err == nil {
		t.Fatal("expected error for bad weekday")
	}
}

func TestParseDayRange(t *testing.T) {
	start, end, err := parseDayRange("10:30-18:00")
	if err != nil {
		t.Fatalf("parseDayRange error: %v", err)
	}
	if start != 10*time.Hour+30*time.Minute {
		t.Fatalf("start = %s, want 10h30m", start)
	}
	if end != 18*time.Hour {
		t.Fatalf("end = %s, want 18h", end)
	}
}

func TestParseDayRangeRejectsBadClock(t *testing.T) {
	if _, _, err := parseDayRange("10:00-25:00"); err == nil {
		t.Fatal("expected error for bad clock")
	}
}
