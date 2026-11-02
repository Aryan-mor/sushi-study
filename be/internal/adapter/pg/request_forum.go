package pg

import (
	"context"
	"fmt"
)

// RequestForumTopics returns the stored open / reject / done thread ids (ADR 0036).
// A missing name is absent from the map.
func (s *Store) RequestForumTopics(ctx context.Context) (map[string]int64, error) {
	rows, err := s.q.ListRequestForumTopics(ctx)
	if err != nil {
		return nil, fmt.Errorf("list request forum topics: %w", err)
	}
	out := make(map[string]int64, len(rows))
	for _, row := range rows {
		out[row.Name] = row.ThreadID
	}
	return out, nil
}

// SaveRequestForumTopic records one topic thread id so the next boot does not create it again.
func (s *Store) SaveRequestForumTopic(ctx context.Context, name string, threadID int64) error {
	err := s.q.UpsertRequestForumTopic(ctx, UpsertRequestForumTopicParams{Name: name, ThreadID: threadID})
	if err != nil {
		return fmt.Errorf("save request forum topic %s: %w", name, err)
	}
	return nil
}
