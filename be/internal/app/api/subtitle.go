package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
)

// SubtitlePack is one candidate subtitle from a provider (subdl today). Tag is opaque and carries
// no secret -- providers are responsible for stripping their own API keys before returning it here
// (R-SEC-12), same discipline as CatalogStore rows never carrying a delivery token.
type SubtitlePack struct {
	Tag         string
	ReleaseName string
	Releases    []string
	Lang        string
	Translator  string
	HI          bool
}

// SubtitleProvider is what Subtitle needs from a subtitle backend (subdl). Declared here, by the
// consumer, and small (R-GO-5) -- satisfied by *subdl.Client via a thin composition-root wrapper,
// same pattern as tmdb.Client and TitleSearcher.
// PackedSubtitle is one file inside a provider zip. Season/episode come from the entry name.
// SDH and Forced are hearing-impaired and forced-only tracks, not full dialogue.
type PackedSubtitle struct {
	Name, Ext, Text       string
	Season, Episode       int
	HasSeason, HasEpisode bool
	SDH, Forced           bool
}

type SubtitleProvider interface {
	Search(ctx context.Context, tmdbID uint64, isSeries bool, season, episode int, lang string) ([]SubtitlePack, error)
	// FetchPack returns every subtitle in the zip. The archive indexes the siblings; the caller
	// still delivers only the episode that was requested.
	FetchPack(ctx context.Context, tag string) ([]PackedSubtitle, error)
}

// CachedSubtitle is one archived cut of a title. SubType and DurationMin describe that file,
// not the request that happened to download it.
type CachedSubtitle struct {
	ChannelID   int64
	MessageID   int64
	Ext         string
	Provider    string
	SubType     string
	DurationMin int32
}

// SubtitleCacheStore is apid's own record of which titles already have a subtitle sitting in the
// archive channel (doc 15 §7) -- populated on demand, checked before ever calling a provider
// again. Several cuts of one episode can sit side by side; lookup picks the one that matches
// the file being watched.
type SubtitleCacheStore interface {
	ListCachedSubtitles(ctx context.Context, tmdbID uint64, kind int16, seasonNo, episodeNo int32, lang string) ([]CachedSubtitle, error)
	PutCachedSubtitle(ctx context.Context, tmdbID uint64, kind int16, seasonNo, episodeNo int32, lang, provider, ext, subType string, durationMin int32, channelID, messageID int64) error
}

// SubtitleUploader places a newly-fetched subtitle in the archive channel (WorkerBot, sendDocument
// only -- doc 15 §7's worker role, same shape as opsd's backup poster). caption is the HTML
// storage caption (catalog.FormatSubtitleCaption), same text indexerd stamps for the reader act
// pass; empty is allowed only when the caller has nothing to say.
type SubtitleUploader interface {
	Upload(ctx context.Context, fileName, text, caption string) (channelID, messageID int64, err error)
}

// SubtitleDeliverer copies an archived subtitle into the requesting reader's own chat (doc 05-style
// delivery, reused because the wire protocol cannot carry a whole file inline).
type SubtitleDeliverer interface {
	CopyToReader(ctx context.Context, readerChatID, channelID, messageID int64, locator string) (botID, newMessageID int64, err error)
}

// WithSubtitles attaches the subtitle provider, cache and delivery dependencies (doc 15 §7). All
// four must be present together for Subtitle/SubtitleFile to do anything; partially configuring
// them (e.g. a provider with no channel to archive into) is refused at the call site, not here,
// since apid boots the same regardless of which providers happen to be configured.
func (a *API) WithSubtitles(provider SubtitleProvider, cache SubtitleCacheStore, uploader SubtitleUploader, deliverer SubtitleDeliverer) *API {
	a.subtitles = provider
	a.subtitleCache = cache
	a.subtitleUploader = uploader
	a.subtitleDeliverer = deliverer
	return a
}

// subtitleTag is what travels as SubtitlePack.Tag / SubtitleFileReq.Tag on the wire -- opaque to
// the client, self-contained so SubtitleFile needs no second DB read to know what it is resolving
// (R-SEC-12: never a provider key, only ever these plain identifying fields).
type subtitleTag struct {
	Cached      bool   `json:"c"`
	ChannelID   int64  `json:"ch,omitempty"`
	MessageID   int64  `json:"m,omitempty"`
	Ext         string `json:"e,omitempty"`
	Provider    string `json:"p,omitempty"`
	ProviderTag string `json:"pt,omitempty"`
	ReleaseName string `json:"rn,omitempty"`
	TmdbID      uint64 `json:"t,omitempty"`
	Kind        int16  `json:"k,omitempty"`
	SeasonNo    int32  `json:"s,omitempty"`
	EpisodeNo   int32  `json:"ep,omitempty"`
	Lang        string `json:"l,omitempty"`
}

func encodeSubtitleTag(t subtitleTag) (string, error) {
	raw, err := json.Marshal(t)
	if err != nil {
		return "", fmt.Errorf("subtitle tag: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeSubtitleTag(s string) (subtitleTag, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return subtitleTag{}, fmt.Errorf("subtitle tag: %w", err)
	}
	var t subtitleTag
	if err := json.Unmarshal(raw, &t); err != nil {
		return subtitleTag{}, fmt.Errorf("subtitle tag: %w", err)
	}
	return t, nil
}

// subtitleLocator is the caption CopyToReader stamps on its copy, and what the client's document
// resolve verifies against -- same role as Play's plm_<fileId> (doc 05).
func subtitleLocator(tmdbID uint64, kind int16, seasonNo, episodeNo int32, lang string) string {
	return fmt.Sprintf("sub_%d_%d_%d_%d_%s", tmdbID, kind, seasonNo, episodeNo, lang)
}

// Subtitle lists candidate subtitle packs for a title already known to the client (tmdb_id + kind,
// the same pair Item uses). A cache hit answers with a single pack and no provider call at all;
// otherwise it delegates to the configured provider (subdl) exactly as before.
func (a *API) Subtitle(ctx context.Context, p Principal, req *sushiv1.SubtitleReq) (*sushiv1.SubtitleRes, error) {
	kind, err := itemKind(req.GetKind())
	if err != nil {
		return nil, err
	}
	tmdbID := req.GetTmdbId()
	seasonNo, episodeNo := req.GetSeasonNo(), req.GetEpisodeNo()
	lang := req.GetLang()
	k := int16(kind)

	wantType := subtitleCutType(req.GetSourceLabel())
	wantMin := int(req.GetDurationMin())
	if a.subtitleCache != nil && wantType != "" && wantMin > 0 {
		rows, cacheErr := a.subtitleCache.ListCachedSubtitles(ctx, tmdbID, k, seasonNo, episodeNo, lang)
		if cacheErr != nil {
			return nil, fmt.Errorf("subtitle: cache lookup: %w", cacheErr)
		}
		if hit, ok := matchCachedSubtitle(rows, wantType, wantMin); ok {
			// TmdbID/Kind/SeasonNo/EpisodeNo/Lang must ride along even on a cache hit: SubtitleFile
			// derives the CopyToReader locator from this tag, and a locator built from zero values
			// never matches what the client armed its delivery waiter for.
			tag, encErr := encodeSubtitleTag(subtitleTag{
				Cached: true, ChannelID: hit.ChannelID, MessageID: hit.MessageID, Ext: hit.Ext,
				TmdbID: tmdbID, Kind: k, SeasonNo: seasonNo, EpisodeNo: episodeNo, Lang: lang,
			})
			if encErr != nil {
				return nil, encErr
			}
			return &sushiv1.SubtitleRes{Packs: []*sushiv1.SubtitlePack{{
				Tag:         tag,
				ReleaseName: hit.SubType,
				Lang:        lang,
				Translator:  hit.Provider,
			}}}, nil
		}
	}

	if a.subtitles == nil {
		return nil, fmt.Errorf("subtitle: not configured")
	}
	packs, err := a.subtitles.Search(ctx, tmdbID, kind == catalog.KindSeries, int(seasonNo), int(episodeNo), lang)
	if err != nil {
		return nil, fmt.Errorf("subtitle: %w", err)
	}
	out := make([]*sushiv1.SubtitlePack, 0, len(packs))
	for _, pk := range packs {
		tag, encErr := encodeSubtitleTag(subtitleTag{
			Provider: "subdl", ProviderTag: pk.Tag, ReleaseName: pk.ReleaseName,
			TmdbID: tmdbID, Kind: k, SeasonNo: seasonNo, EpisodeNo: episodeNo, Lang: lang,
		})
		if encErr != nil {
			return nil, encErr
		}
		out = append(out, &sushiv1.SubtitlePack{
			Tag:         tag,
			ReleaseName: pk.ReleaseName,
			Releases:    pk.Releases,
			Lang:        pk.Lang,
			Translator:  pk.Translator,
			Hi:          pk.HI,
		})
	}
	return &sushiv1.SubtitleRes{Packs: out}, nil
}

// SubtitleFile resolves one chosen pack's tag to a copy of the file in the reader's own chat (doc
// 15 §7 two-step shape, mirroring Search -> Play). A provider (not yet cached) tag fetches the
// text, archives it (WorkerBot -> SubtitleChannelID), and records it before the same delivery step
// a cache hit takes directly.
func (a *API) SubtitleFile(ctx context.Context, p Principal, req *sushiv1.SubtitleFileReq) (*sushiv1.SubtitleFileRes, error) {
	if a.subtitleDeliverer == nil {
		return nil, fmt.Errorf("subtitle_file: not configured")
	}
	if req.GetTag() == "" {
		return nil, fmt.Errorf("subtitle_file: tag not specified")
	}
	tag, err := decodeSubtitleTag(req.GetTag())
	if err != nil {
		return nil, fmt.Errorf("subtitle_file: %w", err)
	}

	channelID, messageID, ext := tag.ChannelID, tag.MessageID, tag.Ext
	if !tag.Cached {
		var archiveErr error
		channelID, messageID, ext, archiveErr = a.archiveProviderSubtitle(ctx, tag)
		if archiveErr != nil {
			return nil, archiveErr
		}
	}

	locator := subtitleLocator(tag.TmdbID, tag.Kind, tag.SeasonNo, tag.EpisodeNo, tag.Lang)
	botID, newMessageID, deliverErr := a.subtitleDeliverer.CopyToReader(ctx, p.ReaderChatID, channelID, messageID, locator)
	if deliverErr != nil {
		return nil, fmt.Errorf("subtitle_file: deliver: %w", deliverErr)
	}
	return &sushiv1.SubtitleFileRes{BotId: botID, MessageId: newMessageID, Ext: ext}, nil
}

func (a *API) archiveProviderSubtitle(ctx context.Context, tag subtitleTag) (channelID, messageID int64, ext string, err error) {
	if a.subtitles == nil || a.subtitleUploader == nil || a.subtitleCache == nil {
		return 0, 0, "", fmt.Errorf("subtitle_file: provider archiving not configured")
	}
	files, fetchErr := a.subtitles.FetchPack(ctx, tag.ProviderTag)
	if fetchErr != nil {
		return 0, 0, "", fmt.Errorf("subtitle_file: %w", fetchErr)
	}
	// Season/episode pick the ZIP entry (not files[0] — Friends S3E12 season-pack miss).
	picked, pickedIdx, pickErr := pickPackedSubtitle(files, int(tag.SeasonNo), int(tag.EpisodeNo))
	if pickErr != nil {
		return 0, 0, "", fmt.Errorf("subtitle_file: %w", pickErr)
	}
	channelID, messageID, ext, err = a.storePackedSubtitle(ctx, tag, picked, tag.ReleaseName, int(tag.SeasonNo), int(tag.EpisodeNo))
	if err != nil {
		return 0, 0, "", err
	}
	// The viewer is waiting on this episode. Sibling episodes of the same zip are indexed after
	// the response, on a detached context, so a 20-file season pack does not hold the RPC open.
	siblings := siblingPackIndexes(files, pickedIdx, tag.ReleaseName, int(tag.SeasonNo), int(tag.EpisodeNo))
	if len(siblings) > 0 {
		go a.indexSiblingSubs(context.WithoutCancel(ctx), tag, siblings)
	}
	return channelID, messageID, ext, nil
}

func (a *API) storePackedSubtitle(ctx context.Context, tag subtitleTag, file PackedSubtitle, label string, season, episode int) (channelID, messageID int64, ext string, err error) {
	ext = file.Ext
	if ext == "" {
		ext = ".srt"
	}
	stored := tag
	stored.SeasonNo = int32(season)
	stored.EpisodeNo = int32(episode)
	if strings.TrimSpace(label) != "" {
		stored.ReleaseName = label
	}
	cut := packCutType(file.Name, tag.ReleaseName)
	mins := int32(subtitleDurationMin(file.Text))
	// Automatic tries the top packs on every play. Each one used to sendDocument into the
	// archive channel and then upsert the same cut, so the channel grew by two messages per
	// replay while the table stayed at one row. Reuse the message already stored for this cut.
	if existing, ok, lookErr := a.cachedCut(ctx, stored, cut, mins); lookErr != nil {
		return 0, 0, "", fmt.Errorf("subtitle_file: cache: %w", lookErr)
	} else if ok {
		slog.Info("subtitle cache hit", "tmdb", stored.TmdbID, "lang", stored.Lang, "cut", cut, "min", mins, "message", existing.MessageID)
		return existing.ChannelID, existing.MessageID, existing.Ext, nil
	}
	fileName := subtitleLocator(stored.TmdbID, stored.Kind, stored.SeasonNo, stored.EpisodeNo, stored.Lang) + ext
	channelID, messageID, uploadErr := a.subtitleUploader.Upload(ctx, fileName, file.Text, a.providerSubtitleCaption(ctx, stored, fileName))
	if uploadErr != nil {
		return 0, 0, "", fmt.Errorf("subtitle_file: archive: %w", uploadErr)
	}
	if cacheErr := a.subtitleCache.PutCachedSubtitle(ctx, stored.TmdbID, stored.Kind, stored.SeasonNo, stored.EpisodeNo, stored.Lang, stored.Provider, ext, cut, mins, channelID, messageID); cacheErr != nil {
		return 0, 0, "", fmt.Errorf("subtitle_file: cache: %w", cacheErr)
	}
	return channelID, messageID, ext, nil
}

// cachedCut is the archive message already stored for this title, language, cut, and minute
// count. An empty cut or a file with no cue timeline is not reusable: lookup would not return
// it later, and a second upload is the only copy the channel would have.
func (a *API) cachedCut(ctx context.Context, tag subtitleTag, cut string, mins int32) (CachedSubtitle, bool, error) {
	if a.subtitleCache == nil || cut == "" || mins <= 0 {
		return CachedSubtitle{}, false, nil
	}
	rows, err := a.subtitleCache.ListCachedSubtitles(ctx, tag.TmdbID, tag.Kind, tag.SeasonNo, tag.EpisodeNo, tag.Lang)
	if err != nil {
		return CachedSubtitle{}, false, err
	}
	for _, row := range rows {
		if row.SubType == cut && row.DurationMin == mins && row.ChannelID != 0 && row.MessageID != 0 {
			if row.Ext == "" {
				row.Ext = ".srt"
			}
			return row, true, nil
		}
	}
	return CachedSubtitle{}, false, nil
}

// siblingIndexPace stays under Telegram's per-chat flood limit. A season pack used to
// sendDocument every entry back-to-back; 429s came back with retry_after and the next
// file was already in flight, so the rest of the pack failed in the same second.
var siblingIndexPace = 3 * time.Second

func (a *API) indexSiblingSubs(ctx context.Context, tag subtitleTag, siblings []packIndex) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	for i, sib := range siblings {
		if ctx.Err() != nil {
			return
		}
		if i > 0 && sleepCtx(ctx, siblingIndexPace) != nil {
			return
		}
		if !a.indexOneSibling(ctx, tag, sib) {
			return
		}
	}
}

func (a *API) indexOneSibling(ctx context.Context, tag subtitleTag, sib packIndex) bool {
	label := sib.File.Name
	if strings.TrimSpace(label) == "" {
		label = tag.ReleaseName
	}
	if _, _, _, err := a.storePackedSubtitle(ctx, tag, sib.File, label, sib.Season, sib.Episode); err != nil {
		slog.Warn("subtitle: index pack entry", "name", sib.File.Name, "err", err)
		var te *tg.Error
		if errors.As(err, &te) && te.IsRateLimit() {
			return false
		}
	}
	return true
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// providerSubtitleCaption is the subtitle-channel caption for a provider fetch (SubDL). Same
// renderer indexerd writes into storage_caption: title, season/episode, language, release name,
// TMDB link. No source post — the file did not come from a channel.
func (a *API) providerSubtitleCaption(ctx context.Context, tag subtitleTag, fileName string) string {
	title, year := a.subtitleTitleYear(ctx, tag.TmdbID, catalog.Kind(tag.Kind))
	name := strings.TrimSpace(tag.ReleaseName)
	if name == "" {
		name = fileName
	}
	return catalog.FormatSubtitleCaption(catalog.SubtitleCaption{
		Title: title, Year: year, Kind: tag.Kind,
		SeasonNo: tag.SeasonNo, EpisodeNo: tag.EpisodeNo, Lang: tag.Lang,
		FileName: name, TmdbID: int64(tag.TmdbID),
	})
}

func (a *API) subtitleTitleYear(ctx context.Context, tmdbID uint64, kind catalog.Kind) (string, int32) {
	if a.browse != nil {
		item, found, err := a.browse.BrowseItem(ctx, tmdbID, kind)
		if err == nil && found && strings.TrimSpace(item.Title) != "" {
			return item.Title, int32(item.Year)
		}
	}
	if a.hydrate != nil {
		item, err := a.hydrate.TitlePage(ctx, tmdbID, kind)
		if err == nil && strings.TrimSpace(item.Title) != "" {
			return item.Title, int32(item.Year)
		}
	}
	return "", 0
}
