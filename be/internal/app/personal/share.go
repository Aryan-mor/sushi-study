package personal

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// Share links (ADR 0038). One link per video: whoever holds access and shares it gets the same one,
// as if forwarding the first message. The token is random and never an id, so a link cannot be
// guessed from another one.

// SharePrefix is the /start payload prefix of a share link.
const SharePrefix = "pv_"

// tokenBytes is the link's entropy: 128 bits, 22 base64url characters.
const tokenBytes = 16

// ErrNoAccess means the person asking to share does not hold a ready grant on the video.
var ErrNoAccess = errors.New("personal: no access to this video")

// ShareStore is the library slice sharing needs (R-GO-5).
type ShareStore interface {
	// Grant is this person's grant; while state is GrantAwaitingTitle, title is the suggestion.
	Grant(ctx context.Context, videoID, tgUserID int64) (state int16, title string, found bool, err error)
	// EnsureShare stores token as the video's link unless it already has one.
	EnsureShare(ctx context.Context, videoID int64, token, title string, createdBy int64) error
	// ShareByVideo is the video's link and the title it carries.
	ShareByVideo(ctx context.Context, videoID int64) (token, title string, found bool, err error)
	// ShareByToken resolves a link.
	ShareByToken(ctx context.Context, token string) (videoID int64, title string, createdBy int64, found bool, err error)
	// InsertSharedGrant gives tgUserID a ready grant; inserted is false when they already had one.
	InsertSharedGrant(ctx context.Context, videoID, tgUserID int64, title string, grantedBy int64) (inserted bool, err error)
}

// ClaimOutcome is what opening a link did.
type ClaimOutcome int

const (
	// ClaimAdded: the person now has the video.
	ClaimAdded ClaimOutcome = iota + 1
	// ClaimAlreadyHad: they had it before; nothing changed.
	ClaimAlreadyHad
	// ClaimInvalid: no video has this link.
	ClaimInvalid
)

// Sharing makes and resolves share links.
type Sharing struct {
	store ShareStore
	// newToken is replaced in tests.
	newToken func() (string, error)
}

// NewSharing builds link handling over the library.
func NewSharing(store ShareStore) *Sharing {
	return &Sharing{store: store, newToken: randomToken}
}

// Link is the video's share link payload (without the bot URL) and the title the link carries.
// The first share makes the link with this person's title; every later share reuses it.
func (s *Sharing) Link(ctx context.Context, tgUserID, videoID int64) (payload, title string, err error) {
	state, myTitle, found, err := s.store.Grant(ctx, videoID, tgUserID)
	if err != nil {
		return "", "", fmt.Errorf("look up grant: %w", err)
	}
	if !found || state != GrantReady {
		return "", "", ErrNoAccess
	}
	token, title, found, err := s.store.ShareByVideo(ctx, videoID)
	if err != nil {
		return "", "", fmt.Errorf("look up link: %w", err)
	}
	if !found {
		fresh, tokErr := s.newToken()
		if tokErr != nil {
			return "", "", fmt.Errorf("new token: %w", tokErr)
		}
		if storeErr := s.store.EnsureShare(ctx, videoID, fresh, myTitle, tgUserID); storeErr != nil {
			return "", "", fmt.Errorf("store link: %w", storeErr)
		}
		// Read back: two people sharing at once both "make" one, and the first insert wins.
		token, title, found, err = s.store.ShareByVideo(ctx, videoID)
		if err != nil || !found {
			return "", "", fmt.Errorf("read link back: found=%v: %w", found, err)
		}
	}
	return SharePrefix + token, title, nil
}

// Claim opens a link for tgUserID. The grant carries the link's title, which they cannot change.
func (s *Sharing) Claim(ctx context.Context, tgUserID int64, payload string) (ClaimOutcome, string, error) {
	token, ok := strings.CutPrefix(payload, SharePrefix)
	if !ok || !validToken(token) {
		return ClaimInvalid, "", nil
	}
	videoID, title, createdBy, found, err := s.store.ShareByToken(ctx, token)
	if err != nil {
		return 0, "", fmt.Errorf("resolve link: %w", err)
	}
	if !found {
		return ClaimInvalid, "", nil
	}
	inserted, err := s.store.InsertSharedGrant(ctx, videoID, tgUserID, title, createdBy)
	if err != nil {
		return 0, "", fmt.Errorf("grant: %w", err)
	}
	if !inserted {
		return ClaimAlreadyHad, title, nil
	}
	return ClaimAdded, title, nil
}

// IsSharePayload reports whether a /start payload is a share link.
func IsSharePayload(payload string) bool { return strings.HasPrefix(payload, SharePrefix) }

func randomToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// validToken matches the library's CHECK, so junk never reaches a query.
func validToken(t string) bool {
	if len(t) < 16 || len(t) > 48 {
		return false
	}
	for _, r := range t {
		ok := r == '-' || r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !ok {
			return false
		}
	}
	return true
}
