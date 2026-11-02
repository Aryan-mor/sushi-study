package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Aryan-mor/sushi-study/be/internal/app/adminreq"
	"github.com/Aryan-mor/sushi-study/be/internal/app/api"
	"github.com/Aryan-mor/sushi-study/be/internal/app/contentnotify"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

// CatalogPlayable is CatalogServeable narrowed to files this user's subtitle language can open.
func (s *Store) CatalogPlayable(ctx context.Context, userID int64, tmdbID uint64, kind catalog.Kind) (bool, error) {
	lang := ""
	row, err := s.q.GetPrefs(ctx, userID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("catalog playable prefs %d: %w", userID, err)
	}
	if err == nil {
		lang = row.SubtitleLang
	}
	ok, err := s.catalogPlayableForLang(ctx, tmdbID, kind, lang)
	if err != nil {
		return false, err
	}
	return ok, nil
}

func (s *Store) catalogPlayableForLang(ctx context.Context, tmdbID uint64, kind catalog.Kind, subtitleLang string) (bool, error) {
	if catalog.SubtitleLangIsPersian(subtitleLang) {
		return s.CatalogServeable(ctx, tmdbID, kind)
	}
	rows, err := s.q.ReadyFileLabels(ctx, ReadyFileLabelsParams{TmdbID: int64(tmdbID), Kind: int16(kind)})
	if err != nil {
		return false, fmt.Errorf("ready file labels %d/%d: %w", tmdbID, kind, err)
	}
	for _, row := range rows {
		if !catalog.FileHiddenForNonPersianSubtitle(row.QualityLabel, row.SubLangs) {
			return true, nil
		}
	}
	return false, nil
}

// FulfillPlayableRequests marks each pending request the requester can play. A shared title
// stays open for a non-Persian user when the only ready files are hardsub or dubbed.
func (s *Store) FulfillPlayableRequests(ctx context.Context, limit int32) (int, error) {
	rows, err := s.q.PendingRequestsForFulfilment(ctx, limit)
	if err != nil {
		return 0, fmt.Errorf("pending requests for fulfilment: %w", err)
	}
	cache := map[string]bool{}
	var n int
	for _, row := range rows {
		key := fmt.Sprintf("%d:%d:%t", row.TmdbID, row.Kind, catalog.SubtitleLangIsPersian(row.SubtitleLang))
		ok, seen := cache[key]
		if !seen {
			ok, err = s.catalogPlayableForLang(ctx, uint64(row.TmdbID), catalog.Kind(row.Kind), row.SubtitleLang)
			if err != nil {
				return n, err
			}
			cache[key] = ok
		}
		if !ok {
			continue
		}
		marked, err := s.q.MarkRequestFulfilled(ctx, row.ID)
		if err != nil {
			return n, fmt.Errorf("mark request %d fulfilled: %w", row.ID, err)
		}
		n += int(marked)
	}
	return n, nil
}

// CatalogServeable reports whether this (tmdb_id, kind) already has a ready file. Staff recheck
// and the catalog bot still use it. The app request path uses CatalogPlayable.
func (s *Store) CatalogServeable(ctx context.Context, tmdbID uint64, kind catalog.Kind) (bool, error) {
	ok, err := s.q.CatalogServeable(ctx, CatalogServeableParams{TmdbID: int64(tmdbID), Kind: int16(kind)})
	if err != nil {
		return false, fmt.Errorf("catalog serveable %d/%d: %w", tmdbID, kind, err)
	}
	return ok, nil
}

// RequestCountSince counts a user's requests in the rolling quota window.
func (s *Store) RequestCountSince(ctx context.Context, userID int64, since time.Time) (int, error) {
	n, err := s.q.CountRequestsSince(ctx, CountRequestsSinceParams{
		UserID:    userID,
		CreatedAt: pgtype.Timestamptz{Time: since, Valid: true},
	})
	if err != nil {
		return 0, fmt.Errorf("count requests since: %w", err)
	}
	return int(n), nil
}

// TitleSnapshot reads a display title + poster for the request row from title_cards (catalog wins
// over the TMDB cache, env/018). No row — an id we have never hydrated — is not an error.
func (s *Store) TitleSnapshot(ctx context.Context, tmdbID uint64, kind catalog.Kind) (title, poster string, err error) {
	row, err := s.q.RequestTitleSnapshot(ctx, RequestTitleSnapshotParams{TmdbID: int64(tmdbID), Kind: int16(kind)})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("title snapshot %d/%d: %w", tmdbID, kind, err)
	}
	return row.Title, row.Poster, nil
}

// InsertRequest writes a new pending request. ON CONFLICT DO NOTHING against the partial unique
// index means an already-open request returns no row — reported as created == false, which the
// API turns into DUPLICATE without charging the quota.
func (s *Store) InsertRequest(ctx context.Context, r api.NewRequest) (created bool, err error) {
	_, err = s.q.InsertContentRequest(ctx, InsertContentRequestParams{
		UserID: r.UserID,
		TmdbID: int64(r.TMDBID),
		Kind:   int16(r.Kind),
		Title:  r.Title,
		Poster: r.Poster,
		ImdbID: r.IMDBID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("insert content request: %w", err)
	}
	return true, nil
}

// --- contentnotify.Store (ADR 0014 §D4) ---

// PendingRequestAdminAlerts lists requests no admin has been told about yet.
func (s *Store) PendingRequestAdminAlerts(ctx context.Context, limit int32) ([]contentnotify.AdminAlert, error) {
	rows, err := s.q.PendingContentRequestsForAdmins(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("pending request admin alerts: %w", err)
	}
	out := make([]contentnotify.AdminAlert, len(rows))
	for i, r := range rows {
		out[i] = contentnotify.AdminAlert{
			ID:                r.ID,
			TMDBID:            uint64(r.TmdbID),
			Kind:              catalog.Kind(r.Kind),
			Title:             r.Title,
			Poster:            r.Poster,
			Year:              r.Year,
			Rating:            r.Rating,
			IMDBID:            r.ImdbID,
			RequesterTG:       r.RequesterTgID,
			RequesterName:     r.RequesterName,
			RequesterUsername: r.RequesterUsername,
			SubtitleLang:      r.SubtitleLang,
		}
	}
	return out, nil
}

// AdminRecipients is the recipient list for a new-request alert: every user whose role is admin
// or superadmin (ADR 0014 §D4), each with the locale their copy is written in.
func (s *Store) AdminRecipients(ctx context.Context) ([]contentnotify.Admin, error) {
	rows, err := s.q.AdminAlertRecipients(ctx)
	if err != nil {
		return nil, fmt.Errorf("admin alert recipients: %w", err)
	}
	out := make([]contentnotify.Admin, len(rows))
	for i, r := range rows {
		out[i] = contentnotify.Admin{TelegramID: r.TgUserID, Locale: r.Locale}
	}
	return out, nil
}

// MarkRequestAdminsNotified stamps a request so its admin alert does not re-send.
func (s *Store) MarkRequestAdminsNotified(ctx context.Context, id int64) error {
	if err := s.q.MarkContentRequestAdminsNotified(ctx, id); err != nil {
		return fmt.Errorf("mark request %d admins notified: %w", id, err)
	}
	return nil
}

// MarkRequestChannelPosted stamps a request after its staff-inbox post landed (ADR 0023).
func (s *Store) MarkRequestChannelPosted(ctx context.Context, id, messageID int64) error {
	if err := s.q.MarkContentRequestChannelPosted(ctx, MarkContentRequestChannelPostedParams{
		ID: id, ChannelMessageID: &messageID,
	}); err != nil {
		return fmt.Errorf("mark request %d channel posted: %w", id, err)
	}
	return nil
}

// SettledRequestChannelPosts lists channel posts whose request is fulfilled or rejected.
func (s *Store) SettledRequestChannelPosts(ctx context.Context, limit int32) ([]contentnotify.ChannelCard, error) {
	rows, err := s.q.SettledRequestChannelPosts(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("settled request channel posts: %w", err)
	}
	out := make([]contentnotify.ChannelCard, 0, len(rows))
	for _, r := range rows {
		if r.ChannelMessageID == nil {
			continue
		}
		out = append(out, contentnotify.ChannelCard{
			ID:        r.ID,
			MessageID: *r.ChannelMessageID,
			Status:    r.Status,
			Title:     r.Title,
			TMDBID:    uint64(r.TmdbID),
			Kind:      catalog.Kind(r.Kind),
		})
	}
	return out, nil
}

// ClearRequestChannelMessage drops the stored message id after the channel post was closed.
func (s *Store) ClearRequestChannelMessage(ctx context.Context, id int64) error {
	if err := s.q.ClearRequestChannelMessage(ctx, id); err != nil {
		return fmt.Errorf("clear request %d channel message: %w", id, err)
	}
	return nil
}

// PendingRequestTargets lists the distinct (tmdb_id, kind) pairs still waiting on content.
func (s *Store) PendingRequestTargets(ctx context.Context, limit int32) ([]contentnotify.Target, error) {
	rows, err := s.q.PendingContentRequests(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("pending request targets: %w", err)
	}
	out := make([]contentnotify.Target, len(rows))
	for i, r := range rows {
		out[i] = contentnotify.Target{TMDBID: uint64(r.TmdbID), Kind: catalog.Kind(r.Kind)}
	}
	return out, nil
}

// MarkTargetFulfilled flips every pending request for a now-serveable target and returns how many.
func (s *Store) MarkTargetFulfilled(ctx context.Context, tmdbID uint64, kind catalog.Kind) (int64, error) {
	n, err := s.q.MarkTargetFulfilled(ctx, MarkTargetFulfilledParams{TmdbID: int64(tmdbID), Kind: int16(kind)})
	if err != nil {
		return 0, fmt.Errorf("mark target %d/%d fulfilled: %w", tmdbID, kind, err)
	}
	return n, nil
}

// FulfilledRequestsToNotify lists fulfilled requests whose requester has not been DMed yet.
func (s *Store) FulfilledRequestsToNotify(ctx context.Context, limit int32) ([]contentnotify.RequesterNotice, error) {
	rows, err := s.q.FulfilledContentRequestsToNotify(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("fulfilled requests to notify: %w", err)
	}
	out := make([]contentnotify.RequesterNotice, len(rows))
	for i, r := range rows {
		out[i] = contentnotify.RequesterNotice{
			ID:          r.ID,
			UserID:      r.UserID,
			RequesterTG: r.TgUserID,
			TMDBID:      uint64(r.TmdbID),
			Kind:        catalog.Kind(r.Kind),
			Title:       r.Title,
			Poster:      r.Poster,
			Year:        r.Year,
			Rating:      r.Rating,
			Locale:      r.Locale,
		}
	}
	return out, nil
}

// MarkRequesterNotified stamps a fulfilled request so its "it's ready" DM does not re-send.
func (s *Store) MarkRequesterNotified(ctx context.Context, id int64) error {
	if err := s.q.MarkContentRequestRequesterNotified(ctx, id); err != nil {
		return fmt.Errorf("mark request %d requester notified: %w", id, err)
	}
	return nil
}

// --- adminreq.Store (ADR 0014 §D5) ---

// PendingRequestPage returns one page of pending requests, oldest first.
func (s *Store) PendingRequestPage(ctx context.Context, limit, offset int32) ([]adminreq.Request, error) {
	rows, err := s.q.AdminPendingRequestsPage(ctx, AdminPendingRequestsPageParams{Limit: limit, Offset: offset})
	if err != nil {
		return nil, fmt.Errorf("pending request page: %w", err)
	}
	out := make([]adminreq.Request, len(rows))
	for i, r := range rows {
		out[i] = adminreq.Request{
			ID:                r.ID,
			TMDBID:            uint64(r.TmdbID),
			IMDBID:            r.ImdbID,
			Kind:              catalog.Kind(r.Kind),
			Title:             r.Title,
			RequesterTG:       r.RequesterTgID,
			RequesterName:     r.RequesterName,
			RequesterUsername: r.RequesterUsername,
		}
	}
	return out, nil
}

// PendingRequestCount is how many requests are still open — drives the panel's paging.
func (s *Store) PendingRequestCount(ctx context.Context) (int, error) {
	n, err := s.q.AdminPendingRequestsCount(ctx)
	if err != nil {
		return 0, fmt.Errorf("pending request count: %w", err)
	}
	return int(n), nil
}

// RequestTarget returns the (tmdb_id, kind) one request points at.
func (s *Store) RequestTarget(ctx context.Context, id int64) (uint64, catalog.Kind, bool, error) {
	row, err := s.q.RequestTarget(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, catalog.KindUnknown, false, nil
	}
	if err != nil {
		return 0, catalog.KindUnknown, false, fmt.Errorf("request %d target: %w", id, err)
	}
	return uint64(row.TmdbID), catalog.Kind(row.Kind), true, nil
}

// RejectRequest marks a still-pending request unavailable; wasPending is false for a stale id.
func (s *Store) RejectRequest(ctx context.Context, id int64) (bool, error) {
	n, err := s.q.RejectContentRequest(ctx, id)
	if err != nil {
		return false, fmt.Errorf("reject request %d: %w", id, err)
	}
	return n > 0, nil
}
