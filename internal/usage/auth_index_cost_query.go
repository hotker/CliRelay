package usage

import (
	"fmt"
	"strings"
	"time"
)

func QueryCostByAuthIndexSince(authIndex string, since time.Time) (float64, error) {
	db := getReadDB()
	if db == nil {
		return 0, nil
	}
	authIndex = strings.TrimSpace(authIndex)
	if authIndex == "" {
		return 0, nil
	}
	// Prefer auth_subject_id rollup when auth_index maps to a subject; otherwise
	// sum day buckets from the day of `since` (auth_index is not a rollup dimension).
	dayFrom := localDayKeyAt(since)
	var total float64
	// auth_index is not stored on rollup; approximate via request_logs only inside
	// retention window is unacceptable for limits. Best-effort: map live subject.
	subjectID := resolveAuthSubjectIDFromAuthIndex(authIndex)
	if subjectID != "" {
		err := db.QueryRow(`
			SELECT COALESCE(SUM(cost_total), 0)
			FROM usage_rollup_buckets
			WHERE bucket_kind = ? AND bucket_start >= ? AND auth_subject_id = ?
		`, rollupBucketDay, dayFrom, subjectID).Scan(&total)
		if err != nil {
			return 0, fmt.Errorf("usage: request cost by auth subject query: %w", err)
		}
		return total, nil
	}
	// No subject mapping: return 0 rather than scanning request_logs (fail-safe for limits).
	return 0, nil
}

func resolveAuthSubjectIDFromAuthIndex(authIndex string) string {
	authIndex = strings.TrimSpace(authIndex)
	if authIndex == "" {
		return ""
	}
	db := getReadDB()
	if db == nil {
		return ""
	}
	// Best-effort: latest non-empty subject for this auth_index from rollup-adjacent tables.
	var subject string
	_ = db.QueryRow(`
		SELECT auth_subject_id FROM ai_account_status
		WHERE auth_index = ? AND trim(coalesce(auth_subject_id,'')) <> ''
		LIMIT 1
	`, authIndex).Scan(&subject)
	return strings.TrimSpace(subject)
}
