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

// serverTrafficWindow preserves the selected chart window and excludes usage
// before the most recent UTC reset. Old buckets remain stored; the leading
// chart points simply have no usage when a reset falls inside the window.
func serverTrafficWindow(now time.Time, rangeName string, resetDay int) (cfg trafficRangeConfig, start, end, usageStart time.Time, ok bool) {
	cfg, ok = trafficRanges[rangeName]
	if !ok {
		return cfg, time.Time{}, time.Time{}, time.Time{}, false
	}
	start, end = trafficWindow(now, cfg, time.UTC)
	usageStart, _ = trafficCycleBounds(now, resetDay)
	if usageStart.Before(start) {
		usageStart = start
	}
	return cfg, start, end, usageStart, true
}
