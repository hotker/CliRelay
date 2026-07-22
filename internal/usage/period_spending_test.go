package usage

import (
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestPeriodWindowKeysAtUsesProjectTimezoneMondayWeek(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*60*60)
	now := time.Date(2026, 7, 22, 12, 30, 0, 0, time.UTC)
	got := PeriodWindowKeysAt(now, loc)
	if got.WeekFrom != "2026-07-20" || got.MonthFrom != "2026-07-01" || got.DayTo != "2026-07-23" {
		t.Fatalf("windows = %+v, want Monday 2026-07-20, month 2026-07-01, tomorrow 2026-07-23", got)
	}
	if got.FiveHourFrom != "2026-07-22T07:30" || got.FiveHourTo != "2026-07-22T12:31" {
		t.Fatalf("5h windows = [%s,%s), want [2026-07-22T07:30,2026-07-22T12:31)", got.FiveHourFrom, got.FiveHourTo)
	}
}

func TestQueryPeriodSpendingWeekAndFiveHourBoundaries(t *testing.T) {
	initTestUsageDB(t, config.RequestLogStorageConfig{})
	db := getDB()
	now := time.Date(2026, 7, 22, 12, 30, 45, 0, time.UTC)
	keyID := "period-key"
	insert := func(kind, start string, cost float64) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO usage_rollup_buckets
			(tenant_id,bucket_kind,bucket_start,api_key_id,cost_total,updated_at)
			VALUES (?,?,?,?,?,?)`, systemTenantID, kind, start, keyID, cost, now); err != nil {
			t.Fatalf("insert %s %s: %v", kind, start, err)
		}
	}
	insert(rollupBucketDay, "2026-07-19", 100) // previous Sunday: excluded from week
	insert(rollupBucketDay, "2026-07-20", 10)  // Monday inclusive
	insert(rollupBucketDay, "2026-07-22", 5)
	insert(rollupBucketDay, "2026-07-23", 20) // tomorrow exclusive
	insert(rollupBucketQuotaMinuteUTC, "2026-07-22T07:29", 100)
	insert(rollupBucketQuotaMinuteUTC, "2026-07-22T07:30", 3)   // cutoff minute included
	insert(rollupBucketQuotaMinuteUTC, "2026-07-22T12:30", 4)   // current minute included
	insert(rollupBucketQuotaMinuteUTC, "2026-07-22T12:31", 100) // exclusive upper bound
	insert(rollupBucketLifetime, rollupLifetimeStart, 999)

	got, err := QueryPeriodSpendingByAPIKeyIDsForTenantAt(systemTenantID, []string{keyID}, now)
	if err != nil {
		t.Fatalf("QueryPeriodSpending: %v", err)
	}
	used := got[keyID]
	if used.FiveHour != 7 || used.Day != 5 || used.Week != 15 || used.Month != 115 || used.Lifetime != 999 {
		t.Fatalf("used = %+v, want 5h=7 day=5 week=15 month=115 lifetime=999", used)
	}
}

func TestFiveHourQuotaProjectionReadinessRequiresFullCoverage(t *testing.T) {
	initTestUsageDB(t, config.RequestLogStorageConfig{})
	db := getDB()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	ensureUsageProjectionMarkerTable(db)
	set := func(start time.Time) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO usage_projection_markers(marker_key,marker_value,updated_at)
			VALUES(?,?,?) ON CONFLICT(marker_key) DO UPDATE SET marker_value=excluded.marker_value,updated_at=excluded.updated_at`,
			quotaMinuteCoverageStartMarker, start.Format(time.RFC3339), now); err != nil {
			t.Fatalf("set marker: %v", err)
		}
	}
	set(now.Add(-4*time.Hour - 59*time.Minute))
	if FiveHourQuotaProjectionReadyAt(now) {
		t.Fatal("projection should still be warming before five hours")
	}
	set(now.Add(-5 * time.Hour))
	if !FiveHourQuotaProjectionReadyAt(now) {
		t.Fatal("projection should be ready at full five-hour coverage")
	}
}

func TestRollupBucketStartsProjectsQuotaMinuteInUTC(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*60*60)
	at := time.Date(2026, 7, 22, 23, 59, 30, 0, loc)
	starts := rollupBucketStarts(at, loc)
	if starts[rollupBucketMinute] != "2026-07-22T23:59" {
		t.Fatalf("local minute = %q", starts[rollupBucketMinute])
	}
	if starts[rollupBucketQuotaMinuteUTC] != "2026-07-22T15:59" {
		t.Fatalf("quota UTC minute = %q, want 2026-07-22T15:59", starts[rollupBucketQuotaMinuteUTC])
	}
}
