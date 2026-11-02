package api_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/app/api"
	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
)

type stubGeminiKeys struct {
	key string
	ok  bool
	err error
}

func (s stubGeminiKeys) GeminiAPIKey(context.Context, int64) (string, bool, error) {
	return s.key, s.ok, s.err
}

func TestPrefsReturnsTheKeyWhenPresent(t *testing.T) {
	t.Parallel()

	svc := newAPI(&fakeRails{}).WithGeminiKeys(stubGeminiKeys{
		key: "AIzaSyxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		ok:  true,
	})
	res, err := svc.Prefs(context.Background(), api.Principal{UserID: 7}, &sushiv1.PrefsReq{})
	if err != nil {
		t.Fatalf("Prefs: %v", err)
	}
	if !res.GetHasGeminiKey() || res.GetGeminiApiKey() != "AIzaSyxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" {
		t.Fatalf("got has=%v key=%q", res.GetHasGeminiKey(), res.GetGeminiApiKey())
	}
}

func TestPrefsEmptyWhenTheUserHasNoKey(t *testing.T) {
	t.Parallel()

	svc := newAPI(&fakeRails{}).WithGeminiKeys(stubGeminiKeys{})
	res, err := svc.Prefs(context.Background(), api.Principal{UserID: 7}, &sushiv1.PrefsReq{})
	if err != nil {
		t.Fatalf("Prefs: %v", err)
	}
	if res.GetHasGeminiKey() || res.GetGeminiApiKey() != "" {
		t.Fatalf("got has=%v key=%q", res.GetHasGeminiKey(), res.GetGeminiApiKey())
	}
}

func TestPrefsErrorsWhenNotConfigured(t *testing.T) {
	t.Parallel()

	_, err := newAPI(&fakeRails{}).Prefs(context.Background(), api.Principal{UserID: 7}, &sushiv1.PrefsReq{})
	if err == nil {
		t.Fatal("Prefs without WithGeminiKeys should error, not claim there is no key")
	}
}

func TestPrefsSurfacesStoreErrors(t *testing.T) {
	t.Parallel()

	svc := newAPI(&fakeRails{}).WithGeminiKeys(stubGeminiKeys{err: errors.New("open failed")})
	if _, err := svc.Prefs(context.Background(), api.Principal{UserID: 7}, &sushiv1.PrefsReq{}); err == nil {
		t.Fatal("Prefs should surface a store error")
	}
}
