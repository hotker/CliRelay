package usagelogs

import (
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

func (s *Service) AuthExists(authIndex string) bool {
	if s == nil || s.authManager == nil {
		return true
	}
	authIndex = strings.TrimSpace(authIndex)
	if authIndex == "" {
		return false
	}
	for _, auth := range s.authManager.List() {
		if auth == nil {
			continue
		}
		auth.EnsureIndex()
		if strings.TrimSpace(auth.Index) == authIndex {
			return true
		}
	}
	return false
}

func (s *Service) AuthFileGroupTrend(group string, days int) (AuthFileGroupTrendResponse, error) {
	authIndexes := s.authIndexesForProviderGroup(group)
	points, err := usage.QueryDailyCallsByAuthIndexes(authIndexes, days)
	if err != nil {
		return AuthFileGroupTrendResponse{}, err
	}
	if points == nil {
		points = []usage.DailyCountPoint{}
	}
	quotaPoints, err := usage.QueryDailyQuotaByAuthIndexes(authIndexes, "code_week", days)
	if err != nil {
		return AuthFileGroupTrendResponse{}, err
	}
	if quotaPoints == nil {
		quotaPoints = []usage.DailyQuotaPoint{}
	}
	return AuthFileGroupTrendResponse{Days: days, Group: group, Points: points, QuotaPoints: quotaPoints}, nil
}

func (s *Service) AuthFileTrend(authIndex string, days int, hours int) (int, any) {
	if strings.TrimSpace(authIndex) == "" {
		return http.StatusBadRequest, map[string]any{"error": "auth_index is required"}
	}
	if !s.AuthExists(authIndex) {
		return http.StatusNotFound, map[string]any{"error": "auth not found"}
	}

	dailyRaw, err := usage.QueryDailyCallsByAuthIndexes([]string{authIndex}, days)
	if err != nil {
		return http.StatusInternalServerError, map[string]any{"error": err.Error()}
	}
	daily := fillDailyCountPoints(dailyRaw, days)

	hourly, err := usage.QueryHourlyCallsByAuthIndex(authIndex, hours)
	if err != nil {
		return http.StatusInternalServerError, map[string]any{"error": err.Error()}
	}
	if hourly == nil {
		hourly = []usage.HourlyCountPoint{}
	}

	cutoff := usage.CutoffStartUTC(days)
	requestTotal, err := usage.QueryRequestCountByAuthIndexSince(authIndex, cutoff)
	if err != nil {
		return http.StatusInternalServerError, map[string]any{"error": err.Error()}
	}

	trendStart := time.Now().AddDate(0, 0, -7)
	trendEnd := time.Now().Add(time.Minute)
	series, err := usage.QueryQuotaSnapshotSeries(authIndex, trendStart, trendEnd)
	if err != nil {
		return http.StatusInternalServerError, map[string]any{"error": err.Error()}
	}
	if series == nil {
		series = []usage.QuotaSnapshotSeries{}
	}

	cycleStart := cutoff
	if weeklyCycleStart, ok := latestWeeklyQuotaCycleStart(series); ok && weeklyCycleStart.After(cutoff) {
		cycleStart = weeklyCycleStart
	}
	cycleRequestTotal, err := usage.QueryRequestCountByAuthIndexSince(authIndex, cycleStart)
	if err != nil {
		return http.StatusInternalServerError, map[string]any{"error": err.Error()}
	}
	cycleCostTotal, err := usage.QueryCostByAuthIndexSince(authIndex, cycleStart)
	if err != nil {
		return http.StatusInternalServerError, map[string]any{"error": err.Error()}
	}

	return http.StatusOK, AuthFileTrendResponse{
		AuthIndex:         authIndex,
		Days:              days,
		Hours:             hours,
		RequestTotal:      requestTotal,
		CycleRequestTotal: cycleRequestTotal,
		CycleCostTotal:    cycleCostTotal,
		CycleStart:        cycleStart.UTC().Format(time.RFC3339),
		DailyUsage:        daily,
		HourlyUsage:       hourly,
		QuotaSeries:       series,
	}
}

func fillDailyCountPoints(points []usage.DailyCountPoint, days int) []usage.DailyCountPoint {
	if days < 1 {
		days = 7
	}
	byDate := make(map[string]int64, len(points))
	for _, point := range points {
		byDate[point.Date] += point.Requests
	}
	start := usage.CutoffStartUTC(days)
	result := make([]usage.DailyCountPoint, 0, days)
	for i := 0; i < days; i++ {
		date := usage.LocalDayKeyAt(start.AddDate(0, 0, i))
		result = append(result, usage.DailyCountPoint{Date: date, Requests: byDate[date]})
	}
	return result
}

func latestWeeklyQuotaCycleStart(series []usage.QuotaSnapshotSeries) (time.Time, bool) {
	var latestPoint *usage.QuotaSnapshotSeriesPoint
	var latestWindow int64
	for i := range series {
		if series[i].WindowSeconds < 604800 {
			continue
		}
		windowSeconds := series[i].WindowSeconds
		for j := range series[i].Points {
			point := &series[i].Points[j]
			if point.ResetAt == nil || point.ResetAt.IsZero() {
				continue
			}
			if latestPoint == nil || point.Timestamp.After(latestPoint.Timestamp) {
				latestPoint = point
				latestWindow = windowSeconds
			}
		}
	}
	if latestPoint == nil || latestWindow <= 0 {
		return time.Time{}, false
	}
	return latestPoint.ResetAt.Add(-time.Duration(latestWindow) * time.Second).UTC(), true
}

func (s *Service) authIndexesForProviderGroup(group string) []string {
	if s == nil || s.authManager == nil {
		return []string{}
	}
	auths := s.authManager.List()
	indexes := make([]string, 0, len(auths))
	for _, auth := range auths {
		if auth == nil {
			continue
		}
		provider := strings.ToLower(strings.TrimSpace(auth.Provider))
		if group != "all" && provider != group {
			continue
		}
		auth.EnsureIndex()
		if idx := strings.TrimSpace(auth.Index); idx != "" {
			indexes = append(indexes, idx)
		}
	}
	return indexes
}
