package catalog

// LogoNone is persisted when TMDB (or the prototype) was checked and has no title logo. Empty string
// still means "unknown / not hydrated yet" so DetailNeedsHydrate re-fetches. Never put LogoNone on
// the wire — WireLogo maps it to "".
const LogoNone = "-"

// CountryNone is persisted when TMDB was checked and had no origin country. Empty still means
// unknown so DetailNeedsHydrate re-fetches.
const CountryNone = "-"

// TrailerNone is persisted when TMDB was checked and had no trailer/teaser. Empty still means
// unknown/not hydrated. Not part of DetailNeedsHydrate -- most of the catalog already has logo,
// country, people and related by the time this field was added, so it would never trigger a
// re-hydrate on its own; backfilling existing items is a one-off job instead.
const TrailerNone = "-"

// DetailNeedsHydrate is true when logo, cast, origin country, or similar cards are incomplete, or
// cast lacks TMDB person ids (the prototype people often have names only — /person needs the id).
func DetailNeedsHydrate(d Detail) bool {
	return d.Logo == "" || d.Country == "" || peopleMissingIDs(d.People) || len(d.RelatedPreview) == 0
}

// WireLogo is the logo stem for ItemRes: blank when missing or LogoNone.
func WireLogo(logo string) string {
	if logo == "" || logo == LogoNone {
		return ""
	}
	return logo
}

// WireTrailer is the YouTube key for ItemRes: blank when missing or TrailerNone.
func WireTrailer(key string) string {
	if key == "" || key == TrailerNone {
		return ""
	}
	return key
}

// WireCountry is the origin ISO code for ItemRes: blank when missing or CountryNone.
func WireCountry(code string) string {
	if code == "" || code == CountryNone {
		return ""
	}
	if iso := CountryISO(code); iso != "" {
		return iso
	}
	return ""
}

// MarkLogoAbsent records that hydration ran and TMDB had no logo (stops repeat fetches).
func MarkLogoAbsent(d Detail) Detail {
	if d.Logo == "" {
		d.Logo = LogoNone
	}
	return d
}

// MarkCountryAbsent records that hydration ran and TMDB had no origin country.
func MarkCountryAbsent(d Detail) Detail {
	if d.Country == "" {
		d.Country = CountryNone
	}
	return d
}

// MarkTrailerAbsent records that TMDB was checked and had no trailer (stops repeat fetches).
func MarkTrailerAbsent(d Detail) Detail {
	if d.TrailerKey == "" {
		d.TrailerKey = TrailerNone
	}
	return d
}

// MarkNetworkAbsent records that TMDB was checked and had no network (stops a news refetch).
func MarkNetworkAbsent(d Detail) Detail {
	if d.Network == "" {
		d.Network = NetworkNone
	}
	return d
}

// peopleMissingIDs is true when there is no cast, or any person has TMDBID == 0.
func peopleMissingIDs(people []Person) bool {
	if len(people) == 0 {
		return true
	}
	for _, p := range people {
		if p.TMDBID == 0 {
			return true
		}
	}
	return false
}

// MergeDetail copies only empty fields from extra into base.
// People without TMDB ids are replaced wholesale when extra has cast with ids.
func MergeDetail(base, extra Detail) Detail {
	out := base
	out.Logo = preferPresent(out.Logo, extra.Logo)
	if out.Backdrop == "" {
		out.Backdrop = extra.Backdrop
	}
	if out.Genres == "" {
		out.Genres = extra.Genres
	}
	if out.RuntimeS == 0 {
		out.RuntimeS = extra.RuntimeS
	}
	if peopleMissingIDs(out.People) && len(extra.People) > 0 {
		out.People = extra.People
	}
	if len(out.SimilarTMDB) == 0 {
		out.SimilarTMDB = extra.SimilarTMDB
	}
	if out.CollectionID == 0 {
		out.CollectionID = extra.CollectionID
		out.CollectionName = extra.CollectionName
	}
	if len(out.RelatedPreview) == 0 {
		out.RelatedPreview = extra.RelatedPreview
	}
	out.Country = preferPresent(out.Country, extra.Country)
	out.TrailerKey = preferPresent(out.TrailerKey, extra.TrailerKey)
	out.Network = preferPresent(out.Network, extra.Network)
	if len(out.Backdrops) == 0 {
		out.Backdrops = extra.Backdrops
	}
	return out
}

// preferPresent keeps a real value over empty or the absent sentinel. LogoNone, CountryNone,
// TrailerNone, and NetworkNone are the same string, so one comparison covers every caller.
func preferPresent(cur, extra string) string {
	if cur != "" && cur != LogoNone {
		return cur
	}
	if extra != "" && extra != LogoNone {
		return extra
	}
	return cur
}

// ComposeRelated keeps SimilarTMDB order: catalog compact row when present, else RelatedPreview.
// Catalog rows may lack poster (~1/4 of the prototype catalog); keep the TMDB preview poster then.
func ComposeRelated(d Detail, catalogRows []Row) []Row {
	byID := make(map[uint64]Row, len(d.RelatedPreview)+len(catalogRows))
	for _, r := range d.RelatedPreview {
		if r.TMDBID > 0 {
			byID[r.TMDBID] = r
		}
	}
	overlayCatalogRelated(byID, catalogRows)
	out := make([]Row, 0, len(d.SimilarTMDB))
	seen := make(map[uint64]struct{}, len(d.SimilarTMDB))
	for _, id := range d.SimilarTMDB {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if r, ok := byID[id]; ok {
			out = append(out, r)
		}
	}
	if len(out) > 0 {
		return out
	}
	return relatedPreviewFallback(d.RelatedPreview, seen)
}

func overlayCatalogRelated(byID map[uint64]Row, catalogRows []Row) {
	for _, r := range catalogRows {
		if r.TMDBID == 0 {
			continue
		}
		if prev, ok := byID[r.TMDBID]; ok {
			if r.Poster == "" {
				r.Poster = prev.Poster
			}
			if r.Title == "" {
				r.Title = prev.Title
			}
		}
		byID[r.TMDBID] = r
	}
}

func relatedPreviewFallback(preview []Row, seen map[uint64]struct{}) []Row {
	out := make([]Row, 0, len(preview))
	for _, r := range preview {
		if r.TMDBID == 0 {
			continue
		}
		if _, ok := seen[r.TMDBID]; ok {
			continue
		}
		seen[r.TMDBID] = struct{}{}
		out = append(out, r)
	}
	return out
}
