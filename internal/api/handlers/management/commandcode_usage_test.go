package management

import (
	"encoding/json"
	"testing"
	"time"
)

func decodeCommandCodeCredits(t *testing.T, raw string) commandCodeCreditsResponse {
	t.Helper()
	var payload commandCodeCreditsResponse
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode credits payload: %v", err)
	}
	return payload
}

func TestParseCommandCodeUsageReportsBothWindows(t *testing.T) {
	resetAt := time.Now().Add(90 * time.Minute).UnixMilli()
	raw := `{"windowLimits":{"fiveHour":{"cap":200,"used":50,"resetAt":` +
		itoa(resetAt) + `},"weekly":{"cap":1000,"used":900,"resetAt":` + itoa(resetAt) + `}}}`

	items := parseCommandCodeUsage(decodeCommandCodeCredits(t, raw))
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	if items[0].Type != "five_hour" || items[0].Percentage != 25 {
		t.Fatalf("five hour window = %#v, want 25%%", items[0])
	}
	if items[1].Type != "weekly" || items[1].Percentage != 90 {
		t.Fatalf("weekly window = %#v, want 90%%", items[1])
	}
	if items[0].ResetsIn == "" {
		t.Fatal("expected a reset countdown for a future resetAt")
	}
}

// A zero or missing cap carries no ratio. Reporting it as 0% would read as
// "plenty of quota left" on an account that may have none.
func TestParseCommandCodeUsageSkipsWindowsWithoutCap(t *testing.T) {
	raw := `{"windowLimits":{"fiveHour":{"cap":0,"used":10},"weekly":{"used":5}}}`
	if items := parseCommandCodeUsage(decodeCommandCodeCredits(t, raw)); len(items) != 0 {
		t.Fatalf("items = %#v, want none", items)
	}
}

func TestParseCommandCodeUsageClampsOverage(t *testing.T) {
	raw := `{"windowLimits":{"weekly":{"cap":100,"used":150}}}`
	items := parseCommandCodeUsage(decodeCommandCodeCredits(t, raw))
	if len(items) != 1 || items[0].Percentage != 100 {
		t.Fatalf("items = %#v, want a single 100%% entry", items)
	}
}

// resetAt has been observed in both units, so both must produce the same answer.
func TestCommandCodeResetInAcceptsSecondsAndMilliseconds(t *testing.T) {
	future := time.Now().Add(2 * time.Hour)
	milliseconds := float64(future.UnixMilli())
	seconds := float64(future.Unix())

	fromMillis := commandCodeResetIn(&milliseconds)
	fromSeconds := commandCodeResetIn(&seconds)
	if fromMillis == "" || fromSeconds == "" {
		t.Fatalf("reset countdowns = %q / %q, want both populated", fromMillis, fromSeconds)
	}
	if fromMillis != fromSeconds {
		t.Fatalf("seconds and milliseconds disagree: %q vs %q", fromSeconds, fromMillis)
	}
}

func TestCommandCodeResetInHandlesPastAndMissing(t *testing.T) {
	if got := commandCodeResetIn(nil); got != "" {
		t.Fatalf("missing resetAt = %q, want empty", got)
	}
	past := float64(time.Now().Add(-time.Hour).UnixMilli())
	if got := commandCodeResetIn(&past); got != "0 seconds" {
		t.Fatalf("expired window = %q, want a floored countdown", got)
	}
}

func itoa(value int64) string {
	return string(mustJSON(value))
}

func mustJSON(value any) []byte {
	out, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return out
}
