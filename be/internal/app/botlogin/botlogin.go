// Package botlogin is the bot-login handoff (ADR 0013): a 16-byte nonce in the /start payload
// keys an AES-256-GCM blob that init-bot posts to the public preview channel.
package botlogin

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/Aryan-mor/sushi-study/be/internal/app/enroll"
)

const (
	// Prefix marks a channel blob so the client can ignore other posts ("test", "Channel created").
	Prefix = "s2."
	// StartPrefix is the Telegram ?start= prefix. The rest is base64url of NonceSize bytes.
	StartPrefix = "ac_"
	// NonceSize is the login nonce. Telegram start payloads cap at 64 characters; ac_ + 22 = 25.
	NonceSize = 16
	// PingText is sent from the person's bot to init-bot to prove Bot-to-Bot mode. init-bot
	// ignores it and never runs protocol on it.
	PingText = "sushi:b2b"
	info     = "sushi-bot-login-v1"
	gcmSize  = 12

	// searchTagInfo domain-separates the search tag hash from the AES key hash (both derive
	// from the same nonce) so publishing the tag never helps derive the decryption key.
	searchTagInfo = "sushi-bot-login-searchtag-v1"
	// SearchTagSize is the tag length in hex characters (5 bytes). Short enough to stay a
	// single Telegram search token, long enough that a collision in one login-post TTL window
	// on a low-traffic channel is not a practical concern.
	SearchTagSize = 10
)

// ErrInvalid means the text is not a bot-login blob, or Open failed.
var ErrInvalid = errors.New("botlogin: invalid blob")

// ErrNonce means the /start payload is not a 16-byte nonce.
var ErrNonce = errors.New("botlogin: invalid start nonce")

// IsPing reports the bot-to-bot probe text, so init-bot can drop it before EnsureUser.
func IsPing(text string) bool {
	return strings.TrimSpace(text) == PingText
}

// ParseStartNonce extracts the 16-byte nonce from a /start rest payload (`ac_<b64>`).
func ParseStartNonce(rest string) ([]byte, error) {
	r := strings.TrimSpace(rest)
	if len(r) < len(StartPrefix) || !strings.EqualFold(r[:len(StartPrefix)], StartPrefix) {
		return nil, ErrNonce
	}
	raw := r[len(StartPrefix):]
	if raw == "" {
		return nil, ErrNonce
	}
	nonce, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(nonce) != NonceSize {
		return nil, ErrNonce
	}
	return nonce, nil
}

// EncodeStartPayload is the ?start= value the app puts on the deep link.
func EncodeStartPayload(nonce []byte) (string, error) {
	if len(nonce) != NonceSize {
		return "", ErrNonce
	}
	return StartPrefix + base64.RawURLEncoding.EncodeToString(nonce), nil
}

func deriveKey(loginNonce []byte) []byte {
	h := sha256.New()
	_, _ = h.Write([]byte(info))
	_, _ = h.Write(loginNonce)
	return h.Sum(nil)
}

// SearchTag derives a public hashtag from the login nonce, so the app can find its own channel
// post with `t.me/s/<channel>?q=%23<tag>` instead of scanning the visible ~20-message tail (the
// original miss: a slow scan lets other posts push the blob off that tail before the poll runs).
// A different context string than deriveKey means the tag never helps derive the AES key, so
// publishing it does not let a channel scraper decrypt a blob without the nonce from the deep
// link/QR.
func SearchTag(loginNonce []byte) (string, error) {
	if len(loginNonce) != NonceSize {
		return "", ErrNonce
	}
	h := sha256.New()
	_, _ = h.Write([]byte(searchTagInfo))
	_, _ = h.Write(loginNonce)
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:SearchTagSize/2]), nil
}

// Seal encrypts a BotFather token under the login nonce. The GCM nonce is random and prefixed.
func Seal(token string, loginNonce []byte) (string, error) {
	iv := make([]byte, gcmSize)
	if _, err := rand.Read(iv); err != nil {
		return "", fmt.Errorf("botlogin: iv: %w", err)
	}
	return sealWith(token, loginNonce, iv)
}

// SealWithGCMNonce is the same as Seal with a chosen GCM nonce. Tests (and the Dart golden)
// need a deterministic blob; production always uses Seal.
func SealWithGCMNonce(token string, loginNonce, gcmNonce []byte) (string, error) {
	if len(gcmNonce) != gcmSize {
		return "", fmt.Errorf("%w: gcm nonce", ErrInvalid)
	}
	return sealWith(token, loginNonce, gcmNonce)
}

func sealWith(token string, loginNonce, iv []byte) (string, error) {
	token = strings.TrimSpace(token)
	if !enroll.LooksLikeToken(token) {
		return "", fmt.Errorf("%w: token shape", ErrInvalid)
	}
	if len(loginNonce) != NonceSize {
		return "", ErrNonce
	}
	block, err := aes.NewCipher(deriveKey(loginNonce))
	if err != nil {
		return "", fmt.Errorf("botlogin: cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("botlogin: gcm: %w", err)
	}
	sealed := aead.Seal(iv, iv, []byte(token), []byte(info))
	return Prefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Open decrypts a channel blob. Wrong nonce or a non-blob fails with ErrInvalid.
func Open(blob string, loginNonce []byte) (string, error) {
	trimmed := strings.TrimSpace(blob)
	if !strings.HasPrefix(trimmed, Prefix) {
		return "", ErrInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(trimmed, Prefix))
	if err != nil || len(raw) < gcmSize+16 {
		return "", ErrInvalid
	}
	if len(loginNonce) != NonceSize {
		return "", ErrNonce
	}
	block, err := aes.NewCipher(deriveKey(loginNonce))
	if err != nil {
		return "", fmt.Errorf("botlogin: cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("botlogin: gcm: %w", err)
	}
	plain, err := aead.Open(nil, raw[:gcmSize], raw[gcmSize:], []byte(info))
	if err != nil {
		return "", ErrInvalid
	}
	token := string(plain)
	if !enroll.LooksLikeToken(token) {
		return "", ErrInvalid
	}
	return token, nil
}

// ExtractBlobs finds s2. payloads in HTML or plain text (t.me/s preview).
func ExtractBlobs(page string) []string {
	const minLen = len(Prefix) + 20
	var out []string
	for i := 0; i < len(page); {
		j := strings.Index(page[i:], Prefix)
		if j < 0 {
			return out
		}
		i += j
		k := i + len(Prefix)
		for k < len(page) {
			c := page[k]
			if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
				k++
				continue
			}
			break
		}
		if k-i >= minLen {
			out = append(out, page[i:k])
		}
		i = k
	}
	return out
}
