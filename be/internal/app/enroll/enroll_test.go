package enroll_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/app/enroll"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
)

type fakeVerifier struct {
	tgUserID int64
	username string
	err      error
}

func (f fakeVerifier) VerifyToken(context.Context, string) (int64, string, error) {
	if f.err != nil {
		return 0, "", f.err
	}
	return f.tgUserID, f.username, nil
}

type fakeBinder struct {
	got struct {
		tgUserID, chatID int64
		readerUsername   string
	}
	binding identity.Binding
	err     error
}

func (f *fakeBinder) Bind(_ context.Context, tgUserID, chatID int64, readerUsername string) (identity.Binding, error) {
	f.got.tgUserID, f.got.chatID, f.got.readerUsername = tgUserID, chatID, readerUsername
	if f.err != nil {
		return identity.Binding{}, f.err
	}
	return f.binding, nil
}

func TestLooksLikeToken(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"123456789:AAFq7v9dR3n8xyzABCDEFGHIJKLMNOPQRSTU":     true,
		"  123456789:AAFq7v9dR3n8xyzABCDEFGHIJKLMNOPQRSTU  ": true,
		"hello there": false,
		"":            false,
		"/start":      false,
		"12345":       false,
		"12345:short": false,
	}
	for in, want := range cases {
		if got := enroll.LooksLikeToken(in); got != want {
			t.Errorf("LooksLikeToken(%q) = %v, want %v", in, got, want)
		}
	}
}

// The bot's own identity comes from Telegram, never from the pasted text -- that is the entire
// reason getMe is called before anything is written (docs/06 §3).
func TestEnrollBindsUsingTheVerifiedIdentityNotTheRawInput(t *testing.T) {
	t.Parallel()

	verifier := fakeVerifier{tgUserID: 987654321, username: "persons_own_bot"}
	binder := &fakeBinder{binding: identity.Binding{Epoch: 1}}

	binding, username, err := enroll.New(verifier, binder).Enroll(context.Background(), 111, "whatever-was-pasted")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if username != "persons_own_bot" {
		t.Errorf("username = %q, want verified handle", username)
	}
	if binder.got.tgUserID != 111 {
		t.Errorf("bound tgUserID = %d, want the person's own id 111", binder.got.tgUserID)
	}
	if binder.got.chatID != 987654321 {
		t.Errorf("bound chatID = %d, want the bot's verified id, not anything from the pasted text", binder.got.chatID)
	}
	if binder.got.readerUsername != "persons_own_bot" {
		t.Errorf("bound readerUsername = %q, want the verified handle", binder.got.readerUsername)
	}
	if binding.Epoch != 1 {
		t.Errorf("returned binding = %+v, want what the binder returned", binding)
	}
}

func TestEnrollRejectsATokenTelegramRefuses(t *testing.T) {
	t.Parallel()

	verifier := fakeVerifier{err: errors.New("telegram: 401 unauthorised")}
	binder := &fakeBinder{}

	_, _, err := enroll.New(verifier, binder).Enroll(context.Background(), 111, "bad-token")
	if !errors.Is(err, enroll.ErrInvalidToken) {
		t.Errorf("err = %v, want it to wrap ErrInvalidToken", err)
	}
	if binder.got.chatID != 0 {
		t.Error("Bind was called despite verification failing")
	}
}

func TestEnrollSurfacesBindFailures(t *testing.T) {
	t.Parallel()

	verifier := fakeVerifier{tgUserID: 1, username: "a_bot"}
	want := errors.New("database is down")
	binder := &fakeBinder{err: want}

	_, _, err := enroll.New(verifier, binder).Enroll(context.Background(), 111, "a-token")
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want it to wrap %v", err, want)
	}
}
