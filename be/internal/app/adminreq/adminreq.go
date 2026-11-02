// Package adminreq is the storage contract for main-bot's admin content-request panel
// (ADR 0014 §D5): list the pending requests a page at a time, reject one, or re-check whether the
// catalog can serve it now. The panel UI itself lives in cmd/main-bot.
package adminreq

import (
	"context"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

// Request is one pending content request as the panel shows it.
type Request struct {
	ID                int64
	TMDBID            uint64
	IMDBID            string // "" when TMDB had none at request time
	Kind              catalog.Kind
	Title             string
	RequesterTG       int64
	RequesterName     string
	RequesterUsername string
}

// Store is what the panel needs. Declared here, by the consumer; *pg.Store satisfies it (the last
// two methods are already used by contentnotify).
type Store interface {
	PendingRequestPage(ctx context.Context, limit, offset int32) ([]Request, error)
	PendingRequestCount(ctx context.Context) (int, error)
	// RequestTarget returns the (tmdb_id, kind) a request points at; found is false for an id that
	// is gone or already settled.
	RequestTarget(ctx context.Context, id int64) (tmdbID uint64, kind catalog.Kind, found bool, err error)
	// RejectRequest marks it unavailable. wasPending is false when it was not pending any more
	// (a stale button).
	RejectRequest(ctx context.Context, id int64) (wasPending bool, err error)
	CatalogServeable(ctx context.Context, tmdbID uint64, kind catalog.Kind) (bool, error)
	MarkTargetFulfilled(ctx context.Context, tmdbID uint64, kind catalog.Kind) (int64, error)
	// ClearRequestChannelMessage drops the stored channel post id after the card has moved
	// out of open (ADR 0036), so the reconciler does not move it a second time.
	ClearRequestChannelMessage(ctx context.Context, id int64) error
}
