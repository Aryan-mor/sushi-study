package personal

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type shareRow struct {
	video     int64
	title     string
	createdBy int64
}

type fakeShareStore struct {
	grants map[[2]int64]struct {
		state int16
		title string
	}
	byVideo map[int64]string
	byToken map[string]shareRow
}

func newShareStore() *fakeShareStore {
	return &fakeShareStore{
		grants: map[[2]int64]struct {
			state int16
			title string
		}{},
		byVideo: map[int64]string{},
		byToken: map[string]shareRow{},
	}
}

func (s *fakeShareStore) Grant(_ context.Context, v, tg int64) (int16, string, bool, error) {
	g, ok := s.grants[[2]int64{v, tg}]
	return g.state, g.title, ok, nil
}

func (s *fakeShareStore) EnsureShare(_ context.Context, v int64, token, title string, by int64) error {
	if _, ok := s.byVideo[v]; ok {
		return nil
	}
	s.byVideo[v] = token
	s.byToken[token] = shareRow{video: v, title: title, createdBy: by}
	return nil
}

func (s *fakeShareStore) ShareByVideo(_ context.Context, v int64) (string, string, bool, error) {
	t, ok := s.byVideo[v]
	return t, s.byToken[t].title, ok, nil
}

func (s *fakeShareStore) ShareByToken(_ context.Context, t string) (int64, string, int64, bool, error) {
	r, ok := s.byToken[t]
	return r.video, r.title, r.createdBy, ok, nil
}

func (s *fakeShareStore) InsertSharedGrant(_ context.Context, v, tg int64, title string, _ int64) (bool, error) {
	k := [2]int64{v, tg}
	if _, ok := s.grants[k]; ok {
		return false, nil
	}
	s.grants[k] = struct {
		state int16
		title string
	}{GrantReady, title}
	return true, nil
}

func TestTokensAreRandomAndURLSafe(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		tok, err := randomToken()
		if err != nil {
			t.Fatal(err)
		}
		if len(tok) != 22 || !validToken(tok) {
			t.Fatalf("token %q: want 22 base64url characters", tok)
		}
		if seen[tok] {
			t.Fatal("token repeated")
		}
		seen[tok] = true
	}
}

// One link per video: everyone with access who shares gets the first link, as if forwarding the
// first message (ADR 0038).
func TestEveryoneWithAccessGetsTheSameLink(t *testing.T) {
	t.Parallel()
	store := newShareStore()
	store.grants[[2]int64{1, 100}] = struct {
		state int16
		title string
	}{GrantReady, "Trip"}
	sh := NewSharing(store)

	first, title, err := sh.Link(context.Background(), 100, 1)
	if err != nil || title != "Trip" || !strings.HasPrefix(first, SharePrefix) {
		t.Fatalf("Link = %q, %q, %v", first, title, err)
	}
	// A recipient claims it, then shares it again.
	if out, _, _ := sh.Claim(context.Background(), 200, first); out != ClaimAdded {
		t.Fatalf("claim = %v, want added", out)
	}
	again, title2, err := sh.Link(context.Background(), 200, 1)
	if err != nil || again != first || title2 != "Trip" {
		t.Fatalf("reshare = %q, %q, %v; want the same link and title", again, title2, err)
	}
}

func TestShareNeedsAReadyGrant(t *testing.T) {
	t.Parallel()
	store := newShareStore()
	store.grants[[2]int64{1, 100}] = struct {
		state int16
		title string
	}{GrantAwaitingTitle, "sugg"}
	sh := NewSharing(store)
	for _, tg := range []int64{100, 999} {
		if _, _, err := sh.Link(context.Background(), tg, 1); !errors.Is(err, ErrNoAccess) {
			t.Errorf("Link(tg=%d) err = %v, want ErrNoAccess", tg, err)
		}
	}
}

func TestClaimCarriesTheLinkTitleAndIsIdempotent(t *testing.T) {
	t.Parallel()
	store := newShareStore()
	store.grants[[2]int64{1, 100}] = struct {
		state int16
		title string
	}{GrantReady, "Trip"}
	sh := NewSharing(store)
	link, _, _ := sh.Link(context.Background(), 100, 1)

	out, title, err := sh.Claim(context.Background(), 300, link)
	if err != nil || out != ClaimAdded || title != "Trip" {
		t.Fatalf("claim = %v, %q, %v", out, title, err)
	}
	if store.grants[[2]int64{1, 300}].title != "Trip" {
		t.Error("recipient's grant does not carry the link title")
	}
	if out, _, _ := sh.Claim(context.Background(), 300, link); out != ClaimAlreadyHad {
		t.Errorf("second claim = %v, want already had", out)
	}
	if out, _, _ := sh.Claim(context.Background(), 100, link); out != ClaimAlreadyHad {
		t.Errorf("sharer claiming own link = %v, want already had", out)
	}
}

func TestClaimRejectsJunkAndUnknownLinks(t *testing.T) {
	t.Parallel()
	sh := NewSharing(newShareStore())
	for _, p := range []string{"", "pv_", "pv_short", "pv_" + strings.Repeat("a", 22) + "'", "xx_" + strings.Repeat("a", 22), "pv_" + strings.Repeat("a", 22)} {
		if out, _, err := sh.Claim(context.Background(), 1, p); err != nil || out != ClaimInvalid {
			t.Errorf("Claim(%q) = %v, %v; want invalid", p, out, err)
		}
	}
}
