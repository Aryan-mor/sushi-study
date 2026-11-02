// Package personal is main-bot's half of personal videos (ADR 0037): a person sends a video, it is
// stored once in personal_clone, and the person names it. Who can see it is the grant table;
// nothing here reads anything else to decide access.
//
// It knows nothing about chat screens or locales. main-bot asks it what happened and words the
// answer itself.
package personal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxTitleRunes bounds a title so it fits a card and a Telegram button.
const MaxTitleRunes = 120

// Grant states, as stored in personal_video_grants.state.
const (
	GrantAwaitingTitle int16 = 1
	GrantReady         int16 = 2
)

// ErrBadTitle means the title is empty or too long after trimming.
var ErrBadTitle = errors.New("personal: title must be 1-120 characters")

// ErrNoPendingTitle means there is nothing waiting for this person's title.
var ErrNoPendingTitle = errors.New("personal: no video is waiting for a title")

// Video is what Telegram told us about the file a person sent.
type Video struct {
	UniqueID  string
	FileName  string
	MimeType  string
	DurationS int
	SizeBytes int64
}

// Upload is one video message in a person's chat with main-bot.
type Upload struct {
	TGUserID  int64
	ChatID    int64
	MessageID int64
	Caption   string
	Video     Video
}

// Store is the library slice intake needs (R-GO-5).
type Store interface {
	VideoByUID(ctx context.Context, uniqueID string) (videoID int64, found bool, err error)
	// InsertVideo returns inserted=false when someone stored the same file first; videoID is then 0.
	InsertVideo(ctx context.Context, uploadedBy int64, v Video) (videoID int64, inserted bool, err error)
	SetCloneLocation(ctx context.Context, videoID, channelID, messageID int64) error
	// Grant is this person's grant; while state is GrantAwaitingTitle, title is the suggestion.
	Grant(ctx context.Context, videoID, tgUserID int64) (state int16, title string, found bool, err error)
	UpsertPendingGrant(ctx context.Context, videoID, tgUserID int64, suggestion string) error
	ConfirmTitle(ctx context.Context, videoID, tgUserID int64, title string) (updated bool, err error)
	LatestPending(ctx context.Context, tgUserID int64) (videoID int64, suggestion string, found bool, err error)
	UploadsSince(ctx context.Context, tgUserID int64, since time.Time) (int, error)
}

// Copier is the Telegram calls intake needs: copy the upload into personal_clone, and remove a copy
// that lost a race.
type Copier interface {
	CopyMessage(ctx context.Context, destChat, sourceChat, messageID int64, caption string) (int64, error)
	DeleteMessage(ctx context.Context, chatID, messageID int64) error
}

// Outcome is what Receive did.
type Outcome int

const (
	// AlreadyYours: this person already has a ready grant on this file. Nothing changed.
	AlreadyYours Outcome = iota + 1
	// NeedsTitle: a pending grant exists; ask this person for their title.
	NeedsTitle
	// OverQuota: this would be a new file and the person is at their daily cap.
	OverQuota
)

// Result is Receive's answer.
type Result struct {
	Outcome Outcome
	VideoID int64
	// Suggestion is the caption or file name to offer as a one-tap title. Empty means none.
	Suggestion string
}

// Service is personal-video intake.
type Service struct {
	store          Store
	copier         Copier
	cloneChannelID int64
	uploadsPerDay  int
	now            func() time.Time
}

// New builds intake over the library and main-bot's client. cloneChannelID is personal_clone.
func New(store Store, copier Copier, cloneChannelID int64, uploadsPerDay int) *Service {
	return &Service{
		store: store, copier: copier, cloneChannelID: cloneChannelID,
		uploadsPerDay: uploadsPerDay, now: time.Now,
	}
}

// Receive takes one video a person sent (ADR 0037 §3). A file already stored is never copied
// again: Telegram gives it the same unique id whoever sends it, so a second sender only gets a
// grant. A file this person already has is a no-op.
func (s *Service) Receive(ctx context.Context, up Upload) (Result, error) {
	suggestion := Suggest(up.Caption, up.Video.FileName)

	videoID, found, err := s.store.VideoByUID(ctx, up.Video.UniqueID)
	if err != nil {
		return Result{}, fmt.Errorf("look up file: %w", err)
	}
	if found {
		state, _, has, grantErr := s.store.Grant(ctx, videoID, up.TGUserID)
		if grantErr != nil {
			return Result{}, fmt.Errorf("look up grant: %w", grantErr)
		}
		if has && state == GrantReady {
			return Result{Outcome: AlreadyYours, VideoID: videoID}, nil
		}
		return s.askTitle(ctx, videoID, up.TGUserID, suggestion)
	}

	n, err := s.store.UploadsSince(ctx, up.TGUserID, s.now().Add(-24*time.Hour))
	if err != nil {
		return Result{}, fmt.Errorf("count uploads: %w", err)
	}
	if n >= s.uploadsPerDay {
		return Result{Outcome: OverQuota}, nil
	}

	videoID, err = s.keep(ctx, up)
	if err != nil {
		return Result{}, err
	}
	return s.askTitle(ctx, videoID, up.TGUserID, suggestion)
}

// keep copies the upload into personal_clone and records it. Two people sending the same new file
// at once both copy; UNIQUE (tg_unique_id) picks one row, and the loser drops its own copy.
func (s *Service) keep(ctx context.Context, up Upload) (int64, error) {
	copyID, err := s.copier.CopyMessage(ctx, s.cloneChannelID, up.ChatID, up.MessageID, storageCaption(up.TGUserID))
	if err != nil {
		return 0, fmt.Errorf("copy into personal_clone: %w", err)
	}
	videoID, inserted, err := s.store.InsertVideo(ctx, up.TGUserID, up.Video)
	if err != nil {
		return 0, fmt.Errorf("record video (orphan message %d in %d): %w", copyID, s.cloneChannelID, err)
	}
	if !inserted {
		//nolint:errcheck // best effort: a copy that lost the race and stays behind costs only space
		s.copier.DeleteMessage(ctx, s.cloneChannelID, copyID)
		id, found, err := s.store.VideoByUID(ctx, up.Video.UniqueID)
		if err != nil || !found {
			return 0, fmt.Errorf("read the row that won the insert: found=%v: %w", found, err)
		}
		return id, nil
	}
	if err := s.store.SetCloneLocation(ctx, videoID, s.cloneChannelID, copyID); err != nil {
		return 0, fmt.Errorf("record clone location for video %d: %w", videoID, err)
	}
	return videoID, nil
}

func (s *Service) askTitle(ctx context.Context, videoID, tgUserID int64, suggestion string) (Result, error) {
	if err := s.store.UpsertPendingGrant(ctx, videoID, tgUserID, suggestion); err != nil {
		return Result{}, fmt.Errorf("pending grant: %w", err)
	}
	return Result{Outcome: NeedsTitle, VideoID: videoID, Suggestion: suggestion}, nil
}

// Name sets this person's title on a video waiting for one and makes the grant ready. videoID
// zero means "the newest one waiting" -- the reply main-bot gets after a restart forgot which
// prompt it sent.
func (s *Service) Name(ctx context.Context, tgUserID, videoID int64, title string) (int64, error) {
	title, ok := CleanTitle(title)
	if !ok {
		return 0, ErrBadTitle
	}
	if videoID == 0 {
		id, _, found, err := s.store.LatestPending(ctx, tgUserID)
		if err != nil {
			return 0, fmt.Errorf("latest pending: %w", err)
		}
		if !found {
			return 0, ErrNoPendingTitle
		}
		videoID = id
	}
	updated, err := s.store.ConfirmTitle(ctx, videoID, tgUserID, title)
	if err != nil {
		return 0, fmt.Errorf("confirm title: %w", err)
	}
	if !updated {
		return 0, ErrNoPendingTitle
	}
	return videoID, nil
}

// UseSuggestion names a waiting video with the suggestion stored on its grant: the one-tap button.
// It reads the suggestion from the database, so the button still works after a restart.
func (s *Service) UseSuggestion(ctx context.Context, tgUserID, videoID int64) (string, error) {
	state, suggestion, found, err := s.store.Grant(ctx, videoID, tgUserID)
	if err != nil {
		return "", fmt.Errorf("look up grant: %w", err)
	}
	if !found || state != GrantAwaitingTitle || suggestion == "" {
		return "", ErrNoPendingTitle
	}
	if _, err := s.Name(ctx, tgUserID, videoID, suggestion); err != nil {
		return "", err
	}
	return suggestion, nil
}

// CleanTitle trims a title and checks its length. Newlines collapse to spaces: a title is one line.
func CleanTitle(raw string) (string, bool) {
	t := strings.Join(strings.Fields(raw), " ")
	n := utf8.RuneCountInString(t)
	return t, n > 0 && n <= MaxTitleRunes
}

// Suggest is the one-tap title: the caption's first line, else the file name without its
// extension. Empty when neither gives a usable title.
func Suggest(caption, fileName string) string {
	if line, _, _ := strings.Cut(strings.TrimSpace(caption), "\n"); line != "" {
		if t, ok := CleanTitle(line); ok {
			return t
		}
	}
	name := strings.TrimSpace(fileName)
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		name = name[:i] // ".mp4" has no stem, and suggests nothing
	}
	name = strings.NewReplacer("_", " ", ".", " ").Replace(name)
	if t, ok := CleanTitle(name); ok {
		return t
	}
	return ""
}

// storageCaption is what the personal_clone copy carries instead of the person's own caption,
// which may be private: enough for an operator to find whose upload a message is, nothing more.
func storageCaption(tgUserID int64) string {
	return fmt.Sprintf("👤 personal · tg <code>%d</code>", tgUserID)
}
