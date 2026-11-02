package wire_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/Aryan-mor/sushi-study/be/internal/wire"
)

func TestRoundTrip(t *testing.T) {
	t.Parallel()

	cases := map[string]wire.Envelope{
		"empty payload": {Corr: 7, Type: 3},
		"small payload": {Corr: 1, Type: 1, Payload: []byte("home")},
		"push":          {Corr: wire.PushCorr, Type: 9, Payload: []byte{0x00, 0xff, 0x10}},
		"large corr":    {Corr: 1 << 40, Type: 1 << 20, Payload: []byte("x")},
		"binary":        {Corr: 2, Type: 2, Payload: []byte{0, 1, 2, 250, 251, 252, 253, 254, 255}},
	}

	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			out, err := wire.Decode(mustEncode(t, in))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if out.Corr != in.Corr {
				t.Errorf("corr = %d, want %d", out.Corr, in.Corr)
			}
			if out.Type != in.Type {
				t.Errorf("type = %d, want %d", out.Type, in.Type)
			}
			if len(out.Payload) != len(in.Payload) || !bytes.Equal(out.Payload, in.Payload) {
				t.Errorf("payload = %v, want %v", out.Payload, in.Payload)
			}
		})
	}
}

func TestTextRoundTrip(t *testing.T) {
	t.Parallel()

	in := wire.Envelope{Corr: 42, Type: 1, Payload: []byte("compact rows go here")}
	s, err := wire.EncodeText(in)
	if err != nil {
		t.Fatalf("EncodeText: %v", err)
	}
	if s[0] != wire.TextMarker {
		t.Errorf("text = %q, want it to start with %q", s, wire.TextMarker)
	}
	if strings.ContainsAny(s, "+/=") {
		t.Errorf("text = %q, want base64url without padding", s)
	}

	out, err := wire.DecodeText(s)
	if err != nil {
		t.Fatalf("DecodeText: %v", err)
	}
	if !bytes.Equal(out.Payload, in.Payload) {
		t.Errorf("payload = %q, want %q", out.Payload, in.Payload)
	}
}

func TestCompressionOnlyWhenItWins(t *testing.T) {
	t.Parallel()

	// Compressible: a long run of repeated structure, like a rail of similar rows.
	compressible := bytes.Repeat([]byte("sushi_movie_row_0123456789"), 60)
	raw, err := wire.Encode(wire.Envelope{Corr: 1, Type: 1, Payload: compressible})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(raw) >= len(compressible) {
		t.Errorf("framed %d bytes for a %d-byte compressible payload; compression did not kick in",
			len(raw), len(compressible))
	}

	// Incompressible: real random bytes, which is what poster paths behave like. The framing must
	// not make it bigger than the payload plus a small header.
	incompressible := randomBytes(t, 1000)
	raw2, err := wire.Encode(wire.Envelope{Corr: 1, Type: 1, Payload: incompressible})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(raw2) > len(incompressible)+8 {
		t.Errorf("framed %d bytes for %d incompressible bytes; the encoder kept a losing compression",
			len(raw2), len(incompressible))
	}

	// Either way the payload survives.
	for _, want := range [][]byte{compressible, incompressible} {
		framed := mustEncode(t, wire.Envelope{Corr: 1, Type: 1, Payload: want})
		got, err := wire.Decode(framed)
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if !bytes.Equal(got.Payload, want) {
			t.Fatalf("payload did not survive the round trip (%d bytes)", len(want))
		}
	}
}

func TestDecodeRejectsHostileInput(t *testing.T) {
	t.Parallel()

	cases := map[string][]byte{
		"empty":              {},
		"version 0":          {0x00, 0x01, 0x01, 0x00},
		"future version":     {0x99, 0x01, 0x01, 0x00},
		"truncated corr":     {wire.Version},
		"truncated type":     {wire.Version, 0x01},
		"truncated flags":    {wire.Version, 0x01, 0x01},
		"reserved flag bits": {wire.Version, 0x01, 0x01, 0xFC, 0x01},
		"bad zstd":           {wire.Version, 0x01, 0x01, 0x01, 'n', 'o', 't', 'z', 's', 't', 'd'},
	}

	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := wire.Decode(raw); err == nil {
				t.Fatalf("Decode(%v) succeeded, want an error", raw)
			}
		})
	}
}

func TestDocumentEnvelopeCarriesNoPayload(t *testing.T) {
	t.Parallel()

	// The header arrives in a caption; the bytes are the attachment. Anything else means the two
	// sides disagree about where the payload is.
	raw := []byte{wire.Version, 0x01, 0x01, byte(wire.FlagDocument), 'x', 'y'}
	_, err := wire.Decode(raw)
	if err == nil {
		t.Fatal("want an error for a document envelope with trailing bytes")
	}
	if !errors.Is(err, wire.ErrMalformed) {
		t.Errorf("err = %v, want ErrMalformed", err)
	}

	ok, err := wire.Decode([]byte{wire.Version, 0x01, 0x01, byte(wire.FlagDocument)})
	if err != nil {
		t.Fatalf("a bare document envelope should decode: %v", err)
	}
	if ok.Flags&wire.FlagDocument == 0 {
		t.Error("document flag was lost")
	}
}

func TestTextTooLargeBecomesADocument(t *testing.T) {
	t.Parallel()

	// Must be genuinely random: an arithmetic pattern looks incompressible but zstd eats it, and
	// the payload would slip under the limit and quietly not test anything.
	big := randomBytes(t, wire.MaxTextPayload+500)
	e := wire.Envelope{Corr: 1, Type: 1, Payload: big}

	if _, err := wire.EncodeText(e); !errors.Is(err, wire.ErrTooLarge) {
		t.Errorf("EncodeText err = %v, want ErrTooLarge", err)
	}
	doc, err := wire.ShouldSendAsDocument(e)
	if err != nil {
		t.Fatalf("ShouldSendAsDocument: %v", err)
	}
	if !doc {
		t.Error("ShouldSendAsDocument = false for an oversize payload")
	}

	// And a payload that does fit stays a text message.
	small := wire.Envelope{Corr: 1, Type: 1, Payload: []byte("small")}
	if doc, err := wire.ShouldSendAsDocument(small); err != nil || doc {
		t.Errorf("ShouldSendAsDocument(small) = %v, %v; want false, nil", doc, err)
	}
}

func TestDecodeTextRejectsUnmarkedInput(t *testing.T) {
	t.Parallel()

	// Anything else that lands in the chat: a human, a service message, another bot's prose.
	for _, s := range []string{"", "hello", "/home 7", base64.RawURLEncoding.EncodeToString([]byte{1, 2, 3})} {
		if _, err := wire.DecodeText(s); err == nil {
			t.Errorf("DecodeText(%q) succeeded, want an error", s)
		}
	}
}

func TestDecodedPayloadIsACopy(t *testing.T) {
	t.Parallel()

	raw, err := wire.Encode(wire.Envelope{Corr: 1, Type: 1, Payload: []byte("abcd")})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := wire.Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	// Scribbling on the transport's buffer must not change what the caller already holds.
	for i := range raw {
		raw[i] = 0
	}
	if string(out.Payload) != "abcd" {
		t.Errorf("payload = %q; Decode returned a view into the input buffer", out.Payload)
	}
}

// mustEncode frames an envelope or fails the test; encoding valid input cannot fail.
func mustEncode(tb testing.TB, e wire.Envelope) []byte {
	tb.Helper()

	raw, err := wire.Encode(e)
	if err != nil {
		tb.Fatalf("Encode: %v", err)
	}
	return raw
}

// randomBytes returns bytes that do not compress, deterministically.
func randomBytes(tb testing.TB, n int) []byte {
	tb.Helper()

	r := rand.New(rand.NewPCG(0x5115, 0xb07))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

func FuzzDecode(f *testing.F) {
	f.Add(mustEncode(f, wire.Envelope{Corr: 1, Type: 1, Payload: []byte("hello")}))
	f.Add(mustEncode(f, wire.Envelope{Corr: 2, Type: 2, Payload: bytes.Repeat([]byte("ab"), 500)}))
	f.Add([]byte{wire.Version})
	f.Add([]byte{wire.Version, 0xff, 0xff, 0xff, 0xff})

	f.Fuzz(func(t *testing.T, raw []byte) {
		// The contract is only that it never panics and never returns an oversize payload.
		e, err := wire.Decode(raw)
		if err != nil {
			return
		}
		if len(e.Payload) > wire.MaxPayload {
			t.Fatalf("accepted a %d-byte payload, limit is %d", len(e.Payload), wire.MaxPayload)
		}
		if e.Flags&wire.FlagCompressed != 0 {
			t.Fatalf("returned a payload still marked compressed")
		}
	})
}

func FuzzDecodeText(f *testing.F) {
	text, err := wire.EncodeText(wire.Envelope{Corr: 1, Type: 1, Payload: []byte("hi")})
	if err != nil {
		f.Fatalf("EncodeText: %v", err)
	}
	f.Add(text)
	f.Add("!")
	f.Add("!!!!")
	f.Add("/home 7")

	f.Fuzz(func(t *testing.T, s string) {
		if _, err := wire.DecodeText(s); err != nil {
			return
		}
	})
}

func TestIsPush(t *testing.T) {
	t.Parallel()

	if !(wire.Envelope{Corr: wire.PushCorr}).IsPush() {
		t.Error("corr 0 should be a push (doc 02 §8)")
	}
	if (wire.Envelope{Corr: 1}).IsPush() {
		t.Error("corr 1 is an answer to a request, not a push")
	}
}

func TestEncodeRejectsBadInput(t *testing.T) {
	t.Parallel()

	// Oversize payload: refuse rather than hand Telegram something it will reject.
	_, err := wire.Encode(wire.Envelope{Corr: 1, Type: 1, Payload: make([]byte, wire.MaxPayload+1)})
	if !errors.Is(err, wire.ErrTooLarge) {
		t.Errorf("Encode(oversize) err = %v, want ErrTooLarge", err)
	}

	// Reserved flag bits: a caller setting them means it expects behaviour we do not have.
	_, err = wire.Encode(wire.Envelope{Corr: 1, Type: 1, Flags: 1 << 5})
	if !errors.Is(err, wire.ErrMalformed) {
		t.Errorf("Encode(reserved flags) err = %v, want ErrMalformed", err)
	}

	// The same failures must surface through the text and document helpers, not be swallowed.
	if _, err := wire.EncodeText(wire.Envelope{Corr: 1, Flags: 1 << 5}); !errors.Is(err, wire.ErrMalformed) {
		t.Errorf("EncodeText err = %v, want ErrMalformed", err)
	}
	if _, err := wire.ShouldSendAsDocument(wire.Envelope{Corr: 1, Flags: 1 << 5}); !errors.Is(err, wire.ErrMalformed) {
		t.Errorf("ShouldSendAsDocument err = %v, want ErrMalformed", err)
	}
}

func TestCompressedPayloadFitsTextWhenItShould(t *testing.T) {
	t.Parallel()

	// A rail that is too big raw but compresses under the limit still travels as text — that is
	// the whole reason compression happens before the size decision.
	repetitive := bytes.Repeat([]byte("row:1234567890:poster:"), 400) // ~8.8 KB raw
	if len(repetitive) <= wire.MaxTextPayload {
		t.Fatalf("test payload is only %d bytes; it must exceed the text limit raw", len(repetitive))
	}

	e := wire.Envelope{Corr: 5, Type: 1, Payload: repetitive}
	doc, err := wire.ShouldSendAsDocument(e)
	if err != nil {
		t.Fatalf("ShouldSendAsDocument: %v", err)
	}
	if doc {
		t.Error("a payload that compresses under the limit should stay a text message")
	}

	text, err := wire.EncodeText(e)
	if err != nil {
		t.Fatalf("EncodeText: %v", err)
	}
	out, err := wire.DecodeText(text)
	if err != nil {
		t.Fatalf("DecodeText: %v", err)
	}
	if !bytes.Equal(out.Payload, repetitive) {
		t.Error("payload did not survive compress -> base64 -> decode")
	}
}
