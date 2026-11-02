// Package pairs parses the packed-list secret format used across Sushi's configuration:
//
//	NAME=field|field|…,field|field|…
//
// One format, one parser. Bot pools, backup bots, and channel sets all use it (docs/08 §2.1).
//
// The field separator is '|' because a Telegram bot token already contains a colon
// ("123456:AAF…"). Splitting on ':' cannot tell the token's colon from a field boundary once an
// entry has three fields (username, token, channel id). Usenames, tokens, and channel ids never
// contain '|'.
//
// Other guarantees:
//   - a malformed entry names itself in the error, because the whole objection to a packed list is
//     that it hides which entry is broken;
//   - duplicates and empties fail the boot rather than silently shrinking the list;
//   - error text never includes a field that looks like a token.
package pairs

import (
	"errors"
	"fmt"
	"strings"
)

// FieldSep joins the fields of one entry. Comma joins entries.
const FieldSep = "|"

// Record is one parsed entry. Index 0 is the identity (username or role) used for duplicate checks.
type Record []string

// Key is the identity field, always index 0.
func (r Record) Key() string {
	if len(r) == 0 {
		return ""
	}
	return r[0]
}

// ErrEmpty reports a secret that parsed to nothing.
var ErrEmpty = errors.New("pairs: no entries")

// Parser knows the fields of one entry, so an error says "empty token" rather than "empty value".
type Parser struct {
	// FieldNames is the ordered list of field labels, at least two. Length is the exact arity.
	FieldNames []string
}

// Parse reads a packed list with generic two-field wording.
func Parse(secretName, raw string) ([]Record, error) {
	return Parser{FieldNames: []string{"key", "value"}}.Parse(secretName, raw)
}

// parseEntry splits one "a|b|c" entry into its fields.
//
// num is the entry's 1-based position in the secret, which is all an error is allowed to point at:
// the values themselves are secrets and never appear in a message (R-SEC-1).
func (p Parser) parseEntry(secretName string, num int, entry string) (Record, error) {
	n := len(p.FieldNames)

	entry = strings.TrimSpace(entry)
	if entry == "" {
		return nil, fmt.Errorf("%s: entry %d is empty", secretName, num)
	}
	if !strings.Contains(entry, FieldSep) {
		return nil, fmt.Errorf("%s: entry %d has no %q separator; fields are %s",
			secretName, num, FieldSep, strings.Join(p.FieldNames, FieldSep))
	}

	parts := strings.Split(entry, FieldSep)
	if len(parts) != n {
		return nil, fmt.Errorf("%s: entry %d has %d fields, want %d (%s)",
			secretName, num, len(parts), n, strings.Join(p.FieldNames, FieldSep))
	}

	rec := make(Record, n)
	for f, part := range parts {
		part = strings.TrimSpace(part)
		if f == 0 {
			part = strings.TrimPrefix(part, "@")
		}
		if part == "" {
			label := p.FieldNames[f]
			if f == 0 {
				return nil, fmt.Errorf("%s: entry %d has an empty %s", secretName, num, label)
			}
			return nil, fmt.Errorf("%s: entry %d (%s) has an empty %s", secretName, num, rec[0], label)
		}
		rec[f] = part
	}
	return rec, nil
}

// Parse reads a packed list. secretName appears in every error, so a failure says which secret to
// go and look at.
func (p Parser) Parse(secretName, raw string) ([]Record, error) {
	n := len(p.FieldNames)
	if n < 2 {
		return nil, fmt.Errorf("pairs: parser for %s needs at least 2 field names", secretName)
	}

	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("%s: %w", secretName, ErrEmpty)
	}

	entries := strings.Split(raw, ",")
	out := make([]Record, 0, len(entries))
	seen := make(map[string]int, len(entries))

	for i, entry := range entries {
		rec, err := p.parseEntry(secretName, i+1, entry)
		if err != nil {
			return nil, err
		}

		if first, dup := seen[strings.ToLower(rec.Key())]; dup {
			return nil, fmt.Errorf("%s: %s appears twice, at entries %d and %d", secretName, rec.Key(), first, i+1)
		}
		seen[strings.ToLower(rec.Key())] = i + 1

		out = append(out, rec)
	}

	return out, nil
}
