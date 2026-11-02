package subdl

import (
	"strings"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/subplus"
)

func TestPickFetchedSubsEpisodeFromSeasonPack(t *testing.T) {
	t.Parallel()
	files := []subplus.SubFile{
		{Name: "Friends.S03E01.srt", Ext: ".srt", Text: "e01"},
		{Name: "Friends.S03E12.srt", Ext: ".srt", Text: "e12"},
	}
	got, err := pickFetchedSubs(files, 3, 12)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "e12" {
		t.Fatalf("got %q, want e12", got.Text)
	}
}

func TestPickFetchedSubsMissIsError(t *testing.T) {
	t.Parallel()
	files := []subplus.SubFile{
		{Name: "Friends.S03E01.srt", Ext: ".srt", Text: "e01"},
		{Name: "Friends.S03E02.srt", Ext: ".srt", Text: "e02"},
	}
	_, err := pickFetchedSubs(files, 3, 12)
	if err == nil || !strings.Contains(err.Error(), "no matching") {
		t.Fatalf("err = %v, want no matching", err)
	}
}
