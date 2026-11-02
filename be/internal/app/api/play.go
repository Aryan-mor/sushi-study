package api

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
)

// locatorPrefix is the literal example from docs/05-media-delivery.md §3 ("plm_4211"), not a
// placeholder — the client hard-codes it too, so changing it is a wire break.
const locatorPrefix = "plm"

// personalLocatorPrefix marks a personal video's copy (ADR 0037). Different from locatorPrefix on
// purpose: personal video 42 and catalog file 42 are different things, and a waiter armed for one
// must never accept the other's copy.
const personalLocatorPrefix = "plp"

// Retry hints for RateLimitError. Not exact -- an honest retry_after_ms (docs/02 §6 forbids
// answering ERR_RATE with zero, which the client reads as "do not retry") without the cost of a
// query that pins down precisely when the next slot opens. The concurrent cap clears as soon as one
// pending job completes, which is normally quick; the hourly cap only clears on the hour boundary,
// so the hint is longer.
const (
	concurrentRetryAfter = 10 * time.Second
	hourlyRetryAfter     = 5 * time.Minute

	// unclaimedStale is just past delivery.maxIdleDelay (10s). A pending row still unclaimed
	// after that means deliveryd is not running — the same threshold deliverywatch uses, kept
	// here so this package does not import the watchdog.
	unclaimedStale = 15 * time.Second
)

// ErrDeliveryStalled means /play enqueued copies that deliveryd has not claimed. Distinct from
// RateLimitError: the concurrent cap is abuse control (R-SEC-11); this is a dead copier. The
// transport maps it onto ERR_CODE_INTERNAL so the client does not retry every 10s as if it were
// ERR_RATE.
var ErrDeliveryStalled = errors.New("api: deliveryd is not claiming pending copies")

// RateLimitError means a Play would exceed one of the R-SEC-11 caps. The transport maps it onto
// ERR_CODE_RATE (with RetryAfter), the same way identity.ErrNoBinding maps onto ERR_CODE_AUTH.
type RateLimitError struct{ RetryAfter time.Duration }

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("api: rate limited, retry after %s", e.RetryAfter)
}

// RateLimits are the R-SEC-11 numbers. Config-driven ("tuning them must never need a deploy") —
// the zero value would silently forbid every delivery, which is exactly the misconfiguration
// R-GO-9 wants loud, so New does not default these; the config loader does.
type RateLimits struct {
	PerHour       int
	MaxConcurrent int
	// SearchPerHour is Search's own R-SEC-11 cap (60/hour), independent of the delivery caps above.
	SearchPerHour int
	// RequestsPerDay is the content-request cap for a non-staff caller (ADR 0014 §D2). A rolling
	// 24h window, config-driven like the rest; admin/superadmin skip it.
	RequestsPerDay int
}

// Delivery is one row of the deliveries table. MessageID (and BotID) are zero for a row still
// pending: deliveryd has not finished the copy yet.
type Delivery struct {
	BotID     int32
	MessageID int64
	Locator   string
}

// Pending reports whether this row is still waiting on deliveryd.
func (d Delivery) Pending() bool { return d.MessageID == 0 }

// DeliveryStore is what Play and Ack need from storage. Declared here, by the consumer, and small
// (R-GO-5).
type DeliveryStore interface {
	// Delivery reports the current row for (readerChatID, kind, fileID), and whether one exists.
	// kind is part of every key: catalog files and personal videos share numbers (ADR 0037).
	Delivery(ctx context.Context, readerChatID int64, kind catalog.MediaKind, fileID int64) (Delivery, bool, error)
	PendingCount(ctx context.Context, readerChatID int64) (int, error)
	DeliveredCountSince(ctx context.Context, readerChatID int64, since time.Time) (int, error)
	Enqueue(ctx context.Context, readerChatID int64, kind catalog.MediaKind, fileID int64, locator string) error
	ForceRedeliver(ctx context.Context, readerChatID int64, kind catalog.MediaKind, fileID int64) error
	Ack(ctx context.Context, readerChatID int64, kind catalog.MediaKind, fileID, messageID int64) error
	// UnclaimedPendingOlderThan is how Play tells a dead copier from the concurrent cap: rows
	// deliveryd has never claimed, older than deliveryd's own idle pause.
	UnclaimedPendingOlderThan(ctx context.Context, readerChatID int64, age time.Duration) (int, error)
}

// Play answers docs/05 §3's state machine: an existing row is handed back as-is (delivered or
// still pending); nothing exists yet enqueues one for deliveryd and answers pending. force skips
// straight past any existing row, per docs/05 §4's "broken row" path.
//
// A personal video (ADR 0037) takes the same path, under its own kind and locator, and only for a
// caller holding a grant. Anyone else gets ErrNotFound -- the same answer as an id that does not
// exist, so a guessed id learns nothing. The grant is checked even when a delivery row already
// exists: access can end after a copy was made.
func (a *API) Play(ctx context.Context, p Principal, req *sushiv1.PlayReq) (*sushiv1.PlayRes, error) {
	kind, id, prefix, err := a.playTarget(ctx, p, req.GetFileId(), req.GetPersonalId())
	if err != nil {
		return nil, err
	}

	if req.GetForce() {
		if err := a.deliveries.ForceRedeliver(ctx, p.ReaderChatID, kind, id); err != nil {
			return nil, fmt.Errorf("play: force redeliver: %w", err)
		}
	} else if res, done, err := a.existingDelivery(ctx, p, kind, id); done || err != nil {
		return res, err
	}

	if err := a.checkPlayLimits(ctx, p.ReaderChatID); err != nil {
		return nil, err
	}

	locator := fmt.Sprintf("%s_%d", prefix, id)
	if err := a.deliveries.Enqueue(ctx, p.ReaderChatID, kind, id, locator); err != nil {
		return nil, fmt.Errorf("play: enqueue: %w", err)
	}
	return pendingRes(locator), nil
}

// playTarget resolves which id space a Play or Ack addresses. personalID wins when set; it is
// granted or it is not found.
func (a *API) playTarget(
	ctx context.Context, p Principal, fileID, personalID int64,
) (kind catalog.MediaKind, id int64, prefix string, err error) {
	if personalID == 0 {
		return catalog.MediaCatalog, fileID, locatorPrefix, nil
	}
	if a.personal == nil {
		return 0, 0, "", ErrNotFound
	}
	ok, err := a.personal.CanViewPersonal(ctx, p.UserID, personalID)
	if err != nil {
		return 0, 0, "", fmt.Errorf("play: personal grant: %w", err)
	}
	if !ok {
		return 0, 0, "", ErrNotFound
	}
	return catalog.MediaPersonal, personalID, personalLocatorPrefix, nil
}

// existingDelivery is Play's non-force path: found tells the caller whether to return res as-is
// (a row already answers the request) or fall through to the rate-limited enqueue.
func (a *API) existingDelivery(
	ctx context.Context, p Principal, kind catalog.MediaKind, fileID int64,
) (res *sushiv1.PlayRes, found bool, err error) {
	d, found, err := a.deliveries.Delivery(ctx, p.ReaderChatID, kind, fileID)
	if err != nil {
		return nil, false, fmt.Errorf("play: %w", err)
	}
	if !found {
		return nil, false, nil
	}
	if d.Pending() {
		return pendingRes(d.Locator), true, nil
	}
	return &sushiv1.PlayRes{Outcome: &sushiv1.PlayRes_Delivered{Delivered: &sushiv1.Delivered{
		BotId:     d.BotID,
		MessageId: d.MessageID,
		Locator:   d.Locator,
	}}}, true, nil
}

// checkPlayLimits enforces R-SEC-11 before a new row is created, and refuses to enqueue more
// when deliveryd has stopped claiming. Per identity, not per request.
func (a *API) checkPlayLimits(ctx context.Context, readerChatID int64) error {
	stale, err := a.deliveries.UnclaimedPendingOlderThan(ctx, readerChatID, unclaimedStale)
	if err != nil {
		return fmt.Errorf("play: unclaimed pending: %w", err)
	}
	if stale > 0 {
		return ErrDeliveryStalled
	}

	pending, err := a.deliveries.PendingCount(ctx, readerChatID)
	if err != nil {
		return fmt.Errorf("play: pending count: %w", err)
	}
	if pending >= a.limits.MaxConcurrent {
		return &RateLimitError{RetryAfter: concurrentRetryAfter}
	}

	delivered, err := a.deliveries.DeliveredCountSince(ctx, readerChatID, time.Now().Add(-time.Hour))
	if err != nil {
		return fmt.Errorf("play: delivered count: %w", err)
	}
	if delivered >= a.limits.PerHour {
		return &RateLimitError{RetryAfter: hourlyRetryAfter}
	}
	return nil
}

func pendingRes(locator string) *sushiv1.PlayRes {
	return &sushiv1.PlayRes{Outcome: &sushiv1.PlayRes_Pending{Pending: &sushiv1.Pending{Locator: locator}}}
}

// Ack records the message id a client actually saw (receiver-side TDLib numbering). Not proof
// of delivery (docs/05 §6) — only a hint; a play that later fails to fetch this id still falls
// back to force re-delivery.
func (a *API) Ack(ctx context.Context, p Principal, req *sushiv1.AckReq) (*sushiv1.AckRes, error) {
	kind, id := catalog.MediaCatalog, req.GetFileId()
	if req.GetPersonalId() != 0 {
		// No grant check: an ack only names a message id in the caller's own chat, and the row it
		// updates exists only if Play already passed the check.
		kind, id = catalog.MediaPersonal, req.GetPersonalId()
	}
	if err := a.deliveries.Ack(ctx, p.ReaderChatID, kind, id, req.GetMessageId()); err != nil {
		return nil, fmt.Errorf("ack: %w", err)
	}
	return &sushiv1.AckRes{}, nil
}
