package catalog

import "time"

// Kind is what a card is. Seasons and episodes are not kinds: an episode is addressed as its
// series' TMDB id plus season and episode numbers (docs/06-data-model.md §5).
type Kind uint8

const (
	KindUnknown Kind = iota
	KindMovie
	KindSeries
)

// String renders a kind for logs and for the database's enum.
func (k Kind) String() string {
	switch k {
	case KindMovie:
		return "movie"
	case KindSeries:
		return "series"
	case KindUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

// Row is a card: everything needed to render it and nothing more.
//
// Year, Rating and Poster are all allowed to be zero. About a quarter of the catalog is missing at
// least one of them, filling those gaps is the prototype's job (docs/12 §7.1), and a card without a
// poster is still a card the user can play (R-META-2).
type Row struct {
	TMDBID uint64
	Kind   Kind
	Title  string
	// OrigTitle is TMDB original_title / original_name when it differs. Classify scores a
	// native-script search query against it. Empty on catalog cards the client renders.
	OrigTitle string
	Year      uint32
	Rating    uint32
	Poster    string
}

// RowKey is the wire address of a title: enough to know whether catalog already carries it.
type RowKey struct {
	TMDBID uint64
	Kind   Kind
}

// RailKind identifies a row of cards on the home screen.
//
// Continue-watching is deliberately not here: it is the client's own data, computed from its local
// database, and never crosses the wire (docs/12 §2).
type RailKind uint8

const (
	RailUnknown RailKind = iota
	// RailSlider is the hero: the newest titles we actually carry.
	RailSlider
	// RailMostWatched is what people here watch, from play counts.
	RailMostWatched
	// RailTrending is what the world watches that we happen to have, from TMDB weekly trending.
	RailTrending
)

// Rail is one row of cards.
type Rail struct {
	Kind RailKind
	Rows []Row
}

// Episode is one entry of a title's season/episode tree (docs/12 §4, §5). A movie has exactly one
// -- season 0, episode 0, the convention fetcherd already writes (docs/06 §2) -- not a special
// case: it is what gives the client an episode id to ask for files with, movie or series alike.
type Episode struct {
	ID                  int64
	SeasonNo, EpisodeNo int32
	Title               string
}

// FileState mirrors files.state exactly (docs/06 §2) -- no translation table, just a name for what
// the client must not offer to play as though it were always ready.
type FileState uint8

const (
	FileUnknown FileState = iota
	FileReady
	FilePending
	FileUnavailable
)

// File is one encoding of one episode: a row of the pick-list (docs/12 §5).
type File struct {
	ID                   int64
	QualityLabel         string
	DisplayName          string
	Height               int32
	AudioLangs, SubLangs string
	SizeBytes            int64
	DurationS            int32
	State                FileState
}

// EpisodeProgress is one viewer's resume for one episode. LastFileID is a preference, not a key.
type EpisodeProgress struct {
	PositionS  int32
	Done       bool
	LastFileID int64
}

// WatchedState is one row of the cross-device sync delta (docs/11 §6.1): what changed on
// user_episode_state since the caller's watermark. Seq is the new watermark once applied.
type WatchedState struct {
	// Kind says whether EpisodeID is a catalog episode or a personal video (ADR 0037). Zero reads
	// as a catalog episode.
	Kind      MediaKind
	EpisodeID int64
	Done      bool
	Seq       int64
}

// WatchedPercent is the mark-as-watched threshold. Matches the client continue store (≥90%).
const WatchedPercent int32 = 90

// EpisodeWatched is true once position is at least WatchedPercent of duration.
func EpisodeWatched(positionS, durationS int32) bool {
	if durationS <= 0 || positionS <= 0 {
		return false
	}
	return int64(positionS)*100 >= int64(durationS)*int64(WatchedPercent)
}

// EpisodeStarted is true once the viewer is past the first 5%, or 30s when duration is unknown.
func EpisodeStarted(positionS, durationS int32) bool {
	if positionS <= 0 {
		return false
	}
	if durationS <= 0 {
		return positionS >= 30
	}
	return int64(positionS)*100 >= int64(durationS)*5
}

// Item is a title page: everything fetched once, on first open, and cached until a catalog delta
// touches it (docs/12 §4). Missing extras degrade to empty (R-META-2).
type Item struct {
	Row
	Overview string
	// OverviewFa is the stored Persian synopsis (items.overview_fa). Catalog-bot captions
	// prefer it over Overview. Empty until a translation exists.
	OverviewFa string
	ReleasedOn time.Time // zero means the catalog does not know it
	Episodes   []Episode
	Detail     Detail
	Related    []Row
	Collection []Row
	// IMDBID is the "tt…" id when TMDB knows it. Transient — set by the TMDB hydrator, never
	// stored in items.detail, never on the wire. Used only for the content-request admin alert
	// (ADR 0014 §D4).
	IMDBID string
}
