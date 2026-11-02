package backup

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/pglib"
)

// fakePersonal is personal_video_locations in memory: a clone row is "missing backup" until a
// personal_backup location is recorded for its video.
type fakePersonal struct {
	mu      sync.Mutex
	clone   []pglib.PersonalCloneMissingBackupRow
	saved   map[int64]pglib.SetPersonalLocationParams
	saveErr map[int64]error
}

func (s *fakePersonal) PersonalCloneMissingBackup(
	_ context.Context, arg pglib.PersonalCloneMissingBackupParams,
) ([]pglib.PersonalCloneMissingBackupRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []pglib.PersonalCloneMissingBackupRow{}
	for _, r := range s.clone {
		if r.VideoID <= arg.AfterVideoID {
			continue
		}
		if _, done := s.saved[r.VideoID]; done {
			continue
		}
		out = append(out, r)
		if len(out) == int(arg.PageSize) {
			break
		}
	}
	return out, nil
}

func (s *fakePersonal) SetPersonalLocation(_ context.Context, arg pglib.SetPersonalLocationParams) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.saveErr[arg.VideoID]; err != nil {
		return err
	}
	s.saved[arg.VideoID] = arg
	return nil
}

func TestPersonalPassBacksUpEveryPersonalCloneCopy(t *testing.T) {
	t.Parallel()

	personal := &fakePersonal{
		clone: []pglib.PersonalCloneMissingBackupRow{
			{VideoID: 1, ChannelID: -100700, MessageID: 71},
			{VideoID: 2, ChannelID: -100700, MessageID: 72},
		},
		saved:   map[int64]pglib.SetPersonalLocationParams{},
		saveErr: map[int64]error{},
	}
	catalogBot, personalBot := newBot(), newBot()
	r, _ := newTestRunner(newStore(), []Slot{slot("backup1", -100222, catalogBot)})
	r.WithPersonal(personal, slot("personal_backup", -100800, personalBot))

	if err := r.Run(t.Context(), true); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, id := range []int64{1, 2} {
		got, ok := personal.saved[id]
		if !ok {
			t.Fatalf("video %d has no personal_backup location", id)
		}
		if got.Role != "personal_backup" || got.ChannelID != -100800 {
			t.Errorf("video %d recorded as %+v, want role personal_backup in -100800", id, got)
		}
	}
	for _, c := range personalBot.attempts() {
		if c.dest != -100800 || c.source != -100700 {
			t.Errorf("personal copy %+v, want personal_clone -100700 -> personal_backup -100800", c)
		}
	}
	if n := len(catalogBot.attempts()); n != 0 {
		t.Errorf("catalog slot copied %d messages; personal rows must never reach it", n)
	}
}

// A personal row that cannot be copied costs one attempt and is stepped over, like a catalog one.
func TestPersonalPassStepsOverAFailingRow(t *testing.T) {
	t.Parallel()

	personal := &fakePersonal{
		clone: []pglib.PersonalCloneMissingBackupRow{
			{VideoID: 1, ChannelID: -100700, MessageID: 71},
			{VideoID: 2, ChannelID: -100700, MessageID: 72},
		},
		saved:   map[int64]pglib.SetPersonalLocationParams{},
		saveErr: map[int64]error{},
	}
	bot := newBot()
	bot.fail[71] = errors.New("message to copy not found")
	r, _ := newTestRunner(newStore(), nil)
	r.WithPersonal(personal, slot("personal_backup", -100800, bot))

	if err := r.Run(t.Context(), true); err == nil {
		t.Fatal("Run reported success with a row it could not copy")
	}
	if _, ok := personal.saved[2]; !ok {
		t.Error("video 2 was not backed up because video 1 failed")
	}
}
