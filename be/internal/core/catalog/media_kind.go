package catalog

// MediaKind says which id space a delivery or a progress row's id belongs to (ADR 0037). Catalog
// files/episodes and personal videos are numbered independently, so the id alone never says what
// it points at: every key that holds one carries a MediaKind beside it.
//
// Not Kind (movie/series): that is what a catalog title is; this is which table the id is from.
type MediaKind int16

const (
	// MediaCatalog is a catalog file (deliveries) or episode (user_episode_state).
	MediaCatalog MediaKind = 1
	// MediaPersonal is a personal_videos row.
	MediaPersonal MediaKind = 2
)
