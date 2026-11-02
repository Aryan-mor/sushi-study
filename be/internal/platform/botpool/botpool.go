// Package botpool parses the packed bot-pool secrets described in docs/08-secrets-and-infisical.md
// §2.1: one secret holding "username|token,username|token,…".
//
// The list format itself lives in platform/pairs; what is here is what makes an entry a *bot*.
package botpool

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Aryan-mor/sushi-study/be/internal/platform/pairs"
)

// Bot is one entry of a pool.
type Bot struct {
	// Username is the handle Telegram reports for the token, without the leading '@'.
	//
	// It is authoritative only when it came from getMe (R-SEC-17). What Parse reads is whatever
	// the secret says, which is why every service verifies at boot: dev has already produced a
	// pool whose configured names and real handles disagreed, and a token that was simply dead.
	Username string
	Token    string
}

// BoundBot is a bot tied to one channel: a backup bot that copies into its own channel.
type BoundBot struct {
	Username  string
	Token     string
	ChannelID int64
}

// Pool is an ordered, de-duplicated set of bots. Order is the order of the secret, which makes
// round-robin assignment reproducible across restarts.
type Pool struct {
	name string
	bots []Bot
}

// BoundPool is an ordered set of bots each bound to a destination channel.
type BoundPool struct {
	name string
	bots []BoundBot
}

// ErrEmpty reports a pool secret that parsed to nothing.
var ErrEmpty = pairs.ErrEmpty

func lookLikeToken(token, secretName string, index int, username string) error {
	// Every Telegram token is "<botID>:<secret>". A token without a colon means the entry was
	// truncated, and it would surface much later as a 401 at runtime.
	if !strings.Contains(token, ":") {
		return fmt.Errorf("%s: entry %d (%s) does not look like a bot token", secretName, index, username)
	}
	return nil
}

// Parse reads a packed pool secret: username|token,…
func Parse(secretName, raw string) (*Pool, error) {
	entries, err := pairs.Parser{FieldNames: []string{"username", "token"}}.Parse(secretName, raw)
	if err != nil {
		return nil, err
	}

	pool := &Pool{name: secretName, bots: make([]Bot, 0, len(entries))}
	for i, e := range entries {
		if err := lookLikeToken(e[1], secretName, i+1, e.Key()); err != nil {
			return nil, err
		}
		pool.bots = append(pool.bots, Bot{Username: e.Key(), Token: e[1]})
	}
	return pool, nil
}

// ParseBound reads username|token|channelId,… — backup bots that copy into one channel each.
func ParseBound(secretName, raw string) (*BoundPool, error) {
	entries, err := pairs.Parser{FieldNames: []string{"username", "token", "channel id"}}.Parse(secretName, raw)
	if err != nil {
		return nil, err
	}

	pool := &BoundPool{name: secretName, bots: make([]BoundBot, 0, len(entries))}
	for i, e := range entries {
		if err := lookLikeToken(e[1], secretName, i+1, e.Key()); err != nil {
			return nil, err
		}
		id, err := strconv.ParseInt(e[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s: entry %d (%s) is not a channel id", secretName, i+1, e.Key())
		}
		if id >= 0 {
			return nil, fmt.Errorf("%s: entry %d (%s) has id %d; channel ids are negative", secretName, i+1, e.Key(), id)
		}
		pool.bots = append(pool.bots, BoundBot{Username: e.Key(), Token: e[1], ChannelID: id})
	}
	return pool, nil
}

// Bots returns the pool's entries in secret order.
func (p *Pool) Bots() []Bot {
	out := make([]Bot, len(p.bots))
	copy(out, p.bots)
	return out
}

// Len reports how many bots the pool holds.
func (p *Pool) Len() int { return len(p.bots) }

// Name returns the secret key this pool was parsed from.
func (p *Pool) Name() string { return p.name }

// Usernames lists the pool's handles. Safe to log — tokens never are.
func (p *Pool) Usernames() []string {
	out := make([]string, len(p.bots))
	for i, b := range p.bots {
		out[i] = b.Username
	}
	return out
}

// ByUsername finds a bot by handle, case-insensitively.
func (p *Pool) ByUsername(username string) (Bot, bool) {
	username = strings.ToLower(strings.TrimPrefix(username, "@"))
	for _, b := range p.bots {
		if strings.ToLower(b.Username) == username {
			return b, true
		}
	}
	return Bot{}, false
}

// String renders the pool for logs: names only, never tokens.
func (p *Pool) String() string {
	return fmt.Sprintf("%s[%d]: %s", p.name, len(p.bots), strings.Join(p.Usernames(), " "))
}

// Bots returns bound entries in secret order.
func (p *BoundPool) Bots() []BoundBot {
	out := make([]BoundBot, len(p.bots))
	copy(out, p.bots)
	return out
}

// Len reports how many bound bots the pool holds.
func (p *BoundPool) Len() int { return len(p.bots) }

// Usernames lists handles. Safe to log.
func (p *BoundPool) Usernames() []string {
	out := make([]string, len(p.bots))
	for i, b := range p.bots {
		out[i] = b.Username
	}
	return out
}

// String renders names and channel ids, never tokens.
func (p *BoundPool) String() string {
	parts := make([]string, len(p.bots))
	for i, b := range p.bots {
		parts[i] = fmt.Sprintf("%s:%d", b.Username, b.ChannelID)
	}
	return fmt.Sprintf("%s[%d]: %s", p.name, len(p.bots), strings.Join(parts, " "))
}
