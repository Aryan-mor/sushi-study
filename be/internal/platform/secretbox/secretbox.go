// Package secretbox is authenticated encryption for the few values Sushi has to keep and cannot
// keep in the clear.
//
// Today that is two: the BotFather token of a user's personal bot, which main-bot stores so the
// app login handoff can be re-issued without asking for it again (ADR 0013), and the user's Gemini
// API key, which main-bot seals and apid opens on /prefs so the app can translate subtitles
// (doc 15 §12). AGENTS.md is unambiguous that a token never goes into the database, and a value
// that has to come back out again cannot be hashed, so it is sealed instead.
//
// AES-256-GCM, nonce prefixed to the ciphertext, and the owner's user id bound in as additional
// data -- so a row lifted out of one user's record and dropped into another's fails to open rather
// than handing over somebody else's bot. The key lives in Infisical (doc 08) and reaches the
// process as SUSHI_SECRET_KEY; the database never sees it and has no way to decrypt anything it
// stores.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// DevKey is the key a dev environment falls back to when SUSHI_SECRET_KEY is unset.
//
// It is published here on purpose. Dev exists to be run from a clean checkout, and a secret that
// every developer has is not a secret -- so rather than pretend, this one is a visible constant
// that config refuses outside dev and main-bot warns about at boot. Anything sealed with it is
// sealed with a key the whole world has, which is the correct amount of protection for a token
// somebody minted to test a login flow.
const DevKey = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"

// ErrNoKey is returned by a zero Box: the process was never given a key, so it can neither store
// nor read a secret. Callers surface it rather than falling back to plaintext.
var ErrNoKey = errors.New("secretbox: no key configured (SUSHI_SECRET_KEY)")

// Box seals and opens values under one key. The zero Box has no key and fails closed.
type Box struct {
	aead cipher.AEAD
}

// New parses a 32-byte key, hex-encoded (64 characters).
//
// Hex rather than base64 because a key travels through Infisical, a .env file, a shell and a
// container env before it arrives, and hex is the one encoding none of those will quietly mangle.
func New(key string) (Box, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return Box{}, ErrNoKey
	}
	raw, err := hex.DecodeString(key)
	if err != nil {
		return Box{}, fmt.Errorf("secretbox: key is not hex: %w", err)
	}
	if len(raw) != 32 {
		return Box{}, fmt.Errorf("secretbox: key is %d bytes, want 32 (64 hex characters)", len(raw))
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return Box{}, fmt.Errorf("secretbox: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return Box{}, fmt.Errorf("secretbox: %w", err)
	}
	return Box{aead: aead}, nil
}

// Ready reports whether this Box has a key.
func (b Box) Ready() bool { return b.aead != nil }

// Seal encrypts plain, binding owner to the result. The nonce is prefixed, so the return value is
// the whole thing to store and nothing else needs keeping.
func (b Box) Seal(plain string, owner []byte) ([]byte, error) {
	if !b.Ready() {
		return nil, ErrNoKey
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secretbox: nonce: %w", err)
	}
	return b.aead.Seal(nonce, nonce, []byte(plain), owner), nil
}

// Open decrypts what Seal produced. It fails if the ciphertext was touched, if the key is not the
// one it was sealed with, or if owner is not the owner it was sealed for.
func (b Box) Open(sealed, owner []byte) (string, error) {
	if !b.Ready() {
		return "", ErrNoKey
	}
	n := b.aead.NonceSize()
	if len(sealed) < n {
		return "", errors.New("secretbox: ciphertext is too short to hold a nonce")
	}
	plain, err := b.aead.Open(nil, sealed[:n], sealed[n:], owner)
	if err != nil {
		return "", fmt.Errorf("secretbox: open: %w", err)
	}
	return string(plain), nil
}
