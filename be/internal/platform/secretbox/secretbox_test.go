package secretbox_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/platform/secretbox"
)

const key = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestSealOpenRoundTrip(t *testing.T) {
	t.Parallel()

	box, err := secretbox.New(key)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	const token = "8000000000:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	sealed, err := box.Seal(token, []byte("reader_bot_token:7"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if bytes.Contains(sealed, []byte(token)) {
		t.Fatal("the plaintext token is still readable in the ciphertext")
	}
	got, err := box.Open(sealed, []byte("reader_bot_token:7"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got != token {
		t.Errorf("open = %q, want the token back", got)
	}
}

// The reason the owner is bound in: a row lifted out of one user's record and dropped into
// another's must not hand over somebody else's bot.
func TestOpenRefusesAnotherOwner(t *testing.T) {
	t.Parallel()

	box, err := secretbox.New(key)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	sealed, err := box.Seal("secret", []byte("reader_bot_token:7"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := box.Open(sealed, []byte("reader_bot_token:8")); err == nil {
		t.Fatal("opened under a different owner")
	}
}

func TestOpenRefusesATamperedCiphertext(t *testing.T) {
	t.Parallel()

	box, err := secretbox.New(key)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	sealed, err := box.Seal("secret", nil)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	sealed[len(sealed)-1] ^= 0xff
	if _, err := box.Open(sealed, nil); err == nil {
		t.Fatal("opened a tampered ciphertext")
	}
}

func TestNonceIsFreshPerSeal(t *testing.T) {
	t.Parallel()

	box, err := secretbox.New(key)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	a, err := box.Seal("secret", nil)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	b, err := box.Seal("secret", nil)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("two seals of the same value produced the same bytes; the nonce is not fresh")
	}
}

// A process with no key must fail closed, never fall back to storing plaintext.
func TestZeroBoxFailsClosed(t *testing.T) {
	t.Parallel()

	var box secretbox.Box
	if box.Ready() {
		t.Fatal("the zero Box claims to have a key")
	}
	if _, err := box.Seal("secret", nil); !errors.Is(err, secretbox.ErrNoKey) {
		t.Errorf("seal error = %v, want ErrNoKey", err)
	}
	if _, err := box.Open([]byte("whatever"), nil); !errors.Is(err, secretbox.ErrNoKey) {
		t.Errorf("open error = %v, want ErrNoKey", err)
	}
}

func TestNewRejectsAKeyOfTheWrongShape(t *testing.T) {
	t.Parallel()

	for name, k := range map[string]string{
		"empty":     "",
		"not hex":   strings.Repeat("z", 64),
		"too short": "00112233",
	} {
		if _, err := secretbox.New(k); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestDevKeyParses(t *testing.T) {
	t.Parallel()

	// config falls back to it in dev, so a typo here would only show up as a boot failure.
	if _, err := secretbox.New(secretbox.DevKey); err != nil {
		t.Fatalf("DevKey: %v", err)
	}
}
