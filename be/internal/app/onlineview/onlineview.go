// Package onlineview is the admin "who is online" read: people with recent playback or search,
// plus one person's watch log. The panel UI lives in cmd/main-bot.
package onlineview

import (
	"context"
	"time"
)

// Viewer is one person on the online list. Platforms are shelf keys seen inside the window.
type Viewer struct {
	ID        int64
	Name      string
	Username  string
	SeenAt    time.Time
	Platforms []string
}

// Device is one app install that checked in inside the window.
type Device struct {
	Platform string
	SeenAt   time.Time
}

// Person is the header of a watch log. Found is false when the id is gone.
type Person struct {
	ID       int64
	Name     string
	Username string
	Found    bool
}

// Entry is one episode the person has progress for. Kind is catalog.Kind as stored (1 movie, 2 series).
type Entry struct {
	At           time.Time
	PositionS    int32
	Done         bool
	TMDBID       int64
	ItemTitle    string
	Kind         int16
	SeasonNo     int32
	EpisodeNo    int32
	EpisodeTitle string
}

// Store is what the Actions panel needs. Declared here, by the consumer; *pg.Store satisfies it.
type Store interface {
	ListOnline(ctx context.Context, mins, limit, offset int32) ([]Viewer, error)
	CountOnline(ctx context.Context, mins int32) (int64, error)
	WatchLog(ctx context.Context, userID int64, limit, offset int32) (Person, []Entry, error)
	WatchCount(ctx context.Context, userID int64) (int64, error)
	LatestWatch(ctx context.Context, userID int64) (Entry, bool, error)
	Devices(ctx context.Context, userID int64, mins int32) ([]Device, error)
}
