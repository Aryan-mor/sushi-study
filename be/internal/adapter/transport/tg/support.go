package tg

import (
	"context"
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"
)

// maxArgsBytes bounds a request's arguments. Requests are small by design; anything larger is not
// a client of ours (R-PROTO-4).
const maxArgsBytes = 4096

func decodeArgs(field string) ([]byte, error) {
	args, err := base64.RawURLEncoding.DecodeString(field)
	if err != nil {
		return nil, fmt.Errorf("transport: arguments are not base64url: %w", err)
	}
	if len(args) > maxArgsBytes {
		return nil, fmt.Errorf("transport: arguments are %d bytes, limit %d", len(args), maxArgsBytes)
	}
	return args, nil
}

func unmarshalArgs(args []byte, into proto.Message) error {
	if len(args) == 0 {
		return nil // a bare command, which every read treats as its default request
	}
	if err := proto.Unmarshal(args, into); err != nil {
		return fmt.Errorf("transport: arguments: %w", err)
	}
	return nil
}

// chatLimiter paces sends per chat.
//
// Telegram allows roughly one message per second to one chat. The scarce resource is the bot
// sending, so this is the one place a wait is correct: an answer that arrives a moment late is
// still an answer, and a dropped one is a client waiting forever (docs/11 §1).
type chatLimiter struct {
	mu   sync.Mutex
	next map[int64]time.Time
}

const perChatInterval = time.Second

func newChatLimiter() *chatLimiter {
	return &chatLimiter{next: make(map[int64]time.Time)}
}

func (l *chatLimiter) wait(ctx context.Context, chatID int64) {
	l.mu.Lock()
	now := time.Now()
	earliest := l.next[chatID]
	if earliest.Before(now) {
		earliest = now
	}
	l.next[chatID] = earliest.Add(perChatInterval)
	l.mu.Unlock()

	delay := time.Until(earliest)
	if delay <= 0 {
		return
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}

// penalise honours a 429's retry_after exactly, rather than guessing at a backoff.
func (l *chatLimiter) penalise(chatID int64, retryAfter time.Duration) {
	if retryAfter <= 0 {
		retryAfter = 5 * time.Second
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.next[chatID] = time.Now().Add(retryAfter)
}

// replyCache makes a repeated request idempotent.
//
// Telegram can redeliver an update, and a client that times out retries with the same correlation
// id on purpose (docs/02 §6). Both must produce the same answer without recomputing it.
type replyCache struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[replyKey]cachedReply
}

type replyKey struct {
	chatID int64
	corr   uint64
}

type cachedReply struct {
	text    string
	expires time.Time
}

func newReplyCache(ttl time.Duration) *replyCache {
	return &replyCache{ttl: ttl, m: make(map[replyKey]cachedReply)}
}

func (c *replyCache) get(chatID int64, corr uint64) (string, bool) {
	if corr == 0 {
		return "", false // pushes are not answers to anything
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.m[replyKey{chatID, corr}]
	if !ok || time.Now().After(entry.expires) {
		return "", false
	}
	return entry.text, true
}

func (c *replyCache) put(chatID int64, corr uint64, text string) {
	if corr == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	c.m[replyKey{chatID, corr}] = cachedReply{text: text, expires: now.Add(c.ttl)}

	// Swept opportunistically: the map only grows while a chat is active, and a service that needs
	// a janitor goroutine for sixty seconds of state does not need one.
	if len(c.m) > 4096 {
		for k, v := range c.m {
			if now.After(v.expires) {
				delete(c.m, k)
			}
		}
	}
}
