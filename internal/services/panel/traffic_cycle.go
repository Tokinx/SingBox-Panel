package panel

import "time"

func validTrafficResetDay(day int) bool { return day >= 1 && day <= 31 }

// trafficCycleBounds returns the current monthly [start, nextReset) cycle.
// Each boundary is computed independently so a short month never shifts the
// configured day in later months. All calendar arithmetic is in UTC.
func trafficCycleBounds(now time.Time, resetDay int) (start, nextReset time.Time) {
	if !validTrafficResetDay(resetDay) {
		resetDay = 1
	}
	now = now.UTC()
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	boundary := func(month time.Time) time.Time {
		lastDay := month.AddDate(0, 1, -1).Day()
		return time.Date(month.Year(), month.Month(), min(resetDay, lastDay), 0, 0, 0, 0, time.UTC)
	}
	start = boundary(month)
	if now.Before(start) {
		return boundary(month.AddDate(0, -1, 0)), start
	}
	return start, boundary(month.AddDate(0, 1, 0))
}

// serverTrafficWindow keeps relative history ranges available while making
// the current reset cycle the default. Only the cycle uses the reset day;
// every range's daily buckets align to UTC midnight.
func serverTrafficWindow(now time.Time, rangeName string, resetDay int) (trafficRangeConfig, time.Time, time.Time, time.Time, time.Time, bool) {
	cycleStart, cycleEnd := trafficCycleBounds(now, resetDay)
	if rangeName == "cycle" {
		cfg := trafficRangeConfig{step: 24 * time.Hour, hourly: true}
		// Include today's bucket, but do not add future days to the chart.
		end := now.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
		return cfg, cycleStart, end, cycleStart, cycleEnd, true
	}
	cfg, ok := trafficRanges[rangeName]
	if !ok {
		return cfg, time.Time{}, time.Time{}, cycleStart, cycleEnd, false
	}
	start, end := trafficWindow(now, cfg, time.UTC)
	return cfg, start, end, cycleStart, cycleEnd, true
}
