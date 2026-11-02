// Package tg is the Telegram side of the protocol: it turns messages into API calls and answers
// back into messages.
//
// Everything Telegram-shaped lives here — chat ids, correlation, envelopes, rate limits,
// idempotency — so that app/api stays a set of typed operations that a second transport could
// serve unchanged (R-API-1, R-API-2).
package tg

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/api"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
	"github.com/Aryan-mor/sushi-study/be/internal/wire"
)

// Request is one parsed client message: "/home 7" or "/item 8 QQ" (docs/02 §3).
//
// Requests stay human-readable on purpose. It costs a few bytes and buys the ability to debug the
// whole system by scrolling a Telegram chat.
type Request struct {
	Command string
	Corr    uint64
	Args    []byte
}

// ErrNotARequest means the text was not addressed to the protocol: a human typing, a service
// message, another bot's prose. Ignored rather than answered.
var ErrNotARequest = errors.New("transport: not a protocol request")

// ParseRequest reads the client's line.
func ParseRequest(text string) (Request, error) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return Request{}, ErrNotARequest
	}

	fields := strings.Fields(text)
	command := strings.TrimPrefix(fields[0], "/")
	// Telegram appends "@botname" when a command is used in a group; harmless to strip everywhere.
	if at := strings.IndexByte(command, '@'); at > 0 {
		command = command[:at]
	}
	if command == "" {
		return Request{}, ErrNotARequest
	}

	req := Request{Command: strings.ToLower(command)}
	if len(fields) > 1 {
		corr, err := strconv.ParseUint(fields[1], 36, 64)
		if err != nil {
			return Request{}, fmt.Errorf("transport: correlation id %q: %w", fields[1], err)
		}
		req.Corr = corr
	}
	if len(fields) > 2 {
		args, err := decodeArgs(fields[2])
		if err != nil {
			return Request{}, err
		}
		req.Args = args
	}
	return req, nil
}

// Dispatcher maps one parsed request onto an answer. It is the seam that lets more than one bot
// share this package's reliability plumbing (idempotency, per-chat pacing, envelope replies)
// without sharing an operation set: apid's Dispatcher is the app/api operations below; init-bot's
// is just /initbot, resolving or minting a binding rather than reading the catalog (ADR 0004).
type Dispatcher interface {
	Dispatch(ctx context.Context, p api.Principal, req Request) (sushiv1.MsgType, proto.Message, error)
}

// Handler serves parsed requests. One method per operation would grow into the transport; instead
// requests are handed to a Dispatcher, which owns the actual command switch.
type Handler struct {
	dispatcher Dispatcher
	log        *slog.Logger
	resolver   PrincipalResolver
	sender     Sender
	limiter    *chatLimiter
	cache      *replyCache
	startText  string
	// access is nil until WithAccess; nil means nobody is refused here (ADR 0039).
	access accessChecker
}

// Sender is the outbound half, kept as an interface so the handler is testable without Telegram
// (R-API-3).
type Sender interface {
	SendMessage(ctx context.Context, chatID int64, text string) (tg.Message, error)
}

// accessChecker is the testing-mode gate, from identity.Service (ADR 0039).
type accessChecker interface {
	Authorized(ctx context.Context, tgUserID int64) (bool, error)
}

// PrincipalResolver turns a chat into whoever is asking. Today that is the bindings table; in dev
// it can be the chat id itself (config.AllowUnbound).
type PrincipalResolver interface {
	Resolve(ctx context.Context, chatID int64) (api.Principal, error)
}

// identityResolver is the identity.Service methods BoundResolver needs -- declared here, by the
// consumer, rather than importing the concrete *identity.Service type (R-GO-5).
type identityResolver interface {
	Resolve(ctx context.Context, chatID int64) (identity.Binding, error)
}

// BoundResolver is the real PrincipalResolver: a chat is whoever /initbot bound it to. Shared by
// every bot that needs one -- apid and init-bot alike -- so "resolve a chat" has exactly one
// implementation.
type BoundResolver struct{ Identity identityResolver }

func (r BoundResolver) Resolve(ctx context.Context, chatID int64) (api.Principal, error) {
	binding, err := r.Identity.Resolve(ctx, chatID)
	if err != nil {
		return api.Principal{}, err
	}
	return api.Principal{
		UserID:       binding.UserID,
		ProviderID:   binding.ProviderID,
		ReaderChatID: binding.ReaderChatID,
		Role:         binding.Role,
		BetaTester:   binding.BetaTester,
	}, nil
}

// NewHandler wires a handler over a Dispatcher. mainBot is the only handle these bots ever mention,
// and it is where a person who wandered in is sent.
func NewHandler(dispatcher Dispatcher, sender Sender, resolver PrincipalResolver, mainBot string, log *slog.Logger) *Handler {
	start := "This bot is part of Sushi and only speaks to the app 🍣"
	if mainBot != "" {
		start += "\n\nOpen @" + mainBot + " to get started."
	}

	return &Handler{
		dispatcher: dispatcher,
		log:        log,
		resolver:   resolver,
		sender:     sender,
		limiter:    newChatLimiter(),
		cache:      newReplyCache(60 * time.Second),
		startText:  start,
	}
}

// WithAccess makes the handler tell a person the gate refuses that the app is not public, instead
// of sending them to main-bot for the same answer (ADR 0039).
func (h *Handler) WithAccess(access accessChecker) *Handler {
	h.access = access
	return h
}

// refused reports a person the gate turns away. A failed check is not a refusal here: main-bot and
// the binding lookup still stand between them and anything, so this only picks the wording.
func (h *Handler) refused(ctx context.Context, from *tg.User, log *slog.Logger) bool {
	if h.access == nil || from == nil {
		return false
	}
	allowed, err := h.access.Authorized(ctx, from.ID)
	if err != nil {
		log.Error("registration check failed", "err", err)
		return false
	}
	return !allowed
}

func (h *Handler) startAnswer(ctx context.Context, from *tg.User, log *slog.Logger) string {
	if h.refused(ctx, from, log) {
		return identity.NotPublicText(from.Language, from.ID)
	}
	return h.startText
}

func (h *Handler) unboundCode(ctx context.Context, from *tg.User, log *slog.Logger) sushiv1.ErrCode {
	if h.refused(ctx, from, log) {
		return sushiv1.ErrCode_ERR_CODE_NOT_PUBLIC
	}
	return sushiv1.ErrCode_ERR_CODE_AUTH
}

// Handle serves one update. It never returns an error to the caller: a poll loop that stops on a
// single bad message is a service one crafted message can silence.
func (h *Handler) Handle(ctx context.Context, msg *tg.Message) {
	req, err := ParseRequest(msg.Text)
	switch {
	case errors.Is(err, ErrNotARequest):
		return
	case err != nil:
		h.log.Debug("unparseable request", "chat", msg.Chat.ID, "err", err)
		return
	}

	log := h.log.With("chat", msg.Chat.ID, "cmd", req.Command, "corr", req.Corr)

	// Telegram puts a START button on every bot chat, so a human will press it here sooner or
	// later. Answering with an error envelope is technically correct and useless to them.
	if req.Command == "start" {
		h.send(ctx, msg.Chat.ID, h.startAnswer(ctx, msg.From, log), log)
		return
	}

	// A redelivered update must not be recomputed, and must produce the same answer (docs/02 §6).
	if cached, ok := h.cache.get(msg.Chat.ID, req.Corr); ok {
		log.Debug("replaying cached answer")
		h.send(ctx, msg.Chat.ID, cached, log)
		return
	}

	principal, err := h.resolver.Resolve(ctx, msg.Chat.ID)
	if err != nil {
		if !errors.Is(err, identity.ErrNoBinding) {
			log.Error("resolve failed", "err", err)
			h.reply(ctx, msg.Chat.ID, req.Corr, sushiv1.MsgType_MSG_TYPE_ERR,
				&sushiv1.Err{Code: sushiv1.ErrCode_ERR_CODE_INTERNAL}, log)
			return
		}
		log.Warn("no binding for chat", "err", err)
		h.reply(ctx, msg.Chat.ID, req.Corr, sushiv1.MsgType_MSG_TYPE_ERR,
			&sushiv1.Err{Code: h.unboundCode(ctx, msg.From, log)}, log)
		return
	}

	started := time.Now()
	msgType, payload, err := h.dispatcher.Dispatch(ctx, principal, req)
	h.finishDispatch(ctx, msg.Chat.ID, req, started, msgType, payload, err, log)
}

func (h *Handler) finishDispatch(
	ctx context.Context, chatID int64, req Request, started time.Time,
	msgType sushiv1.MsgType, payload proto.Message, err error, log *slog.Logger,
) {
	// /ack and /ev are fire-and-forget on the client. A reply would only spend this chat's
	// one-message-per-second budget (chatLimiter, docs/11 §1) on a message nobody reads.
	if req.Command == "ack" || req.Command == "ev" {
		if err != nil {
			log.Warn(req.Command+" failed (no reply sent — client does not wait for one)", "err", err)
			return
		}
		log.Info("served", "took", time.Since(started).Round(time.Millisecond))
		return
	}
	if err != nil {
		h.replyDispatchErr(ctx, chatID, req.Corr, err, log)
		return
	}
	log.Info("served", "took", time.Since(started).Round(time.Millisecond))
	h.reply(ctx, chatID, req.Corr, msgType, payload, log)
}

func (h *Handler) replyDispatchErr(ctx context.Context, chatID int64, corr uint64, err error, log *slog.Logger) {
	var rl *api.RateLimitError
	if errors.As(err, &rl) {
		log.Warn("rate limited", "retry_after", rl.RetryAfter)
		h.reply(ctx, chatID, corr, sushiv1.MsgType_MSG_TYPE_ERR,
			&sushiv1.Err{Code: sushiv1.ErrCode_ERR_CODE_RATE, RetryAfterMs: uint32(rl.RetryAfter.Milliseconds())}, log)
		return
	}
	if errors.Is(err, api.ErrNotFound) {
		log.Info("not found")
		h.reply(ctx, chatID, corr, sushiv1.MsgType_MSG_TYPE_ERR,
			&sushiv1.Err{Code: sushiv1.ErrCode_ERR_CODE_NOT_FOUND}, log)
		return
	}
	log.Error("operation failed", "err", err)
	h.reply(ctx, chatID, corr, sushiv1.MsgType_MSG_TYPE_ERR,
		&sushiv1.Err{Code: sushiv1.ErrCode_ERR_CODE_INTERNAL}, log)
}

// APIDispatcher is the Dispatcher for apid: the full app/api operation set (docs/02 §5). The API
// sees a Principal and a typed request — never a chat, a bot or an envelope (R-API-1). One small
// method per command below keeps Dispatch flat; each does exactly what it always did: decode,
// call, wrap.
type APIDispatcher struct {
	api    *api.API
	copier MessageCopier
	botID  int64
}

// NewAPIDispatcher wraps an API for apid's Handler.
func NewAPIDispatcher(a *api.API) *APIDispatcher { return &APIDispatcher{api: a} }

func (d *APIDispatcher) Dispatch(ctx context.Context, p api.Principal, req Request) (sushiv1.MsgType, proto.Message, error) {
	switch req.Command {
	case "home":
		return d.dispatchHome(ctx, p, req.Args)
	case "play":
		return d.dispatchPlay(ctx, p, req.Args)
	case "ack":
		return d.dispatchAck(ctx, p, req.Args)
	case "item":
		return d.dispatchItem(ctx, p, req.Args)
	case "episodes":
		return d.dispatchEpisodes(ctx, p, req.Args)
	case "files":
		return d.dispatchFiles(ctx, p, req.Args)
	case "search":
		return d.dispatchSearch(ctx, p, req.Args)
	case "person":
		return d.dispatchPerson(ctx, p, req.Args)
	case "list":
		return d.dispatchList(ctx, p, req.Args)
	case "request":
		return d.dispatchRequest(ctx, p, req.Args)
	case "prefs":
		return d.dispatchPrefs(ctx, p, req.Args)
	case "appupdate":
		return d.dispatchAppUpdate(ctx, p, req.Args)
	case "ev":
		return d.dispatchEvents(ctx, p, req.Args)
	case "sync":
		return d.dispatchSync(ctx, p, req.Args)
	case "subtitle":
		return d.dispatchSubtitle(ctx, p, req.Args)
	case "subtitle_file":
		return d.dispatchSubtitleFile(ctx, p, req.Args)
	case "taste":
		return d.dispatchTaste(ctx, p, req.Args)
	case "personal":
		return d.dispatchPersonal(ctx, p, req.Args)
	default:
		return 0, nil, fmt.Errorf("transport: unknown command %q", req.Command)
	}
}

func (d *APIDispatcher) dispatchHome(ctx context.Context, p api.Principal, args []byte) (sushiv1.MsgType, proto.Message, error) {
	var in sushiv1.HomeReq
	if err := unmarshalArgs(args, &in); err != nil {
		return 0, nil, err
	}
	if in.GetTab() == sushiv1.Tab_TAB_UNSPECIFIED {
		// A bare "/home" from a human debugging in the chat means the movies tab.
		in.Tab = sushiv1.Tab_TAB_MOVIES
	}
	out, err := d.api.Home(ctx, p, &in)
	if err != nil {
		return 0, nil, err
	}
	return sushiv1.MsgType_MSG_TYPE_HOME_RES, out, nil
}

func (d *APIDispatcher) dispatchPlay(ctx context.Context, p api.Principal, args []byte) (sushiv1.MsgType, proto.Message, error) {
	var in sushiv1.PlayReq
	if err := unmarshalArgs(args, &in); err != nil {
		return 0, nil, err
	}
	out, err := d.api.Play(ctx, p, &in)
	if err != nil {
		return 0, nil, err
	}
	return sushiv1.MsgType_MSG_TYPE_PLAY_RES, out, nil
}

func (d *APIDispatcher) dispatchAck(ctx context.Context, p api.Principal, args []byte) (sushiv1.MsgType, proto.Message, error) {
	var in sushiv1.AckReq
	if err := unmarshalArgs(args, &in); err != nil {
		return 0, nil, err
	}
	out, err := d.api.Ack(ctx, p, &in)
	if err != nil {
		return 0, nil, err
	}
	return sushiv1.MsgType_MSG_TYPE_ACK_RES, out, nil
}

func (d *APIDispatcher) dispatchItem(ctx context.Context, p api.Principal, args []byte) (sushiv1.MsgType, proto.Message, error) {
	var in sushiv1.ItemReq
	if err := unmarshalArgs(args, &in); err != nil {
		return 0, nil, err
	}
	out, err := d.api.Item(ctx, p, &in)
	if err != nil {
		return 0, nil, err
	}
	return sushiv1.MsgType_MSG_TYPE_ITEM_RES, out, nil
}

func (d *APIDispatcher) dispatchEpisodes(ctx context.Context, p api.Principal, args []byte) (sushiv1.MsgType, proto.Message, error) {
	var in sushiv1.EpisodesReq
	if err := unmarshalArgs(args, &in); err != nil {
		return 0, nil, err
	}
	out, err := d.api.Episodes(ctx, p, &in)
	if err != nil {
		return 0, nil, err
	}
	return sushiv1.MsgType_MSG_TYPE_EPISODES_RES, out, nil
}

func (d *APIDispatcher) dispatchFiles(ctx context.Context, p api.Principal, args []byte) (sushiv1.MsgType, proto.Message, error) {
	var in sushiv1.FilesReq
	if err := unmarshalArgs(args, &in); err != nil {
		return 0, nil, err
	}
	out, err := d.api.Files(ctx, p, &in)
	if err != nil {
		return 0, nil, err
	}
	return sushiv1.MsgType_MSG_TYPE_FILES_RES, out, nil
}

func (d *APIDispatcher) dispatchSearch(ctx context.Context, p api.Principal, args []byte) (sushiv1.MsgType, proto.Message, error) {
	var in sushiv1.SearchReq
	if err := unmarshalArgs(args, &in); err != nil {
		return 0, nil, err
	}
	out, err := d.api.Search(ctx, p, &in)
	if err != nil {
		return 0, nil, err
	}
	return sushiv1.MsgType_MSG_TYPE_SEARCH_RES, out, nil
}

func (d *APIDispatcher) dispatchPerson(ctx context.Context, p api.Principal, args []byte) (sushiv1.MsgType, proto.Message, error) {
	var in sushiv1.PersonReq
	if err := unmarshalArgs(args, &in); err != nil {
		return 0, nil, err
	}
	out, err := d.api.Person(ctx, p, &in)
	if err != nil {
		return 0, nil, err
	}
	return sushiv1.MsgType_MSG_TYPE_PERSON_RES, out, nil
}

func (d *APIDispatcher) dispatchList(ctx context.Context, p api.Principal, args []byte) (sushiv1.MsgType, proto.Message, error) {
	var in sushiv1.ListReq
	if err := unmarshalArgs(args, &in); err != nil {
		return 0, nil, err
	}
	out, err := d.api.List(ctx, p, &in)
	if err != nil {
		return 0, nil, err
	}
	return sushiv1.MsgType_MSG_TYPE_LIST_RES, out, nil
}

func (d *APIDispatcher) dispatchRequest(ctx context.Context, p api.Principal, args []byte) (sushiv1.MsgType, proto.Message, error) {
	var in sushiv1.RequestReq
	if err := unmarshalArgs(args, &in); err != nil {
		return 0, nil, err
	}
	out, err := d.api.Request(ctx, p, &in)
	if err != nil {
		return 0, nil, err
	}
	return sushiv1.MsgType_MSG_TYPE_REQUEST_RES, out, nil
}

func (d *APIDispatcher) dispatchPersonal(ctx context.Context, p api.Principal, args []byte) (sushiv1.MsgType, proto.Message, error) {
	var in sushiv1.PersonalReq
	if err := unmarshalArgs(args, &in); err != nil {
		return 0, nil, err
	}
	out, err := d.api.Personal(ctx, p, &in)
	if err != nil {
		return 0, nil, err
	}
	return sushiv1.MsgType_MSG_TYPE_PERSONAL_RES, out, nil
}

func (d *APIDispatcher) dispatchPrefs(ctx context.Context, p api.Principal, args []byte) (sushiv1.MsgType, proto.Message, error) {
	var in sushiv1.PrefsReq
	if err := unmarshalArgs(args, &in); err != nil {
		return 0, nil, err
	}
	out, err := d.api.Prefs(ctx, p, &in)
	if err != nil {
		return 0, nil, err
	}
	return sushiv1.MsgType_MSG_TYPE_PREFS_RES, out, nil
}

func (d *APIDispatcher) dispatchEvents(ctx context.Context, p api.Principal, args []byte) (sushiv1.MsgType, proto.Message, error) {
	var in sushiv1.Events
	if err := unmarshalArgs(args, &in); err != nil {
		return 0, nil, err
	}
	if err := d.api.Events(ctx, p, &in); err != nil {
		return 0, nil, err
	}
	// Fire-and-forget: no answer (docs/11). Handler still needs a type; empty AckRes unused.
	return sushiv1.MsgType_MSG_TYPE_EVENTS, &sushiv1.Events{}, nil
}

func (d *APIDispatcher) dispatchSync(ctx context.Context, p api.Principal, args []byte) (sushiv1.MsgType, proto.Message, error) {
	var in sushiv1.SyncReq
	if err := unmarshalArgs(args, &in); err != nil {
		return 0, nil, err
	}
	out, err := d.api.Sync(ctx, p, &in)
	if err != nil {
		return 0, nil, err
	}
	return sushiv1.MsgType_MSG_TYPE_SYNC_RES, out, nil
}

func (d *APIDispatcher) dispatchSubtitle(ctx context.Context, p api.Principal, args []byte) (sushiv1.MsgType, proto.Message, error) {
	var in sushiv1.SubtitleReq
	if err := unmarshalArgs(args, &in); err != nil {
		return 0, nil, err
	}
	out, err := d.api.Subtitle(ctx, p, &in)
	if err != nil {
		return 0, nil, err
	}
	return sushiv1.MsgType_MSG_TYPE_SUBTITLE_RES, out, nil
}

func (d *APIDispatcher) dispatchSubtitleFile(ctx context.Context, p api.Principal, args []byte) (sushiv1.MsgType, proto.Message, error) {
	var in sushiv1.SubtitleFileReq
	if err := unmarshalArgs(args, &in); err != nil {
		return 0, nil, err
	}
	out, err := d.api.SubtitleFile(ctx, p, &in)
	if err != nil {
		return 0, nil, err
	}
	return sushiv1.MsgType_MSG_TYPE_SUBTITLE_FILE_RES, out, nil
}

func (d *APIDispatcher) dispatchTaste(ctx context.Context, p api.Principal, args []byte) (sushiv1.MsgType, proto.Message, error) {
	var in sushiv1.TasteReq
	if err := unmarshalArgs(args, &in); err != nil {
		return 0, nil, err
	}
	out, err := d.api.Taste(ctx, p, &in)
	if err != nil {
		return 0, nil, err
	}
	return sushiv1.MsgType_MSG_TYPE_TASTE_RES, out, nil
}

func (h *Handler) reply(ctx context.Context, chatID int64, corr uint64, t sushiv1.MsgType, m proto.Message, log *slog.Logger) {
	payload, err := proto.Marshal(m)
	if err != nil {
		log.Error("marshal answer", "err", err)
		return
	}

	env := wire.Envelope{Corr: corr, Type: uint64(t), Payload: payload}
	text, err := wire.EncodeText(env)
	if err != nil {
		// Over the text limit: this is where the document path belongs (docs/03 §5). Until it
		// exists, say so plainly rather than sending something the client cannot read.
		log.Error("answer does not fit a text message", "err", err)
		return
	}

	h.cache.put(chatID, corr, text)
	h.send(ctx, chatID, text, log)
}

func (h *Handler) send(ctx context.Context, chatID int64, text string, log *slog.Logger) {
	// Roughly one message per second per chat is what Telegram allows, and the limit that hurts is
	// the bot sending (docs/11 §1). Waiting is right; dropping an answer is not.
	h.limiter.wait(ctx, chatID)

	if _, err := h.sender.SendMessage(ctx, chatID, text); err != nil {
		var tgErr *tg.Error
		if errors.As(err, &tgErr) && tgErr.IsRateLimit() {
			log.Warn("rate limited by telegram", "retry_after", tgErr.RetryAfter)
			h.limiter.penalise(chatID, tgErr.RetryAfter)
			return
		}
		log.Error("send failed", "err", err)
	}
}
