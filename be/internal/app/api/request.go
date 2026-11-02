package api

import (
	"context"
	"fmt"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
)

// requestQuotaWindow is the rolling span the per-day cap counts over — the same shape as Play's
// and Search's hourly caps, so "10 a day" never resets on a wall-clock boundary a user would have
// to learn.
const requestQuotaWindow = 24 * time.Hour

// NewRequest is one content request about to be written.
type NewRequest struct {
	UserID int64
	TMDBID uint64
	Kind   catalog.Kind
	Title  string
	Poster string
	IMDBID string
}

// RequestStore is what Request needs from storage. Declared here, by the consumer, and small
// (R-GO-5).
type RequestStore interface {
	// CatalogPlayable reports whether this user can open a ready file. Persian (or unset)
	// subtitle language: any ready file. Any other language: hardsub and dubbed files do not count.
	CatalogPlayable(ctx context.Context, userID int64, tmdbID uint64, kind catalog.Kind) (bool, error)
	// RequestCountSince counts the user's requests created at or after since, any status.
	RequestCountSince(ctx context.Context, userID int64, since time.Time) (int, error)
	// TitleSnapshot returns a display title + poster path for the request row; empty when unknown.
	TitleSnapshot(ctx context.Context, tmdbID uint64, kind catalog.Kind) (title, poster string, err error)
	// InsertRequest writes a new pending request. created is false when the user already has an
	// open request for this exact target (the partial unique index).
	InsertRequest(ctx context.Context, r NewRequest) (created bool, err error)
}

// Request records a wish for a movie or whole series we do not carry yet (ADR 0014 §D2). It is a
// read in transport terms — one message in, one typed answer out — because the user needs to know
// straight away whether it landed. The daily cap is a product limit with its own copy, not
// transport backpressure, so QUOTA_EXCEEDED is a normal answer and never ERR_CODE_RATE.
func (a *API) Request(ctx context.Context, p Principal, req *sushiv1.RequestReq) (*sushiv1.RequestRes, error) {
	if a.requests == nil {
		return nil, fmt.Errorf("request: not configured")
	}
	if req.GetTmdbId() == 0 {
		// Defaulting would hide a client bug behind an answer that looks right — the same call
		// Home, Item and Search make about their own required fields.
		return nil, fmt.Errorf("request: tmdb_id not specified")
	}
	kind, err := itemKind(req.GetKind())
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}

	playable, err := a.requests.CatalogPlayable(ctx, p.UserID, req.GetTmdbId(), kind)
	if err != nil {
		return nil, fmt.Errorf("request: serveable check: %w", err)
	}
	if playable {
		return outcome(sushiv1.RequestOutcome_REQUEST_OUTCOME_ALREADY_AVAILABLE, 0), nil
	}

	staff := isStaff(p.Role)
	remaining, err := a.remainingRequestQuota(ctx, p.UserID, staff)
	if err != nil {
		return nil, err
	}
	if !staff && remaining == 0 {
		return outcome(sushiv1.RequestOutcome_REQUEST_OUTCOME_QUOTA_EXCEEDED, 0), nil
	}

	title, poster, imdb, err := a.requestSnapshot(ctx, req.GetTmdbId(), kind)
	if err != nil {
		return nil, fmt.Errorf("request: title snapshot: %w", err)
	}

	created, err := a.requests.InsertRequest(ctx, NewRequest{
		UserID: p.UserID,
		TMDBID: req.GetTmdbId(),
		Kind:   kind,
		Title:  title,
		Poster: poster,
		IMDBID: imdb,
	})
	if err != nil {
		return nil, fmt.Errorf("request: insert: %w", err)
	}
	if !created {
		return outcome(sushiv1.RequestOutcome_REQUEST_OUTCOME_DUPLICATE, 0), nil
	}
	if err := a.autoFollowSeries(ctx, p.UserID, req.GetTmdbId(), kind); err != nil {
		return nil, err
	}
	if !staff && remaining > 0 {
		remaining-- // the row just written counts against the window
	}
	return outcome(sushiv1.RequestOutcome_REQUEST_OUTCOME_ACCEPTED, remaining), nil
}

// autoFollowSeries follows the series behind an accepted series request, so the requester keeps
// getting new-episode pings after the one-off "it's ready" DM (ADR 0014 §D2). The follow
// reconciler is deduped against that first DM. A no-op for movies or with no browse store.
func (a *API) autoFollowSeries(ctx context.Context, userID int64, tmdbID uint64, kind catalog.Kind) error {
	if kind != catalog.KindSeries || a.browse == nil {
		return nil
	}
	if err := a.browse.SetFollow(ctx, userID, tmdbID, true); err != nil {
		return fmt.Errorf("request: auto-follow: %w", err)
	}
	return nil
}

// remainingRequestQuota is how many requests the user may still make in the rolling window. Zero
// for a non-staff caller already at the cap; always zero for staff, who skip the check entirely
// (the caller reads staff status separately, so zero there means "no counter", not "blocked").
func (a *API) remainingRequestQuota(ctx context.Context, userID int64, staff bool) (uint32, error) {
	if staff {
		return 0, nil
	}
	used, err := a.requests.RequestCountSince(ctx, userID, time.Now().Add(-requestQuotaWindow))
	if err != nil {
		return 0, fmt.Errorf("request: quota check: %w", err)
	}
	if used >= a.limits.RequestsPerDay {
		return 0, nil
	}
	return uint32(a.limits.RequestsPerDay - used), nil
}

func outcome(o sushiv1.RequestOutcome, remaining uint32) *sushiv1.RequestRes {
	return &sushiv1.RequestRes{Outcome: o, RemainingToday: remaining}
}

// requestSnapshot gathers the display fields stored on the request row so later DMs need no
// further lookups (ADR 0014 §D4). TMDB (a.hydrate) is preferred — it also carries the IMDb id —
// and is fail-open; the local title_cards read is the fallback.
func (a *API) requestSnapshot(ctx context.Context, tmdbID uint64, kind catalog.Kind) (title, poster, imdb string, err error) {
	if a.hydrate != nil {
		if item, herr := a.hydrate.TitlePage(ctx, tmdbID, kind); herr == nil && item.Title != "" {
			return item.Title, item.Poster, item.IMDBID, nil
		}
	}
	t, p, serr := a.requests.TitleSnapshot(ctx, tmdbID, kind)
	return t, p, "", serr
}

// isStaff — admin and superadmin skip the request quota (ADR 0014 §D2). The strings match
// identity.Role*, but api does not import identity: the transport already resolved the role onto
// the Principal (R-SEC-9).
func isStaff(role string) bool {
	return role == "admin" || role == "superadmin"
}
