package contentnotify

import (
	"context"

	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

const (
	statusFulfilled   int16 = 2
	statusUnavailable int16 = 3
)

// alertAdmins tells staff about each request they have not seen, once. With a requests channel
// (ADR 0023) that is one post; without it, one DM per admin, both with reject / re-check buttons.
func (r *Reconciler) alertAdmins(ctx context.Context) {
	alerts, err := r.store.PendingRequestAdminAlerts(ctx, r.batch)
	if err != nil {
		r.log.Warn("content-request admin alerts: list", "err", err)
		return
	}
	if len(alerts) == 0 {
		return
	}
	if r.channelID != 0 {
		r.postToChannel(ctx, alerts)
		return
	}
	r.dmAdmins(ctx, alerts)
}

func (r *Reconciler) postToChannel(ctx context.Context, alerts []AdminAlert) {
	for _, a := range alerts {
		if ctx.Err() != nil {
			return
		}
		post := alertPost(r.channelID, a, "fa")
		if r.topics.Enabled() {
			post.ThreadID = r.topics.Open
		}
		msgID, err := r.dm.PostAlert(ctx, post)
		if err != nil {
			r.log.Warn("content-request admin alert: channel", "err", err, "request", a.ID)
			continue
		}
		if err := r.store.MarkRequestChannelPosted(ctx, a.ID, msgID); err != nil {
			r.log.Warn("content-request admin alert: mark channel", "err", err, "request", a.ID)
		}
	}
}

func (r *Reconciler) dmAdmins(ctx context.Context, alerts []AdminAlert) {
	admins, err := r.store.AdminRecipients(ctx)
	if err != nil {
		r.log.Warn("content-request admin alerts: recipients", "err", err)
		return
	}
	for _, a := range alerts {
		if ctx.Err() != nil {
			return
		}
		// With no admins configured there is nobody to tell; mark it anyway so the row does not
		// re-list every tick forever.
		if !r.sendAdminAlerts(ctx, admins, a) && len(admins) != 0 {
			continue
		}
		if err := r.store.MarkRequestAdminsNotified(ctx, a.ID); err != nil {
			r.log.Warn("content-request admin alert: mark", "err", err, "request", a.ID)
		}
	}
}

func (r *Reconciler) sendAdminAlerts(ctx context.Context, admins []Admin, a AdminAlert) (anySent bool) {
	for _, ad := range admins {
		if _, err := r.dm.PostAlert(ctx, alertPost(ad.TelegramID, a, ad.Locale)); err != nil {
			r.log.Warn("content-request admin alert: send", "err", err, "admin", ad.TelegramID, "request", a.ID)
			continue
		}
		anySent = true
	}
	return anySent
}

func alertPost(chatID int64, a AdminAlert, locale string) AlertPost {
	return AlertPost{
		ChatID:    chatID,
		PhotoURL:  catalog.PosterURL(a.Poster, posterSize),
		Caption:   formatAdminAlert(a, locale),
		RequestID: a.ID,
		Locale:    locale,
	}
}

// closeSettledChannelPosts strips the keyboard off channel posts whose request is fulfilled or
// rejected, so a stale button cannot be pressed twice.
func (r *Reconciler) closeSettledChannelPosts(ctx context.Context) {
	if r.channelID == 0 {
		return
	}
	cards, err := r.store.SettledRequestChannelPosts(ctx, r.batch)
	if err != nil {
		r.log.Warn("content-request channel close: list", "err", err)
		return
	}
	for _, c := range cards {
		if ctx.Err() != nil {
			return
		}
		if err := r.dm.CloseAlert(ctx, r.channelID, c.MessageID, formatClosedChannel(c), r.topics.ThreadFor(c.Status)); err != nil {
			r.log.Warn("content-request channel close: edit", "err", err, "request", c.ID)
			continue
		}
		if err := r.store.ClearRequestChannelMessage(ctx, c.ID); err != nil {
			r.log.Warn("content-request channel close: clear", "err", err, "request", c.ID)
		}
	}
}

// ThreadFor is the topic a settled card moves into. Fulfilled goes to done; every other
// terminal status goes to reject. A disabled forum returns 0 so the card is edited in place.
func (t ForumTopics) ThreadFor(status int16) int64 {
	if !t.Enabled() {
		return 0
	}
	if status == statusFulfilled {
		return t.Done
	}
	return t.Reject
}

func formatClosedChannel(c ChannelCard) string {
	mark := closedRejected
	switch c.Status {
	case statusFulfilled:
		mark = closedAdded
	case statusUnavailable:
		mark = closedRejected
	}
	return joinBlocks(kindIcon(c.Kind)+" <b>"+titleOr(c.Title, c.TMDBID)+"</b>", mark)
}
