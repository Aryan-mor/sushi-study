package api

import (
	"context"
	"fmt"

	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
)

// PersonalVideo is one card in My videos (ADR 0037).
type PersonalVideo struct {
	ID        int64
	Title     string
	DurationS int32
	SizeBytes int64
	PositionS int32
	Done      bool
}

// PersonalStore is the two access questions plus progress for personal videos (ADR 0037).
// Declared here, by the consumer, and small (R-GO-5). Sharing later changes what the two access
// questions answer and nothing else.
type PersonalStore interface {
	// CanViewPersonal reports whether userID holds a ready grant on videoID.
	CanViewPersonal(ctx context.Context, userID, videoID int64) (bool, error)
	// ListPersonalVideos is userID's My videos, newest first.
	ListPersonalVideos(ctx context.Context, userID int64, limit, offset int32) ([]PersonalVideo, error)
	// SetPersonalProgress is resume for a personal video; never a play_event.
	SetPersonalProgress(ctx context.Context, userID, videoID int64, positionS, durationS int32) error
	// SetPersonalDone is a manual mark-as-watched for a personal video.
	SetPersonalDone(ctx context.Context, userID, videoID int64, done bool) error
}

// PersonalActions are the two things a person can do to a video they hold (ADR 0038).
type PersonalActions interface {
	// DeletePersonalGrant ends userID's access to videoID. The video and every other grant stay.
	DeletePersonalGrant(ctx context.Context, userID, videoID int64) error
	// EnqueuePersonalShare asks main-bot to send userID the video's share link.
	EnqueuePersonalShare(ctx context.Context, userID, videoID int64) error
}

// WithPersonalActions attaches share and delete (ADR 0038).
func (a *API) WithPersonalActions(s PersonalActions) *API {
	a.personalActions = s
	return a
}

// Personal answers the "personal" command: share or delete one personal video. Both need a grant;
// without one the answer is ErrNotFound, as for any personal id the caller does not hold.
func (a *API) Personal(ctx context.Context, p Principal, req *sushiv1.PersonalReq) (*sushiv1.PersonalRes, error) {
	if a.personal == nil || a.personalActions == nil {
		return nil, ErrNotFound
	}
	id := req.GetPersonalId()
	ok, err := a.personal.CanViewPersonal(ctx, p.UserID, id)
	if err != nil {
		return nil, fmt.Errorf("personal: grant: %w", err)
	}
	if !ok {
		return nil, ErrNotFound
	}
	switch req.GetAction() {
	case sushiv1.PersonalAction_PERSONAL_ACTION_DELETE:
		err = a.personalActions.DeletePersonalGrant(ctx, p.UserID, id)
	case sushiv1.PersonalAction_PERSONAL_ACTION_SHARE:
		err = a.personalActions.EnqueuePersonalShare(ctx, p.UserID, id)
	default:
		return nil, fmt.Errorf("personal: unknown action %d", req.GetAction())
	}
	if err != nil {
		return nil, fmt.Errorf("personal %s: %w", req.GetAction(), err)
	}
	return &sushiv1.PersonalRes{}, nil
}

// WithPersonal attaches personal videos (ADR 0037). Absent, a personal Play is not found and My
// videos is empty -- never somebody else's.
func (a *API) WithPersonal(s PersonalStore) *API {
	a.personal = s
	return a
}

// listMyVideos answers LIST_SCOPE_MY_VIDEOS.
func (a *API) listMyVideos(ctx context.Context, p Principal, limit, offset int32) (*sushiv1.ListRes, error) {
	if a.personal == nil {
		return &sushiv1.ListRes{}, nil
	}
	videos, err := a.personal.ListPersonalVideos(ctx, p.UserID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("my videos: %w", err)
	}
	out := make([]*sushiv1.PersonalVideo, len(videos))
	for i, v := range videos {
		out[i] = &sushiv1.PersonalVideo{
			PersonalId: v.ID,
			Title:      v.Title,
			DurationS:  v.DurationS,
			SizeBytes:  v.SizeBytes,
			PositionS:  v.PositionS,
			Done:       v.Done,
		}
	}
	var next uint32
	if len(videos) >= int(limit) {
		next = uint32(offset) + uint32(len(videos))
	}
	return &sushiv1.ListRes{PersonalVideos: out, Cursor: next}, nil
}

// applyPersonalEvent takes the progress events that name a personal video. handled is false for
// every other event, which then goes to the catalog path.
func (a *API) applyPersonalEvent(ctx context.Context, p Principal, ev *sushiv1.Event) (handled bool, err error) {
	switch body := ev.GetBody().(type) {
	case *sushiv1.Event_Prog:
		id := body.Prog.GetPersonalId()
		if id == 0 {
			return false, nil
		}
		return true, a.applyPersonalProgress(ctx, p, id, func() error {
			return a.personal.SetPersonalProgress(ctx, p.UserID, id, body.Prog.GetPositionS(), body.Prog.GetDurationS())
		})
	case *sushiv1.Event_Watched:
		id := body.Watched.GetPersonalId()
		if id == 0 {
			return false, nil
		}
		return true, a.applyPersonalProgress(ctx, p, id, func() error {
			return a.personal.SetPersonalDone(ctx, p.UserID, id, body.Watched.GetDone())
		})
	default:
		return false, nil
	}
}

// applyPersonalProgress handles a ProgEvent / WatchedEvent that names a personal video. Progress is
// fire-and-forget like every event; without a grant it is dropped, not an error, so a revoked
// video's last few progress reports do not fail the rest of the batch.
func (a *API) applyPersonalProgress(ctx context.Context, p Principal, videoID int64, apply func() error) error {
	if a.personal == nil {
		return nil
	}
	ok, err := a.personal.CanViewPersonal(ctx, p.UserID, videoID)
	if err != nil {
		return fmt.Errorf("personal progress grant: %w", err)
	}
	if !ok {
		return nil
	}
	return apply()
}
