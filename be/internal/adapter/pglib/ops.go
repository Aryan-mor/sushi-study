package pglib

// Ops is main-bot's narrow library-side store: derived news text and artwork, never catalog rows.
type Ops struct {
	q *Queries
}

// NewOps wraps a library pool (or a test fake that satisfies DBTX).
func NewOps(db DBTX) *Ops {
	return &Ops{q: New(db)}
}
