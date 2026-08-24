package usage

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

const aiAccountSubjectCycleRolloverRepairMarker = "ai_account_subject_cycle_rollover_repair_v1"

type meteredWeeklyQuotaProbe struct {
	resetAt       time.Time
	windowSeconds int64
	recordedAt    time.Time
}

type aiAccountSubjectCycleTotals struct {
	requests   int64
	successes  int64
	failures   int64
	cost       float64
	tokens     int64
	firstEvent time.Time
	lastEvent  time.Time
}

// RunAIAccountSubjectCycleRolloverRepairAtInit re-derives anchors that were held
// on a period the upstream had already ended, and rebuilds the usage bucket of
// the period they should have rolled into.
//
// Anchoring refused to leave a stored period before its own reset, to stop an
// idle quota bucket's "a full window remaining" countdown from re-keying the
// usage bucket on every probe. That also blocked the legitimate case: an upstream
// may end a period early. Production had a Codex account whose code_week window
// reported 41% spent against a reset on the 27th, then a full allowance against a
// reset on the 31st — a real new period, four days before the stored one was due.
// The anchor stayed on the old period, so the card kept showing the previous
// week's start while the new week's requests were added to the previous week's
// bucket: 12735 requests and $702 against a window the upstream said was 1% used.
//
// Anchoring now rolls on a metering probe, so no new anchor freezes. This repairs
// the ones already on disk rather than leaving operators to wait out a window
// whose totals stay wrong until it ends.
func RunAIAccountSubjectCycleRolloverRepairAtInit() error {
	return runAIAccountSubjectCycleRolloverRepairDB(getDB())
}

func runAIAccountSubjectCycleRolloverRepairDB(db *sql.DB) error {
	if db == nil {
		return nil
	}

	usageProjectionMu.Lock()
	defer usageProjectionMu.Unlock()

	ensureUsageProjectionMarkerTable(db)
	if projectionMarkerValue(db, aiAccountSubjectCycleRolloverRepairMarker) == rollupMarkerDone {
		return nil
	}

	cyclesBySubject, err := loadAIAccountSubjectWeeklyCyclesBySubject(db)
	if err != nil {
		return err
	}
	probes, err := loadLatestMeteredWeeklyQuotaProbes(db)
	if err != nil {
		return err
	}

	subjectIDs := make([]string, 0, len(cyclesBySubject))
	for subjectID := range cyclesBySubject {
		subjectIDs = append(subjectIDs, subjectID)
	}
	sort.Strings(subjectIDs)

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("usage: begin shared subject cycle rollover repair: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	repaired := 0
	for _, subjectID := range subjectIDs {
		cycles := cyclesBySubject[subjectID]
		before, hadBefore := selectAIAccountSubjectWeeklyCycle(cycles)

		changed := false
		for i, cycle := range cycles {
			probe, ok := probes[aiAccountSubjectQuotaProbeKey(subjectID, cycle.QuotaKey)]
			if !ok {
				continue
			}
			rolled, ok := aiAccountSubjectCycleRolloverFromProbe(cycle, probe)
			if !ok {
				continue
			}
			if err := writeAIAccountSubjectQuotaCycleAnchorTx(tx, rolled); err != nil {
				return err
			}
			cycles[i] = rolled
			changed = true
		}
		if !changed || !hadBefore {
			continue
		}
		after, hadAfter := selectAIAccountSubjectWeeklyCycle(cycles)
		if !hadAfter || !after.CycleStartAt.After(before.CycleStartAt) {
			continue
		}
		moved, err := repairAIAccountSubjectCycleBucketsTx(tx, subjectID, before.CycleStartAt, after.CycleStartAt, now)
		if err != nil {
			return err
		}
		repaired++
		log.WithFields(log.Fields{
			"auth_subject_id": subjectID,
			"from":            before.CycleStartAt.Format(time.RFC3339),
			"to":              after.CycleStartAt.Format(time.RFC3339),
			"moved_requests":  moved,
		}).Info("usage: repaired a cycle anchor held on an ended period")
	}

	if _, err := tx.Exec(`
		INSERT INTO usage_projection_markers (marker_key, marker_value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(marker_key) DO UPDATE SET
			marker_value = excluded.marker_value,
			updated_at = excluded.updated_at
	`, aiAccountSubjectCycleRolloverRepairMarker, rollupMarkerDone, now.Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("usage: mark shared subject cycle rollover repair: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("usage: commit shared subject cycle rollover repair: %w", err)
	}
	if repaired > 0 {
		// The projection keys its buckets off this cache; drop it so the next write
		// re-reads the anchors this pass rolled.
		resetAIAccountSubjectCycleCache()
	}
	return nil
}

func aiAccountSubjectQuotaProbeKey(subjectID, quotaKey string) string {
	return subjectID + "\x00" + quotaKey
}

// loadLatestMeteredWeeklyQuotaProbes keeps only probes that reported consumption.
// A window at a full allowance carries no evidence about where its period began,
// which is exactly the countdown the anchor must not follow.
func loadLatestMeteredWeeklyQuotaProbes(db *sql.DB) (map[string]meteredWeeklyQuotaProbe, error) {
	rows, err := db.Query(`
		SELECT auth_subject_id, quota_key, reset_at, window_seconds, recorded_at
		FROM ai_account_subject_quota_points
		WHERE window_seconds >= ? AND percent IS NOT NULL AND percent < 100
		ORDER BY recorded_at ASC
	`, aiAccountSubjectWeeklyWindowSeconds)
	if err != nil {
		return nil, fmt.Errorf("usage: query metered weekly quota probes: %w", err)
	}
	defer rows.Close()

	out := make(map[string]meteredWeeklyQuotaProbe)
	for rows.Next() {
		var subjectID, quotaKey string
		var reset, recorded storedTime
		var windowSeconds int64
		if err := rows.Scan(&subjectID, &quotaKey, &reset, &windowSeconds, &recorded); err != nil {
			return nil, fmt.Errorf("usage: scan metered weekly quota probe: %w", err)
		}
		subjectID = strings.TrimSpace(subjectID)
		quotaKey = strings.TrimSpace(quotaKey)
		if subjectID == "" || quotaKey == "" || !reset.Valid || !recorded.Valid || windowSeconds <= 0 {
			continue
		}
		// Ascending order means the last row written per key is the newest one.
		out[aiAccountSubjectQuotaProbeKey(subjectID, quotaKey)] = meteredWeeklyQuotaProbe{
			resetAt:       reset.Time.UTC(),
			windowSeconds: windowSeconds,
			recordedAt:    recorded.Time.UTC(),
		}
	}
	return out, rows.Err()
}

// aiAccountSubjectCycleRolloverFromProbe applies the live anchoring rule to a
// stored row, so the repair and the running server cannot disagree about which
// period an account is in.
func aiAccountSubjectCycleRolloverFromProbe(
	stored AIAccountSubjectQuotaCycle,
	probe meteredWeeklyQuotaProbe,
) (AIAccountSubjectQuotaCycle, bool) {
	if stored.CycleStartAt.IsZero() || probe.windowSeconds != stored.WindowSeconds {
		return stored, false
	}
	incoming := stored
	incoming.CycleStartAt = probe.resetAt.Add(-time.Duration(probe.windowSeconds) * time.Second)
	incoming.ResetAt = probe.resetAt
	incoming.LastVerifiedAt = probe.recordedAt
	// Only ever forward: a probe older than the stored anchor must not rewind a
	// period the server has already rolled into.
	if !incoming.CycleStartAt.After(stored.CycleStartAt) {
		return stored, false
	}
	if sameAIAccountSubjectCycle(incoming.CycleStartAt, stored.CycleStartAt, stored.WindowSeconds) {
		return stored, false
	}
	if !aiAccountSubjectCycleRollover(incoming, stored.ResetAt, true) {
		return stored, false
	}
	return incoming, true
}

func writeAIAccountSubjectQuotaCycleAnchorTx(tx *sql.Tx, cycle AIAccountSubjectQuotaCycle) error {
	if _, err := tx.Exec(`
		UPDATE ai_account_subject_quota_cycles
		SET cycle_start_at = ?, reset_at = ?, last_verified_at = ?
		WHERE auth_subject_id = ? AND quota_key = ?
	`, cycle.CycleStartAt.UTC().Format(time.RFC3339Nano),
		cycle.ResetAt.UTC().Format(time.RFC3339Nano),
		cycle.LastVerifiedAt.UTC().Format(time.RFC3339Nano),
		cycle.AuthSubjectID, cycle.QuotaKey); err != nil {
		return fmt.Errorf("usage: roll stored shared quota cycle: %w", err)
	}
	return nil
}

// repairAIAccountSubjectCycleBucketsTx moves the new period's usage out of the
// bucket the frozen anchor kept it in.
//
// The request log is the only record with the per-request timestamps needed to
// split them, so the new period is recomputed from it and the same totals are
// taken back off the previous bucket. Whatever the log no longer covers stays
// where it is: totals move between buckets but are never invented or lost.
func repairAIAccountSubjectCycleBucketsTx(
	tx *sql.Tx,
	subjectID string,
	previousStart, newStart, now time.Time,
) (int64, error) {
	totals, err := sumAIAccountSubjectRequestLogsTx(tx, subjectID, newStart, now)
	if err != nil {
		return 0, err
	}
	if totals.requests == 0 {
		return 0, nil
	}
	if err := writeAIAccountSubjectCycleBucketTx(tx, subjectID, newStart, totals); err != nil {
		return 0, err
	}
	if err := subtractAIAccountSubjectCycleBucketTx(tx, subjectID, previousStart, totals); err != nil {
		return 0, err
	}
	return totals.requests, nil
}

func sumAIAccountSubjectRequestLogsTx(
	tx *sql.Tx,
	subjectID string,
	from, to time.Time,
) (aiAccountSubjectCycleTotals, error) {
	var totals aiAccountSubjectCycleTotals
	var cost sql.NullFloat64
	var tokens sql.NullInt64
	var successes, failures sql.NullInt64
	var first, last storedTime
	err := tx.QueryRow(`
		SELECT COUNT(*),
			SUM(CASE WHEN failed = 0 THEN 1 ELSE 0 END),
			SUM(CASE WHEN failed = 0 THEN 0 ELSE 1 END),
			SUM(cost), SUM(total_tokens), MIN(timestamp), MAX(timestamp)
		FROM request_logs
		WHERE auth_subject_id = ? AND timestamp >= ? AND timestamp <= ?
	`, subjectID, from.UTC().Format(time.RFC3339Nano), to.UTC().Format(time.RFC3339Nano)).
		Scan(&totals.requests, &successes, &failures, &cost, &tokens, &first, &last)
	if err != nil {
		return totals, fmt.Errorf("usage: sum shared subject cycle request logs: %w", err)
	}
	totals.successes = successes.Int64
	totals.failures = failures.Int64
	totals.cost = cost.Float64
	totals.tokens = tokens.Int64
	if first.Valid {
		totals.firstEvent = first.Time.UTC()
	}
	if last.Valid {
		totals.lastEvent = last.Time.UTC()
	}
	return totals, nil
}

func writeAIAccountSubjectCycleBucketTx(
	tx *sql.Tx,
	subjectID string,
	start time.Time,
	totals aiAccountSubjectCycleTotals,
) error {
	firstEvent := totals.firstEvent
	if firstEvent.IsZero() {
		firstEvent = start
	}
	updatedAt := totals.lastEvent
	if updatedAt.IsZero() {
		updatedAt = start
	}
	// Overwrite rather than accumulate: the request log is authoritative for this
	// period, so a rerun that lands on an existing bucket must not double it.
	if _, err := tx.Exec(`
		INSERT INTO ai_account_subject_usage_buckets (
			auth_subject_id, bucket_kind, bucket_start, request_count,
			success_count, failure_count, cost_total, total_tokens, first_event_at, updated_at
		) VALUES (?, 'cycle', ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(auth_subject_id, bucket_kind, bucket_start) DO UPDATE SET
			request_count = excluded.request_count,
			success_count = excluded.success_count,
			failure_count = excluded.failure_count,
			cost_total = excluded.cost_total,
			total_tokens = excluded.total_tokens,
			first_event_at = excluded.first_event_at,
			updated_at = excluded.updated_at
	`, subjectID, formatAIAccountSubjectCycleBucketStart(start),
		totals.requests, totals.successes, totals.failures, totals.cost, totals.tokens,
		firstEvent.Format(time.RFC3339Nano), updatedAt.Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("usage: write repaired cycle bucket: %w", err)
	}
	return nil
}

// subtractAIAccountSubjectCycleBucketTx clamps in Go rather than in SQL: the
// greatest-of-two spelling differs between the SQLite and Postgres backends this
// store runs on.
func subtractAIAccountSubjectCycleBucketTx(
	tx *sql.Tx,
	subjectID string,
	start time.Time,
	totals aiAccountSubjectCycleTotals,
) error {
	key := formatAIAccountSubjectCycleBucketStart(start)
	var requests, successes, failures, tokens int64
	var cost float64
	err := tx.QueryRow(`
		SELECT request_count, success_count, failure_count, cost_total, total_tokens
		FROM ai_account_subject_usage_buckets
		WHERE auth_subject_id = ? AND bucket_kind = 'cycle' AND bucket_start = ?
	`, subjectID, key).Scan(&requests, &successes, &failures, &cost, &tokens)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("usage: load previous cycle bucket for repair: %w", err)
	}
	if _, err := tx.Exec(`
		UPDATE ai_account_subject_usage_buckets
		SET request_count = ?, success_count = ?, failure_count = ?, cost_total = ?, total_tokens = ?
		WHERE auth_subject_id = ? AND bucket_kind = 'cycle' AND bucket_start = ?
	`, clampNonNegativeInt64(requests-totals.requests),
		clampNonNegativeInt64(successes-totals.successes),
		clampNonNegativeInt64(failures-totals.failures),
		clampNonNegativeFloat64(cost-totals.cost),
		clampNonNegativeInt64(tokens-totals.tokens),
		subjectID, key); err != nil {
		return fmt.Errorf("usage: subtract repaired usage from previous cycle bucket: %w", err)
	}
	return nil
}

func clampNonNegativeInt64(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func clampNonNegativeFloat64(value float64) float64 {
	if value < 0 {
		return 0
	}
	return value
}
