package loginpost

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

type memStore struct {
	pending []Pending
	posted  []Posted
	expired []Posted
}

func (m *memStore) PendingLoginPosts(context.Context) ([]Pending, error) {
	return append([]Pending(nil), m.pending...), nil
}

func (m *memStore) MarkLoginPostPosted(_ context.Context, id, channelMessageID int64) error {
	var rest []Pending
	for _, p := range m.pending {
		if p.ID == id {
			m.posted = append(m.posted, Posted{ID: id, ChannelMessageID: channelMessageID})
			continue
		}
		rest = append(rest, p)
	}
	m.pending = rest
	return nil
}

func (m *memStore) LoginPostsToDelete(context.Context) ([]Posted, error) {
	return append([]Posted(nil), m.expired...), nil
}

func (m *memStore) DeleteLoginPost(_ context.Context, id int64) error {
	var rest []Posted
	for _, p := range m.expired {
		if p.ID != id {
			rest = append(rest, p)
		}
	}
	m.expired = rest
	return nil
}

type fakeCh struct {
	posts   []string
	deleted []int64
	nextID  int64
	fail    error
}

func (f *fakeCh) Post(_ context.Context, _ int64, text string) (int64, error) {
	if f.fail != nil {
		return 0, f.fail
	}
	f.nextID++
	f.posts = append(f.posts, text)
	return f.nextID, nil
}

func (f *fakeCh) Delete(_ context.Context, _ int64, messageID int64) error {
	f.deleted = append(f.deleted, messageID)
	return nil
}

func TestDrainOncePostsWithoutDeletingFresh(t *testing.T) {
	t.Parallel()

	st := &memStore{pending: []Pending{{ID: 7, Blob: "s2.abc"}}}
	ch := &fakeCh{}
	if err := drainOnce(context.Background(), st, ch, -100, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	if len(ch.posts) != 1 || ch.posts[0] != "s2.abc" {
		t.Fatalf("posts = %v", ch.posts)
	}
	if len(ch.deleted) != 0 {
		t.Fatalf("deleted fresh post: %v", ch.deleted)
	}
	if len(st.posted) != 1 || st.posted[0].ChannelMessageID != 1 {
		t.Fatalf("posted = %+v", st.posted)
	}
}

func TestDrainOnceDeletesExpired(t *testing.T) {
	t.Parallel()

	st := &memStore{expired: []Posted{{ID: 3, ChannelMessageID: 99}}}
	ch := &fakeCh{}
	if err := drainOnce(context.Background(), st, ch, -100, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	if len(ch.deleted) != 1 || ch.deleted[0] != 99 {
		t.Fatalf("deleted = %v", ch.deleted)
	}
	if len(st.expired) != 0 {
		t.Fatalf("expired leftover %v", st.expired)
	}
}

func TestDrainOnceStopsOnSendError(t *testing.T) {
	t.Parallel()

	st := &memStore{pending: []Pending{{ID: 1, Blob: "s2.x"}}}
	ch := &fakeCh{fail: errors.New("telegram down")}
	if err := drainOnce(context.Background(), st, ch, -100, slog.New(slog.DiscardHandler)); err == nil {
		t.Fatal("want send error")
	}
}
