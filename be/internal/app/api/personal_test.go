package api_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/app/api"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
)

type grantKey struct{ userID, videoID int64 }

// fakePersonal is the grant table plus My videos, keyed the way the env queries answer them.
type fakePersonal struct {
	grants   map[grantKey]bool
	videos   map[int64][]api.PersonalVideo
	progress []grantKey
	done     []grantKey
}

func (f *fakePersonal) CanViewPersonal(_ context.Context, userID, videoID int64) (bool, error) {
	return f.grants[grantKey{userID, videoID}], nil
}

func (f *fakePersonal) ListPersonalVideos(_ context.Context, userID int64, limit, offset int32) ([]api.PersonalVideo, error) {
	all := f.videos[userID]
	if int(offset) >= len(all) {
		return nil, nil
	}
	end := min(int(offset+limit), len(all))
	return all[offset:end], nil
}

func (f *fakePersonal) SetPersonalProgress(_ context.Context, userID, videoID int64, _, _ int32) error {
	f.progress = append(f.progress, grantKey{userID, videoID})
	return nil
}

func (f *fakePersonal) SetPersonalDone(_ context.Context, userID, videoID int64, _ bool) error {
	f.done = append(f.done, grantKey{userID, videoID})
	return nil
}

func personalAPI(d *fakeDeliveries, p *fakePersonal) *api.API {
	return playAPI(d, api.RateLimits{PerHour: 30, MaxConcurrent: 2}).WithPersonal(p)
}

var viewer = api.Principal{UserID: 5, ReaderChatID: 111}

func TestPlayPersonalWithGrantEnqueuesUnderItsOwnKindAndLocator(t *testing.T) {
	t.Parallel()

	d := &fakeDeliveries{}
	p := &fakePersonal{grants: map[grantKey]bool{{5, 42}: true}}
	res, err := personalAPI(d, p).Play(context.Background(), viewer, &sushiv1.PlayReq{PersonalId: 42})
	if err != nil {
		t.Fatalf("Play: %v", err)
	}
	if got := res.GetPending().GetLocator(); got != "plp_42" {
		t.Errorf("locator = %q, want plp_42", got)
	}
	if _, ok := d.rows[deliveryKey{111, catalog.MediaPersonal, 42}]; !ok {
		t.Error("no personal delivery row was enqueued")
	}
	if _, ok := d.rows[deliveryKey{111, catalog.MediaCatalog, 42}]; ok {
		t.Error("a personal play enqueued a catalog row for the same number")
	}
}

// Without a grant the answer is the same as for an id that does not exist: a guessed id learns
// nothing (ADR 0037 §4).
func TestPlayPersonalWithoutGrantIsNotFound(t *testing.T) {
	t.Parallel()

	d := &fakeDeliveries{}
	p := &fakePersonal{grants: map[grantKey]bool{{6, 42}: true}} // somebody else's
	_, err := personalAPI(d, p).Play(context.Background(), viewer, &sushiv1.PlayReq{PersonalId: 42})
	if !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if len(d.rows) != 0 {
		t.Errorf("rows = %v, want nothing enqueued", d.rows)
	}
}

// A delivery row from before the grant ended must not be handed back: the grant is checked first.
func TestPlayPersonalChecksTheGrantEvenWhenACopyExists(t *testing.T) {
	t.Parallel()

	d := &fakeDeliveries{rows: map[deliveryKey]deliveryRow{
		{111, catalog.MediaPersonal, 42}: {botID: 7, messageID: 900, locator: "plp_42"},
	}}
	_, err := personalAPI(d, &fakePersonal{}).Play(context.Background(), viewer, &sushiv1.PlayReq{PersonalId: 42})
	if !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// Catalog file 42 and personal video 42 are different rows: a delivered catalog copy must never
// answer a personal play.
func TestPlayPersonalDoesNotReuseTheCatalogRowWithTheSameNumber(t *testing.T) {
	t.Parallel()

	d := &fakeDeliveries{}
	d.deliver(42, 500, "plm_42")
	p := &fakePersonal{grants: map[grantKey]bool{{5, 42}: true}}
	res, err := personalAPI(d, p).Play(context.Background(), viewer, &sushiv1.PlayReq{PersonalId: 42})
	if err != nil {
		t.Fatalf("Play: %v", err)
	}
	if res.GetDelivered() != nil {
		t.Fatalf("got the catalog copy %+v for a personal play", res.GetDelivered())
	}
}

func TestPlayPersonalWithoutAPersonalStoreIsNotFound(t *testing.T) {
	t.Parallel()

	_, err := playAPI(&fakeDeliveries{}, api.RateLimits{PerHour: 30, MaxConcurrent: 2}).
		Play(context.Background(), viewer, &sushiv1.PlayReq{PersonalId: 42})
	if !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestAckPersonalRecordsAgainstThePersonalKind(t *testing.T) {
	t.Parallel()

	d := &fakeDeliveries{}
	_, err := personalAPI(d, &fakePersonal{}).
		Ack(context.Background(), viewer, &sushiv1.AckReq{PersonalId: 42, MessageId: 77})
	if err != nil {
		t.Fatalf("Ack: %v", err)
	}
	if len(d.acked) != 1 || d.acked[0].kind != catalog.MediaPersonal || d.acked[0].fileID != 42 {
		t.Fatalf("acked = %+v, want one personal ack for 42", d.acked)
	}
}

func TestListMyVideosReturnsTheCallersCards(t *testing.T) {
	t.Parallel()

	p := &fakePersonal{videos: map[int64][]api.PersonalVideo{
		5: {{ID: 42, Title: "Birthday", DurationS: 120, SizeBytes: 9000, PositionS: 30}},
		6: {{ID: 43, Title: "Not yours"}},
	}}
	res, err := personalAPI(&fakeDeliveries{}, p).List(context.Background(), viewer,
		&sushiv1.ListReq{Scope: sushiv1.ListScope_LIST_SCOPE_MY_VIDEOS})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := res.GetPersonalVideos()
	if len(got) != 1 || got[0].GetPersonalId() != 42 || got[0].GetTitle() != "Birthday" || got[0].GetPositionS() != 30 {
		t.Fatalf("personal_videos = %+v, want just the caller's Birthday", got)
	}
	if len(res.GetRows()) != 0 {
		t.Error("MY_VIDEOS carried catalog rows")
	}
}

func TestPersonalProgressNeedsAGrant(t *testing.T) {
	t.Parallel()

	p := &fakePersonal{grants: map[grantKey]bool{{5, 42}: true}}
	a := personalAPI(&fakeDeliveries{}, p)
	err := a.Events(context.Background(), viewer, &sushiv1.Events{Events: []*sushiv1.Event{
		{Body: &sushiv1.Event_Prog{Prog: &sushiv1.ProgEvent{PersonalId: 42, PositionS: 60}}},
		{Body: &sushiv1.Event_Prog{Prog: &sushiv1.ProgEvent{PersonalId: 99, PositionS: 60}}},
		{Body: &sushiv1.Event_Watched{Watched: &sushiv1.WatchedEvent{PersonalId: 42, Done: true}}},
	}})
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(p.progress) != 1 || p.progress[0] != (grantKey{5, 42}) {
		t.Errorf("progress = %v, want only the granted video 42", p.progress)
	}
	if len(p.done) != 1 || p.done[0] != (grantKey{5, 42}) {
		t.Errorf("done = %v, want video 42", p.done)
	}
}

type fakeActions struct{ deleted, shared []grantKey }

func (f *fakeActions) DeletePersonalGrant(_ context.Context, userID, videoID int64) error {
	f.deleted = append(f.deleted, grantKey{userID, videoID})
	return nil
}

func (f *fakeActions) EnqueuePersonalShare(_ context.Context, userID, videoID int64) error {
	f.shared = append(f.shared, grantKey{userID, videoID})
	return nil
}

func TestPersonalActionsNeedAGrant(t *testing.T) {
	t.Parallel()

	acts := &fakeActions{}
	a := personalAPI(&fakeDeliveries{}, &fakePersonal{grants: map[grantKey]bool{{5, 42}: true}}).WithPersonalActions(acts)

	for _, action := range []sushiv1.PersonalAction{
		sushiv1.PersonalAction_PERSONAL_ACTION_DELETE, sushiv1.PersonalAction_PERSONAL_ACTION_SHARE,
	} {
		_, err := a.Personal(context.Background(), viewer, &sushiv1.PersonalReq{PersonalId: 99, Action: action})
		if !errors.Is(err, api.ErrNotFound) {
			t.Errorf("%s on a video without access: err = %v, want ErrNotFound", action, err)
		}
	}
	if len(acts.deleted)+len(acts.shared) != 0 {
		t.Fatalf("acted without a grant: %+v", acts)
	}
}

func TestPersonalDeleteAndShareReachTheirStores(t *testing.T) {
	t.Parallel()

	acts := &fakeActions{}
	a := personalAPI(&fakeDeliveries{}, &fakePersonal{grants: map[grantKey]bool{{5, 42}: true}}).WithPersonalActions(acts)

	if _, err := a.Personal(context.Background(), viewer,
		&sushiv1.PersonalReq{PersonalId: 42, Action: sushiv1.PersonalAction_PERSONAL_ACTION_SHARE}); err != nil {
		t.Fatalf("share: %v", err)
	}
	if _, err := a.Personal(context.Background(), viewer,
		&sushiv1.PersonalReq{PersonalId: 42, Action: sushiv1.PersonalAction_PERSONAL_ACTION_DELETE}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(acts.shared) != 1 || acts.shared[0] != (grantKey{5, 42}) {
		t.Errorf("shared = %v", acts.shared)
	}
	if len(acts.deleted) != 1 || acts.deleted[0] != (grantKey{5, 42}) {
		t.Errorf("deleted = %v", acts.deleted)
	}
}

func TestPersonalActionsOffWithoutTheLibrary(t *testing.T) {
	t.Parallel()

	a := personalAPI(&fakeDeliveries{}, &fakePersonal{grants: map[grantKey]bool{{5, 42}: true}})
	_, err := a.Personal(context.Background(), viewer,
		&sushiv1.PersonalReq{PersonalId: 42, Action: sushiv1.PersonalAction_PERSONAL_ACTION_DELETE})
	if !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound when apid has no library connection", err)
	}
}
