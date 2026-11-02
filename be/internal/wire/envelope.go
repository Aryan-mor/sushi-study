// Package wire implements the framed unit of the bot protocol described in docs/03-wire-format.md.
//
//	+--------+-------------+-------------+-------------+---------------+
//	| ver:u8 | corr:varint | type:varint | flags:varint | payload:bytes |
//	+--------+-------------+-------------+-------------+---------------+
//
// The payload is opaque here. Encoding it is the schema layer's job; this package only frames,
// compresses and base64-wraps, so it can be tested — and fuzzed — without any business types.
//
// Everything a client sends passes through Decode, which means Decode parses bytes an attacker
// chooses. It is written accordingly: bounded allocations, explicit limits, no panics, and a hard
// no to input that is merely *plausible* (R-PROTO-4).
package wire

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/klauspost/compress/zstd"
)

// Version is the envelope layout version. It changes only if the layout above changes, which is
// expected to be never — message evolution happens in the payload schema instead.
const Version uint8 = 1

// TextMarker prefixes a protocol message in a chat, so a client can ignore everything else that
// lands there: Telegram service messages, a human typing, a bot's own prose.
const TextMarker = '!'

// MaxTextPayload is the largest binary envelope we will put in a text message.
//
// Telegram's limit is 4096 UTF-16 code units and base64url is ASCII, so 4096 chars ≈ 3072 bytes.
// 2800 leaves room for the marker and any future prefix; anything larger travels as a document,
// which costs exactly the same one message against the rate limit (doc 03 §5).
const MaxTextPayload = 2800

// MaxPayload bounds a decoded payload, including one that arrived compressed. It exists so a
// hostile 40-byte message cannot ask us to allocate a gigabyte.
const MaxPayload = 8 << 20 // 8 MiB

// Flags describe how to read the payload.
type Flags uint64

const (
	// FlagCompressed means the payload is zstd-compressed.
	FlagCompressed Flags = 1 << 0
	// FlagDocument means the payload is not here: it is the attached Telegram document, and this
	// envelope is only the header that arrived in the caption.
	FlagDocument Flags = 1 << 1

	// flagsKnown is every bit we understand. Anything else is rejected rather than ignored: an
	// unknown flag means the sender expects behaviour we do not implement.
	flagsKnown = FlagCompressed | FlagDocument
)

// PushCorr marks a message the server sent unprompted, rather than an answer to a request.
const PushCorr uint64 = 0

// Envelope is one protocol message.
type Envelope struct {
	Corr    uint64
	Type    uint64
	Flags   Flags
	Payload []byte
}

// IsPush reports whether this arrived unsolicited (doc 02 §8).
func (e Envelope) IsPush() bool { return e.Corr == PushCorr }

var (
	// ErrMalformed covers every "these bytes are not an envelope" case. Callers answer all of
	// them the same way, so they are one error with a specific message rather than a taxonomy.
	ErrMalformed = errors.New("wire: malformed envelope")
	// ErrTooLarge means a payload exceeded MaxPayload, before or after decompression.
	ErrTooLarge = errors.New("wire: payload too large")
	// ErrUnsupportedVersion means the sender speaks a newer envelope layout than we do.
	ErrUnsupportedVersion = errors.New("wire: unsupported envelope version")
)

var (
	zstdEncoder *zstd.Encoder
	zstdDecoder *zstd.Decoder
)

// The two constructors below can only fail on invalid options, and ours are compile-time
// constants — so a failure here means the binary is built wrong, not that a request went wrong.
// Panicking at init is the MustCompile convention, and is the one place R-GO-10 allows it: it
// cannot happen at runtime, and if it somehow did, continuing would be worse.
func init() {
	var err error
	// Level 3: the payloads are kilobytes of short strings and varints; anything slower buys
	// nothing measurable (doc 03 §4).
	zstdEncoder, err = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		panic("wire: zstd encoder: " + err.Error())
	}
	zstdDecoder, err = zstd.NewReader(nil, zstd.WithDecoderMaxMemory(MaxPayload))
	if err != nil {
		panic("wire: zstd decoder: " + err.Error())
	}
}

// Encode frames an envelope, compressing the payload when that actually helps.
//
// "Actually helps" matters: compact rows are mostly random-looking poster paths and packed varints,
// and zstd can make those *larger*. The flag is set only when the compressed form wins.
func Encode(e Envelope) ([]byte, error) {
	if len(e.Payload) > MaxPayload {
		return nil, fmt.Errorf("%w: %d bytes", ErrTooLarge, len(e.Payload))
	}
	if e.Flags&^flagsKnown != 0 {
		return nil, fmt.Errorf("%w: unknown flags %#b", ErrMalformed, e.Flags&^flagsKnown)
	}

	payload := e.Payload
	flags := e.Flags

	if flags&FlagDocument == 0 && flags&FlagCompressed == 0 && len(payload) >= 256 {
		if packed := zstdEncoder.EncodeAll(payload, nil); len(packed) < len(payload)-16 {
			payload = packed
			flags |= FlagCompressed
		}
	}

	out := make([]byte, 0, 1+3*binary.MaxVarintLen64+len(payload))
	out = append(out, Version)
	out = binary.AppendUvarint(out, e.Corr)
	out = binary.AppendUvarint(out, e.Type)
	out = binary.AppendUvarint(out, uint64(flags))
	out = append(out, payload...)
	return out, nil
}

// Decode parses a framed envelope and returns the payload decompressed.
func Decode(raw []byte) (Envelope, error) {
	if len(raw) == 0 {
		return Envelope{}, fmt.Errorf("%w: empty", ErrMalformed)
	}
	if raw[0] != Version {
		return Envelope{}, fmt.Errorf("%w: %d", ErrUnsupportedVersion, raw[0])
	}

	rest := raw[1:]
	corr, n := binary.Uvarint(rest)
	if n <= 0 {
		return Envelope{}, fmt.Errorf("%w: bad corr", ErrMalformed)
	}
	rest = rest[n:]

	msgType, n := binary.Uvarint(rest)
	if n <= 0 {
		return Envelope{}, fmt.Errorf("%w: bad type", ErrMalformed)
	}
	rest = rest[n:]

	rawFlags, n := binary.Uvarint(rest)
	if n <= 0 {
		return Envelope{}, fmt.Errorf("%w: bad flags", ErrMalformed)
	}
	rest = rest[n:]

	flags := Flags(rawFlags)
	if flags&^flagsKnown != 0 {
		// Reserved bits are reserved. Ignoring them would mean answering a request whose terms we
		// do not actually understand.
		return Envelope{}, fmt.Errorf("%w: unknown flags %#b", ErrMalformed, flags&^flagsKnown)
	}
	if flags&FlagDocument != 0 && len(rest) != 0 {
		// A document envelope is a header; the bytes live in the attachment. Trailing data here
		// means the sender and we disagree about where the payload is.
		return Envelope{}, fmt.Errorf("%w: document envelope carries %d trailing bytes", ErrMalformed, len(rest))
	}
	if len(rest) > MaxPayload {
		return Envelope{}, fmt.Errorf("%w: %d bytes", ErrTooLarge, len(rest))
	}

	payload := rest
	if flags&FlagCompressed != 0 {
		out, err := zstdDecoder.DecodeAll(rest, make([]byte, 0, min(len(rest)*4, MaxPayload)))
		if err != nil {
			return Envelope{}, fmt.Errorf("%w: zstd: %w", ErrMalformed, err)
		}
		if len(out) > MaxPayload {
			return Envelope{}, fmt.Errorf("%w: %d bytes decompressed", ErrTooLarge, len(out))
		}
		payload = out
		flags &^= FlagCompressed // the caller gets plain bytes; how they travelled is our business
	}

	// Copy so the caller cannot retain a slice of the transport's buffer.
	cp := make([]byte, len(payload))
	copy(cp, payload)

	return Envelope{Corr: corr, Type: msgType, Flags: flags, Payload: cp}, nil
}

// EncodeText renders an envelope for a Telegram text message: '!' + base64url, unpadded.
//
// It returns ErrTooLarge when the result would not fit a text message; the caller should then send
// the raw Encode bytes as a document instead (see ShouldSendAsDocument).
func EncodeText(e Envelope) (string, error) {
	raw, err := Encode(e)
	if err != nil {
		return "", err
	}
	if len(raw) > MaxTextPayload {
		return "", fmt.Errorf("%w: %d bytes framed, limit %d", ErrTooLarge, len(raw), MaxTextPayload)
	}
	return string(TextMarker) + base64.RawURLEncoding.EncodeToString(raw), nil
}

// DecodeText parses a text-framed envelope.
func DecodeText(s string) (Envelope, error) {
	if len(s) == 0 || s[0] != TextMarker {
		return Envelope{}, fmt.Errorf("%w: missing %q marker", ErrMalformed, TextMarker)
	}
	raw, err := base64.RawURLEncoding.DecodeString(s[1:])
	if err != nil {
		return Envelope{}, fmt.Errorf("%w: base64: %w", ErrMalformed, err)
	}
	return Decode(raw)
}

// ShouldSendAsDocument reports whether a framed envelope is too big for a text message.
//
// A document is still one message, so it costs the same against the per-chat rate limit — which is
// why chunking across several text messages was rejected (doc 03 §5).
func ShouldSendAsDocument(e Envelope) (bool, error) {
	raw, err := Encode(e)
	if err != nil {
		return false, err
	}
	return len(raw) > MaxTextPayload, nil
}
