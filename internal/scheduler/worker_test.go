package scheduler

import "testing"

func TestParseDailyTime_Default(t *testing.T) {
	hour, minute := parseDailyTime("")
	if hour != 22 || minute != 0 {
		t.Fatalf("expected 22:00, got %02d:%02d", hour, minute)
	}
}

func TestParseDailyTime_Valid(t *testing.T) {
	hour, minute := parseDailyTime("07:35")
	if hour != 7 || minute != 35 {
		t.Fatalf("expected 07:35, got %02d:%02d", hour, minute)
	}
}

func TestParseDailyTime_InvalidFallback(t *testing.T) {
	hour, minute := parseDailyTime("abc")
	if hour != 22 || minute != 0 {
		t.Fatalf("expected fallback 22:00, got %02d:%02d", hour, minute)
	}
}

func TestParseWeekday(t *testing.T) {
	if got := parseWeekday("mon"); got != 1 {
		t.Fatalf("expected monday=1, got %d", got)
	}
	if got := parseWeekday("sun"); got != 0 {
		t.Fatalf("expected sunday=0, got %d", got)
	}
	if got := parseWeekday("bad"); got != 1 {
		t.Fatalf("expected fallback monday=1, got %d", got)
	}
}
