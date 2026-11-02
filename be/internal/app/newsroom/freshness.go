package newsroom

import "time"

// Freshness after a catalog resync: files.created_at is "now" for the whole library, so a year
// fallback (the prototype's productionYear >= Year()-1) treats every 2025/2026 title as news. The only
// date that still means "this just came out" is the premiere / first-air date, and it must be
// inside 90 days. No premiere → not news. Episodes use the series first-air we store; we do not
// have per-episode air dates.

func freshDebut(d Debut, now time.Time) bool {
	return freshPremiere(d.PremiereOn, now)
}

// FreshAnnouncement is the same rule for a queued row.
func FreshAnnouncement(a Announcement, now time.Time) bool {
	return freshPremiere(a.PremiereOn, now)
}

func freshPremiere(premiere time.Time, now time.Time) bool {
	if premiere.IsZero() {
		return false
	}
	age := now.Sub(premiere.UTC().Truncate(24 * time.Hour))
	if age < 0 {
		return false
	}
	return age <= defaultPremiereWindow
}

// skipsAnnounceDelay is the prototype's channelNewsSkipsAnnounceDelay: a premiere in the current ISO
// week (Mon–Sun) is already "this week's news", so the import-settling delay would only hide it.
func skipsAnnounceDelay(premiere time.Time, now time.Time) bool {
	if premiere.IsZero() {
		return false
	}
	p := premiere.UTC().Truncate(24 * time.Hour)
	weekStart := startOfISOWeek(now)
	weekEnd := weekStart.AddDate(0, 0, 7)
	return !p.Before(weekStart) && p.Before(weekEnd)
}

func startOfISOWeek(t time.Time) time.Time {
	t = t.UTC().Truncate(24 * time.Hour)
	daysFromMonday := (int(t.Weekday()) + 6) % 7
	return t.AddDate(0, 0, -daysFromMonday)
}
