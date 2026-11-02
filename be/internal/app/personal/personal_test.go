package personal_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/app/personal"
)

const cloneID = -1009900000004

type grant struct {
	state int16
	title string
	seq   int
}

type fakeStore struct {
	videos    map[string]int64 // unique id -> video id
	uploader  map[int64]int64  // video id -> uploaded_by
	locations map[int64][2]int64
	grants    map[[2]int64]*grant // (video, tg user)
	next      int64
	seq       int
	// raceUID makes InsertVideo lose to a concurrent sender of this unique id.
	raceUID string
}

func newStore() *fakeStore {
	return &fakeStore{
		videos: map[string]int64{}, uploader: map[int64]int64{},
		locations: map[int64][2]int64{}, grants: map[[2]int64]*grant{}, next: 40,
	}
}

func (s *fakeStore) VideoByUID(_ context.Context, uid string) (int64, bool, error) {
	id, ok := s.videos[uid]
	return id, ok, nil
}

func (s *fakeStore) InsertVideo(_ context.Context, by int64, v personal.Video) (int64, bool, error) {
	if v.UniqueID == s.raceUID {
		s.next++
		s.videos[v.UniqueID] = s.next
		s.uploader[s.next] = 999
		return 0, false, nil
	}
	if _, ok := s.videos[v.UniqueID]; ok {
		return 0, false, nil
	}
	s.next++
	s.videos[v.UniqueID] = s.next
	s.uploader[s.next] = by
	return s.next, true, nil
}

func (s *fakeStore) SetCloneLocation(_ context.Context, videoID, channelID, messageID int64) error {
	s.locations[videoID] = [2]int64{channelID, messageID}
	return nil
}

func (s *fakeStore) Grant(_ context.Context, videoID, tg int64) (int16, string, bool, error) {
	g, ok := s.grants[[2]int64{videoID, tg}]
	if !ok {
		return 0, "", false, nil
	}
	return g.state, g.title, true, nil
}

func (s *fakeStore) UpsertPendingGrant(_ context.Context, videoID, tg int64, suggestion string) error {
	s.seq++
	k := [2]int64{videoID, tg}
	if g, ok := s.grants[k]; ok {
		if g.state == personal.GrantAwaitingTitle {
			g.title, g.seq = suggestion, s.seq
		}
		return nil
	}
	s.grants[k] = &grant{state: personal.GrantAwaitingTitle, title: suggestion, seq: s.seq}
	return nil
}

func (s *fakeStore) ConfirmTitle(_ context.Context, videoID, tg int64, title string) (bool, error) {
	g, ok := s.grants[[2]int64{videoID, tg}]
	if !ok {
		return false, nil
	}
	g.title, g.state = title, personal.GrantReady
	return true, nil
}

func (s *fakeStore) LatestPending(_ context.Context, tg int64) (int64, string, bool, error) {
	var best int64
	var bestSeq int
	var title string
	for k, g := range s.grants {
		if k[1] == tg && g.state == personal.GrantAwaitingTitle && g.seq > bestSeq {
			best, bestSeq, title = k[0], g.seq, g.title
		}
	}
	return best, title, best != 0, nil
}

func (s *fakeStore) UploadsSince(_ context.Context, tg int64, _ time.Time) (int, error) {
	n := 0
	for _, by := range s.uploader {
		if by == tg {
			n++
		}
	}
	return n, nil
}

type copyCall struct{ dest, source, msg int64 }

type fakeCopier struct {
	copies  []copyCall
	deleted []int64
	next    int64
	err     error
}

func (c *fakeCopier) CopyMessage(_ context.Context, dest, source, msg int64, _ string) (int64, error) {
	if c.err != nil {
		return 0, c.err
	}
	c.copies = append(c.copies, copyCall{dest, source, msg})
	c.next++
	return 7000 + c.next, nil
}

func (c *fakeCopier) DeleteMessage(_ context.Context, _, msg int64) error {
	c.deleted = append(c.deleted, msg)
	return nil
}

func upload(tg int64, uid, caption, fileName string) personal.Upload {
	return personal.Upload{
		TGUserID: tg, ChatID: tg, MessageID: 55, Caption: caption,
		Video: personal.Video{UniqueID: uid, FileName: fileName, DurationS: 90, SizeBytes: 1 << 20},
	}
}

func TestNewFileIsCopiedOnceAndWaitsForATitle(t *testing.T) {
	t.Parallel()

	store, copier := newStore(), &fakeCopier{}
	svc := personal.New(store, copier, cloneID, 20)

	res, err := svc.Receive(context.Background(), upload(100, "uid-a", "Birthday party\nmore text", "x.mp4"))
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if res.Outcome != personal.NeedsTitle || res.Suggestion != "Birthday party" {
		t.Fatalf("result = %+v, want NeedsTitle with the caption's first line", res)
	}
	if len(copier.copies) != 1 || copier.copies[0] != (copyCall{cloneID, 100, 55}) {
		t.Fatalf("copies = %+v, want one copy from the DM into personal_clone", copier.copies)
	}
	if loc := store.locations[res.VideoID]; loc != [2]int64{cloneID, 7001} {
		t.Errorf("clone location = %v", loc)
	}
	if st, _, _, _ := store.Grant(context.Background(), res.VideoID, 100); st != personal.GrantAwaitingTitle {
		t.Errorf("grant state = %d, want awaiting title", st)
	}
}

// Telegram gives the same file the same unique id whoever sends it: a second person gets a grant
// and names it themselves, and nothing is copied again (ADR 0037 §3).
func TestSecondSenderGetsAGrantNotACopy(t *testing.T) {
	t.Parallel()

	store, copier := newStore(), &fakeCopier{}
	svc := personal.New(store, copier, cloneID, 20)
	first, _ := svc.Receive(context.Background(), upload(100, "uid-a", "", "a.mp4"))
	if _, err := svc.Name(context.Background(), 100, first.VideoID, "Mine"); err != nil {
		t.Fatalf("Name: %v", err)
	}

	res, err := svc.Receive(context.Background(), upload(200, "uid-a", "", "a.mp4"))
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if res.Outcome != personal.NeedsTitle || res.VideoID != first.VideoID {
		t.Fatalf("result = %+v, want a title prompt on the same video", res)
	}
	if len(copier.copies) != 1 {
		t.Errorf("copied %d times, want once in total", len(copier.copies))
	}
	if _, err := svc.Name(context.Background(), 200, res.VideoID, "Theirs"); err != nil {
		t.Fatalf("Name: %v", err)
	}
	if store.grants[[2]int64{first.VideoID, 100}].title != "Mine" ||
		store.grants[[2]int64{first.VideoID, 200}].title != "Theirs" {
		t.Error("titles are not per person")
	}
}

func TestSendingYourOwnFileAgainChangesNothing(t *testing.T) {
	t.Parallel()

	store, copier := newStore(), &fakeCopier{}
	svc := personal.New(store, copier, cloneID, 20)
	first, _ := svc.Receive(context.Background(), upload(100, "uid-a", "", "a.mp4"))
	_, _ = svc.Name(context.Background(), 100, first.VideoID, "Mine")

	res, err := svc.Receive(context.Background(), upload(100, "uid-a", "", "a.mp4"))
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if res.Outcome != personal.AlreadyYours {
		t.Fatalf("outcome = %v, want AlreadyYours", res.Outcome)
	}
	if store.grants[[2]int64{first.VideoID, 100}].title != "Mine" {
		t.Error("re-sending reset the title")
	}
}

func TestQuotaStopsNewFilesButNotGrants(t *testing.T) {
	t.Parallel()

	store, copier := newStore(), &fakeCopier{}
	svc := personal.New(store, copier, cloneID, 1)
	if _, err := svc.Receive(context.Background(), upload(100, "uid-a", "", "")); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Receive(context.Background(), upload(100, "uid-b", "", ""))
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != personal.OverQuota {
		t.Fatalf("outcome = %v, want OverQuota", res.Outcome)
	}
	if len(copier.copies) != 1 {
		t.Errorf("copied %d, want the over-quota file left uncopied", len(copier.copies))
	}

	// A file someone else already stored costs no quota.
	_, _ = svc.Receive(context.Background(), upload(300, "uid-c", "", ""))
	res, err = svc.Receive(context.Background(), upload(100, "uid-c", "", ""))
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != personal.NeedsTitle {
		t.Errorf("outcome = %v, want a grant on the stored file despite the quota", res.Outcome)
	}
}

// Two people send the same new file at once: one row wins, the loser drops its own channel copy and
// uses the winner's row.
func TestLosingTheInsertRaceDropsTheExtraCopy(t *testing.T) {
	t.Parallel()

	store, copier := newStore(), &fakeCopier{}
	store.raceUID = "uid-a"
	svc := personal.New(store, copier, cloneID, 20)

	res, err := svc.Receive(context.Background(), upload(100, "uid-a", "", ""))
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if res.VideoID != store.videos["uid-a"] {
		t.Errorf("video = %d, want the winner's row %d", res.VideoID, store.videos["uid-a"])
	}
	if len(copier.deleted) != 1 || copier.deleted[0] != 7001 {
		t.Errorf("deleted = %v, want the losing copy 7001", copier.deleted)
	}
}

func TestCopyFailureStoresNothing(t *testing.T) {
	t.Parallel()

	store := newStore()
	svc := personal.New(store, &fakeCopier{err: errors.New("bot is not admin")}, cloneID, 20)
	if _, err := svc.Receive(context.Background(), upload(100, "uid-a", "", "")); err == nil {
		t.Fatal("Receive succeeded with a failed copy")
	}
	if len(store.videos) != 0 || len(store.grants) != 0 {
		t.Error("a failed copy left rows behind")
	}
}

func TestNameWithoutAVideoIDUsesTheNewestPending(t *testing.T) {
	t.Parallel()

	store := newStore()
	svc := personal.New(store, &fakeCopier{}, cloneID, 20)
	_, _ = svc.Receive(context.Background(), upload(100, "uid-a", "", ""))
	second, _ := svc.Receive(context.Background(), upload(100, "uid-b", "", ""))

	got, err := svc.Name(context.Background(), 100, 0, "  Holiday \n clip ")
	if err != nil {
		t.Fatalf("Name: %v", err)
	}
	if got != second.VideoID {
		t.Errorf("named %d, want the newest pending %d", got, second.VideoID)
	}
	if title := store.grants[[2]int64{second.VideoID, 100}].title; title != "Holiday clip" {
		t.Errorf("title = %q, want it trimmed to one line", title)
	}
}

func TestNameRejectsEmptyAndOverlongTitles(t *testing.T) {
	t.Parallel()

	svc := personal.New(newStore(), &fakeCopier{}, cloneID, 20)
	for _, title := range []string{"", "   ", string(make([]rune, personal.MaxTitleRunes+1))} {
		if _, err := svc.Name(context.Background(), 100, 41, title); !errors.Is(err, personal.ErrBadTitle) {
			t.Errorf("Name(%q) err = %v, want ErrBadTitle", title, err)
		}
	}
	if _, err := svc.Name(context.Background(), 100, 0, "ok"); !errors.Is(err, personal.ErrNoPendingTitle) {
		t.Errorf("err = %v, want ErrNoPendingTitle with nothing waiting", err)
	}
}

func TestUseSuggestionNamesTheVideoWithTheStoredSuggestion(t *testing.T) {
	t.Parallel()

	store := newStore()
	svc := personal.New(store, &fakeCopier{}, cloneID, 20)
	res, _ := svc.Receive(context.Background(), upload(100, "uid-a", "Trip", ""))

	title, err := svc.UseSuggestion(context.Background(), 100, res.VideoID)
	if err != nil || title != "Trip" {
		t.Fatalf("UseSuggestion = %q, %v; want Trip", title, err)
	}
	if g := store.grants[[2]int64{res.VideoID, 100}]; g.state != personal.GrantReady {
		t.Errorf("grant state = %d, want ready", g.state)
	}
	// A second tap on the same button finds nothing waiting.
	if _, err := svc.UseSuggestion(context.Background(), 100, res.VideoID); !errors.Is(err, personal.ErrNoPendingTitle) {
		t.Errorf("second tap err = %v, want ErrNoPendingTitle", err)
	}
}

func TestSuggest(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ caption, file, want string }{
		{"Trip to Shiraz", "VID_001.mp4", "Trip to Shiraz"},
		{"", "my_holiday.clip.mp4", "my holiday clip"},
		{"", "", ""},
		{"   ", ".mp4", ""},
	} {
		if got := personal.Suggest(tc.caption, tc.file); got != tc.want {
			t.Errorf("Suggest(%q, %q) = %q, want %q", tc.caption, tc.file, got, tc.want)
		}
	}
}
