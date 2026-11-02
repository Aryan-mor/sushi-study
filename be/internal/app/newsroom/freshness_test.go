package newsroom

import (
	"testing"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

func TestFreshPremiereIsNinetyDaysOnly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	recent := now.AddDate(0, 0, -30)
	old := now.AddDate(0, 0, -120)
	future := now.AddDate(0, 0, 2)

	if !freshPremiere(recent, now) {
		t.Error("premiere inside 90 days is news")
	}
	if freshPremiere(old, now) {
		t.Error("premiere older than 90 days is not news")
	}
	if freshPremiere(future, now) {
		t.Error("a future premiere is not news")
	}
	if freshPremiere(time.Time{}, now) {
		t.Error("no premiere is not news — year must not rescue a catalog resync")
	}
}

func TestFreshDebutUsesSeriesFirstAir(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	// Year 2026 is not enough: a January first-air is old news by September, and after a resync
	// every episode of that show would otherwise page staff.
	startedJan := Debut{
		Kind:       catalog.KindSeries,
		Year:       2026,
		PremiereOn: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if freshDebut(startedJan, now) {
		t.Error("episode of a show that started >90 days ago is not news")
	}

	newShow := Debut{
		Kind:       catalog.KindSeries,
		Year:       2026,
		PremiereOn: now.AddDate(0, 0, -14),
	}
	if !freshDebut(newShow, now) {
		t.Error("episode of a show that premiered this month is news")
	}
}

func TestFreshAnnouncementParksOldMovies(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	old := Announcement{
		Kind:       catalog.KindMovie,
		Year:       1999,
		PremiereOn: time.Date(1999, 3, 31, 0, 0, 0, 0, time.UTC),
	}
	if FreshAnnouncement(old, now) {
		t.Error("The Matrix must not reach staff after a catalog resync")
	}

	yearOnly := Announcement{Kind: catalog.KindMovie, Year: 2026}
	if FreshAnnouncement(yearOnly, now) {
		t.Error("a 2026 movie with no premiere date is not news")
	}
}

func TestSkipsAnnounceDelayThisISOWeek(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC) // Monday
	thisWeek := time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)
	lastWeek := time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC)

	if !skipsAnnounceDelay(now, now) {
		t.Error("premiere today should skip the settling delay")
	}
	if !skipsAnnounceDelay(thisWeek, now) {
		t.Error("premiere later this week should skip the settling delay")
	}
	if skipsAnnounceDelay(lastWeek, now) {
		t.Error("premiere last week should keep the delay")
	}
	if skipsAnnounceDelay(time.Time{}, now) {
		t.Error("missing premiere should keep the delay")
	}
}
