package botlogin_test

import (
	"bytes"
	"crypto/rand"
	"strings"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/app/botlogin"
)

const goldenToken = "123456789:AAFq7v9dR3n8xyzABCDEFGHIJKLMNOPQRSTU"

func TestSealOpenRoundTrip(t *testing.T) {
	t.Parallel()

	nonce := make([]byte, botlogin.NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	blob, err := botlogin.Seal(goldenToken, nonce)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if !strings.HasPrefix(blob, botlogin.Prefix) {
		t.Fatalf("prefix = %q", blob)
	}
	got, err := botlogin.Open(blob, nonce)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got != goldenToken {
		t.Fatalf("token = %q, want golden", got)
	}
}

func TestOpenRejectsWrongNonce(t *testing.T) {
	t.Parallel()

	nonce := bytes.Repeat([]byte{0x01}, botlogin.NonceSize)
	blob, err := botlogin.Seal(goldenToken, nonce)
	if err != nil {
		t.Fatal(err)
	}
	other := bytes.Repeat([]byte{0x02}, botlogin.NonceSize)
	if _, err := botlogin.Open(blob, other); err == nil {
		t.Fatal("Open accepted the wrong nonce")
	}
}

func TestOpenRejectsJunk(t *testing.T) {
	t.Parallel()

	nonce := bytes.Repeat([]byte{0x03}, botlogin.NonceSize)
	for _, in := range []string{"", "s2.", "hello", goldenToken, "s1.abc", "s2.!!!!"} {
		if _, err := botlogin.Open(in, nonce); err == nil {
			t.Errorf("Open(%q) succeeded, want error", in)
		}
	}
}

func TestSealRejectsNonToken(t *testing.T) {
	t.Parallel()

	nonce := bytes.Repeat([]byte{0x04}, botlogin.NonceSize)
	if _, err := botlogin.Seal("not-a-token", nonce); err == nil {
		t.Fatal("Seal accepted junk")
	}
}

func TestParseStartNonce(t *testing.T) {
	t.Parallel()

	nonce := bytes.Repeat([]byte{0x11}, botlogin.NonceSize)
	payload, err := botlogin.EncodeStartPayload(nonce)
	if err != nil {
		t.Fatal(err)
	}
	got, err := botlogin.ParseStartNonce(payload)
	if err != nil {
		t.Fatalf("ParseStartNonce: %v", err)
	}
	if !bytes.Equal(got, nonce) {
		t.Fatalf("got %x", got)
	}
	if _, err := botlogin.ParseStartNonce("ac_k7x2"); err == nil {
		t.Fatal("short legacy nonce must fail")
	}
	if _, err := botlogin.ParseStartNonce("bot"); err == nil {
		t.Fatal("bot payload must fail")
	}
}

func TestIsPing(t *testing.T) {
	t.Parallel()

	if !botlogin.IsPing("sushi:b2b") || !botlogin.IsPing("  sushi:b2b\n") {
		t.Fatal("ping must match")
	}
	if botlogin.IsPing("/initbot") || botlogin.IsPing("") {
		t.Fatal("non-ping matched")
	}
}

func TestGoldenBlobMatchesDart(t *testing.T) {
	t.Parallel()

	nonce := bytes.Repeat([]byte{0x11}, botlogin.NonceSize)
	iv := bytes.Repeat([]byte{0x22}, 12)
	blob, err := botlogin.SealWithGCMNonce(goldenToken, nonce, iv)
	if err != nil {
		t.Fatal(err)
	}
	got, err := botlogin.Open(blob, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if got != goldenToken {
		t.Fatal("golden did not open")
	}
	t.Log(blob)
}

func TestSearchTag(t *testing.T) {
	t.Parallel()

	a := bytes.Repeat([]byte{0x11}, botlogin.NonceSize)
	b := bytes.Repeat([]byte{0x22}, botlogin.NonceSize)

	tagA, err := botlogin.SearchTag(a)
	if err != nil {
		t.Fatal(err)
	}
	if len(tagA) != botlogin.SearchTagSize {
		t.Fatalf("len(tag) = %d, want %d", len(tagA), botlogin.SearchTagSize)
	}
	for _, c := range tagA {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Fatalf("tag %q has non-hex rune %q", tagA, c)
		}
	}

	again, err := botlogin.SearchTag(a)
	if err != nil {
		t.Fatal(err)
	}
	if again != tagA {
		t.Fatalf("SearchTag not deterministic: %q vs %q", tagA, again)
	}

	tagB, err := botlogin.SearchTag(b)
	if err != nil {
		t.Fatal(err)
	}
	if tagB == tagA {
		t.Fatalf("different nonces produced the same tag: %q", tagA)
	}

	if _, err := botlogin.SearchTag(a[:4]); err == nil {
		t.Fatal("SearchTag accepted a short nonce")
	}
}

// TestSearchTagMatchesDart is the golden Go computes so the Dart port
// (sushiLoginSearchTag) can be checked against the same nonce.
func TestSearchTagMatchesDart(t *testing.T) {
	t.Parallel()

	nonce := bytes.Repeat([]byte{0x11}, botlogin.NonceSize)
	tag, err := botlogin.SearchTag(nonce)
	if err != nil {
		t.Fatal(err)
	}
	const want = "0eb1feafa2" // keep in sync with sushi_login_seal_test.dart
	if tag != want {
		t.Fatalf("tag = %q, want %q (update the Dart golden alongside this one)", tag, want)
	}
}

func TestExtractBlobs(t *testing.T) {
	t.Parallel()

	nonce := bytes.Repeat([]byte{0x05}, botlogin.NonceSize)
	blob, err := botlogin.Seal(goldenToken, nonce)
	if err != nil {
		t.Fatal(err)
	}
	html := `<div class="tgme_widget_message_text">test</div>` +
		`<div class="tgme_widget_message_text">` + blob + `</div>`
	found := botlogin.ExtractBlobs(html)
	if len(found) != 1 || found[0] != blob {
		t.Fatalf("ExtractBlobs = %v, want [%s]", found, blob)
	}
}
