package tg_test

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	adaptertg "github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	transport "github.com/Aryan-mor/sushi-study/be/internal/adapter/transport/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/api"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
	"github.com/Aryan-mor/sushi-study/be/internal/wire"
)

func TestParseRequestAccepts(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		in   string
		cmd  string
		corr uint64
	}{
		"bare command":     {in: "/home", cmd: "home"},
		"with corr":        {in: "/home 7", cmd: "home", corr: 7},
		"corr is base36":   {in: "/home zz", cmd: "home", corr: 1295},
		"group suffix":     {in: "/home@exampleMainBot 3", cmd: "home", corr: 3},
		"case insensitive": {in: "/HOME 1", cmd: "home", corr: 1},
		"padded":           {in: "  /home 2  ", cmd: "home", corr: 2},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			req, err := transport.ParseRequest(tc.in)
			if err != nil {
				t.Fatalf("ParseRequest(%q): %v", tc.in, err)
			}
			if req.Command != tc.cmd || req.Corr != tc.corr {
				t.Errorf("got %q/%d, want %q/%d", req.Command, req.Corr, tc.cmd, tc.corr)
			}
		})
	}
}

// Anything a person might type is not a failure, it is simply not addressed to us. The distinction
// matters because an API bot must stay silent rather than answer chatter (docs/02 §4).
func TestParseRequestIgnores(t *testing.T) {
	t.Parallel()

	for name, in := range map[string]string{
		"human typing": "hello there",
		"empty":        "",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := transport.ParseRequest(in); !errors.Is(err, transport.ErrNotARequest) {
				t.Fatalf("ParseRequest(%q) err = %v, want ErrNotARequest", in, err)
			}
		})
	}
}

// A malformed correlation id, by contrast, IS addressed to us and is worth an error: the client
// sent something it should not have.
func TestParseRequestRejects(t *testing.T) {
	t.Parallel()

	for name, in := range map[string]string{
		"bad corr": "/home !!",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := transport.ParseRequest(in); err == nil {
				t.Fatalf("ParseRequest(%q) succeeded, want an error", in)
			}
		})
	}
}

// fakeSender stands in for Telegram: the transport is testable without a network, which is the
// property R-TEST-5 asks for.
type fakeSender struct {
	mu   sync.Mutex
	sent []string
}

func (f *fakeSender) SendMessage(_ context.Context, _ int64, text string) (adaptertg.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, text)
	return adaptertg.Message{}, nil
}

func (f *fakeSender) texts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.sent))
	copy(out, f.sent)
	return out
}

type fixedResolver struct{ err error }

func (r fixedResolver) Resolve(context.Context, int64) (api.Principal, error) {
	if r.err != nil {
		return api.Principal{}, r.err
	}
	return api.Principal{UserID: 1}, nil
}

type stubRails struct{}

func (stubRails) Rail(context.Context, catalog.RailKind, catalog.Kind) ([]catalog.Row, error) {
	return []catalog.Row{{TMDBID: 603, Kind: catalog.KindMovie, Title: "The Matrix", Year: 1999}}, nil
}
func (stubRails) RailSeq(context.Context) (uint64, error) { return 4, nil }

// stubDeliveries is a no-op DeliveryStore: these tests exercise dispatch and reply plumbing, not
// Play's own state machine (that is api_test.go's job, per R-API-3 -- business logic is tested
// through the API layer, not through Telegram).
type stubDeliveries struct{ pending int }

func (s stubDeliveries) Delivery(context.Context, int64, catalog.MediaKind, int64) (api.Delivery, bool, error) {
	return api.Delivery{}, false, nil
}

func (s stubDeliveries) PendingCount(context.Context, int64) (int, error) { return s.pending, nil }

func (stubDeliveries) UnclaimedPendingOlderThan(context.Context, int64, time.Duration) (int, error) {
	return 0, nil
}

func (stubDeliveries) DeliveredCountSince(context.Context, int64, time.Time) (int, error) {
	return 0, nil
}

func (stubDeliveries) Enqueue(context.Context, int64, catalog.MediaKind, int64, string) error {
	return nil
}

func (stubDeliveries) ForceRedeliver(context.Context, int64, catalog.MediaKind, int64) error {
	return nil
}

func (stubDeliveries) Ack(context.Context, int64, catalog.MediaKind, int64, int64) error { return nil }

// stubCatalog is a no-op CatalogStore: these tests exercise dispatch and reply plumbing, not
// Item/Files' own logic (that is api_test.go's job, per R-API-3).
type stubCatalog struct{}

func (stubCatalog) Item(context.Context, uint64, catalog.Kind) (catalog.Item, bool, error) {
	return catalog.Item{}, false, nil
}

func (stubCatalog) Files(context.Context, int64) ([]catalog.File, error) { return nil, nil }

func (stubCatalog) EpisodesForSeason(context.Context, uint64, catalog.Kind, int32, int32, int32) ([]catalog.Episode, int32, error) {
	return nil, 0, nil
}

func (stubCatalog) FirstPlayableEpisode(context.Context, uint64, catalog.Kind) (catalog.Episode, bool, error) {
	return catalog.Episode{}, false, nil
}

// stubSearch is a no-op SearchStore: these tests exercise dispatch and reply plumbing, not
// Search's own logic (that is api_test.go's job, per R-API-3).
type stubSearch struct{}

func (stubSearch) SearchItems(context.Context, string, uint32, uint32) ([]catalog.Row, error) {
	return []catalog.Row{{TMDBID: 603, Kind: catalog.KindMovie, Title: "The Matrix", Year: 1999}}, nil
}

func (stubSearch) RecordSearch(context.Context, int64) error { return nil }

func (stubSearch) SearchCountSince(context.Context, int64, time.Time) (int, error) { return 0, nil }

func (stubSearch) CatalogKeysByTMDB(context.Context, []uint64) ([]catalog.RowKey, error) {
	return nil, nil
}

func (stubSearch) CatalogRowsByTMDBAnyKind(context.Context, []uint64) ([]catalog.Row, error) {
	return nil, nil
}

func newHandler(t *testing.T, resolver transport.PrincipalResolver) (*transport.Handler, *fakeSender) {
	t.Helper()
	return newHandlerWithAPI(t, resolver, stubDeliveries{}, api.RateLimits{PerHour: 30, MaxConcurrent: 2, SearchPerHour: 60})
}

func newHandlerWithAPI(
	t *testing.T, resolver transport.PrincipalResolver, deliveries api.DeliveryStore, limits api.RateLimits,
) (*transport.Handler, *fakeSender) {
	t.Helper()

	sender := &fakeSender{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := api.New(stubRails{}, deliveries, stubCatalog{}, stubSearch{}, limits)
	dispatcher := transport.NewAPIDispatcher(service)
	return transport.NewHandler(dispatcher, sender, resolver, "exampleMainBot", log), sender
}

// encodeArgsForTest is the inverse of transport.decodeArgs (support.go): base64url, matching what
// a real client puts in the third space-separated field (docs/02 §3).
func encodeArgsForTest(args []byte) string {
	return base64.RawURLEncoding.EncodeToString(args)
}

func decodeOnly(t *testing.T, text string) wire.Envelope {
	t.Helper()

	env, err := wire.DecodeText(text)
	if err != nil {
		t.Fatalf("the client could not decode what we sent (%q): %v", text, err)
	}
	return env
}

func TestHandleServesHome(t *testing.T) {
	t.Parallel()

	handler, sender := newHandler(t, fixedResolver{})
	handler.Handle(context.Background(), &adaptertg.Message{
		Chat: adaptertg.Chat{ID: 55},
		Text: "/home 7",
	})

	sent := sender.texts()
	if len(sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(sent))
	}

	env := decodeOnly(t, sent[0])
	if env.Corr != 7 {
		t.Errorf("corr = %d, want the request's 7 echoed back", env.Corr)
	}
	if env.Type != uint64(sushiv1.MsgType_MSG_TYPE_HOME_RES) {
		t.Errorf("type = %d, want HOME_RES", env.Type)
	}

	var res sushiv1.HomeRes
	if err := proto.Unmarshal(env.Payload, &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(res.GetRails()) == 0 {
		t.Fatal("no rails in the answer")
	}
	if got := res.GetRails()[0].GetRows()[0].GetTitle(); got != "The Matrix" {
		t.Errorf("first row = %q", got)
	}
}

func TestHandleIsIdempotentForARepeatedCorr(t *testing.T) {
	t.Parallel()

	handler, sender := newHandler(t, fixedResolver{})
	msg := &adaptertg.Message{Chat: adaptertg.Chat{ID: 55}, Text: "/home 9"}

	// Telegram redelivers, or a client that timed out retries with the same corr on purpose.
	handler.Handle(context.Background(), msg)
	handler.Handle(context.Background(), msg)

	sent := sender.texts()
	if len(sent) != 2 {
		t.Fatalf("sent %d messages, want 2 (the retry is answered, not ignored)", len(sent))
	}
	if sent[0] != sent[1] {
		t.Error("the same request produced two different answers")
	}
}

func TestHandleIgnoresChatter(t *testing.T) {
	t.Parallel()

	handler, sender := newHandler(t, fixedResolver{})
	for _, text := range []string{"hello", "", "not a command", "/unknown 1"} {
		handler.Handle(context.Background(), &adaptertg.Message{
			Chat: adaptertg.Chat{ID: 55}, Text: text,
		})
	}

	// Prose gets no reply at all; an unknown command gets an error envelope rather than silence,
	// so a client bug is visible instead of looking like a hang.
	sent := sender.texts()
	if len(sent) != 1 {
		t.Fatalf("sent %d messages, want exactly 1 (for the unknown command)", len(sent))
	}
	env := decodeOnly(t, sent[0])
	if env.Type != uint64(sushiv1.MsgType_MSG_TYPE_ERR) {
		t.Errorf("type = %d, want ERR", env.Type)
	}
}

func TestHandleRefusesAnUnboundChat(t *testing.T) {
	t.Parallel()

	handler, sender := newHandler(t, fixedResolver{err: identity.ErrNoBinding})
	handler.Handle(context.Background(), &adaptertg.Message{
		Chat: adaptertg.Chat{ID: 55}, Text: "/home 1",
	})

	sent := sender.texts()
	if len(sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(sent))
	}
	env := decodeOnly(t, sent[0])
	var e sushiv1.Err
	if err := proto.Unmarshal(env.Payload, &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if e.GetCode() != sushiv1.ErrCode_ERR_CODE_AUTH {
		t.Errorf("code = %v, want ERR_CODE_AUTH", e.GetCode())
	}
}

func TestUnboundRefusedPersonGetsNotPublicInsteadOfAuth(t *testing.T) {
	t.Parallel()

	handler, sender := newHandler(t, fixedResolver{err: identity.ErrNoBinding})
	handler.WithAccess(fixedAccess{allowed: false})
	handler.Handle(context.Background(), &adaptertg.Message{
		From: &adaptertg.User{ID: 55}, Chat: adaptertg.Chat{ID: 55}, Text: "/initbot 1",
	})

	sent := sender.texts()
	if len(sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(sent))
	}
	env := decodeOnly(t, sent[0])
	var e sushiv1.Err
	if err := proto.Unmarshal(env.Payload, &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if e.GetCode() != sushiv1.ErrCode_ERR_CODE_NOT_PUBLIC {
		t.Errorf("code = %v, want ERR_CODE_NOT_PUBLIC", e.GetCode())
	}
}

func TestHandleResolveInfraErrorIsInternal(t *testing.T) {
	t.Parallel()

	handler, sender := newHandler(t, fixedResolver{err: errors.New("column beta_tester does not exist")})
	handler.Handle(context.Background(), &adaptertg.Message{
		Chat: adaptertg.Chat{ID: 55}, Text: "/initbot 1",
	})

	sent := sender.texts()
	if len(sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(sent))
	}
	env := decodeOnly(t, sent[0])
	var e sushiv1.Err
	if err := proto.Unmarshal(env.Payload, &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if e.GetCode() != sushiv1.ErrCode_ERR_CODE_INTERNAL {
		t.Errorf("code = %v, want ERR_CODE_INTERNAL", e.GetCode())
	}
}

func TestHandleDispatchesPlay(t *testing.T) {
	t.Parallel()

	handler, sender := newHandlerWithAPI(t, fixedResolver{}, stubDeliveries{}, api.RateLimits{PerHour: 30, MaxConcurrent: 2})
	args, err := proto.Marshal(&sushiv1.PlayReq{FileId: 4211})
	if err != nil {
		t.Fatalf("marshal PlayReq: %v", err)
	}
	handler.Handle(context.Background(), &adaptertg.Message{
		Chat: adaptertg.Chat{ID: 55},
		Text: "/play 1 " + encodeArgsForTest(args),
	})

	sent := sender.texts()
	if len(sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(sent))
	}
	env := decodeOnly(t, sent[0])
	if env.Type != uint64(sushiv1.MsgType_MSG_TYPE_PLAY_RES) {
		t.Errorf("type = %d, want PLAY_RES", env.Type)
	}
	var res sushiv1.PlayRes
	if err := proto.Unmarshal(env.Payload, &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.GetPending().GetLocator() != "plm_4211" {
		t.Errorf("locator = %q, want plm_4211", res.GetPending().GetLocator())
	}
}

// ackTrackingDeliveries records its Ack calls so a test can assert the API layer actually ran,
// even though (per TestHandleAckSendsNoReply) nothing comes back over the wire to prove it.
type ackTrackingDeliveries struct {
	stubDeliveries
	acked []sushiv1.AckReq
	err   error
}

func (d *ackTrackingDeliveries) Ack(_ context.Context, _ int64, _ catalog.MediaKind, fileID, messageID int64) error {
	d.acked = append(d.acked, sushiv1.AckReq{FileId: fileID, MessageId: messageID})
	return d.err
}

// The client (sushiSendTextFireAndForget, sushi-app's sushi_play_transport.dart) never waits for a
// reply to /ack -- it has nothing to learn from one -- so the server must not send it one: doing so
// would only spend this chat's one-message-per-second budget (chatLimiter, docs/11 §1) on a message
// nobody reads. The API call must still happen -- ackTrackingDeliveries proves that side.
func TestHandleAckSendsNoReply(t *testing.T) {
	t.Parallel()

	deliveries := &ackTrackingDeliveries{}
	handler, sender := newHandlerWithAPI(t, fixedResolver{}, deliveries, api.RateLimits{PerHour: 30, MaxConcurrent: 2})
	args, err := proto.Marshal(&sushiv1.AckReq{FileId: 4211, MessageId: 99000})
	if err != nil {
		t.Fatalf("marshal AckReq: %v", err)
	}
	handler.Handle(context.Background(), &adaptertg.Message{
		Chat: adaptertg.Chat{ID: 55},
		Text: "/ack 1 " + encodeArgsForTest(args),
	})

	if sent := sender.texts(); len(sent) != 0 {
		t.Fatalf("sent %d messages, want 0 (ack replies nothing)", len(sent))
	}
	if len(deliveries.acked) != 1 || deliveries.acked[0].GetFileId() != 4211 || deliveries.acked[0].GetMessageId() != 99000 {
		t.Fatalf("Ack calls = %+v, want exactly one for file_id=4211 message_id=99000", deliveries.acked)
	}
}

// A failed ack is exactly as silent as a successful one -- AckReq is best-effort either way
// (docs/05 §6), so there is still nothing worth telling a client that is not listening.
func TestHandleAckFailureSendsNoReply(t *testing.T) {
	t.Parallel()

	deliveries := &ackTrackingDeliveries{err: errors.New("db unavailable")}
	handler, sender := newHandlerWithAPI(t, fixedResolver{}, deliveries, api.RateLimits{PerHour: 30, MaxConcurrent: 2})
	args, err := proto.Marshal(&sushiv1.AckReq{FileId: 1, MessageId: 2})
	if err != nil {
		t.Fatalf("marshal AckReq: %v", err)
	}
	handler.Handle(context.Background(), &adaptertg.Message{
		Chat: adaptertg.Chat{ID: 55},
		Text: "/ack 1 " + encodeArgsForTest(args),
	})

	if sent := sender.texts(); len(sent) != 0 {
		t.Fatalf("sent %d messages, want 0 even on ack failure", len(sent))
	}
}

func TestHandleDispatchesSearch(t *testing.T) {
	t.Parallel()

	handler, sender := newHandler(t, fixedResolver{})
	args, err := proto.Marshal(&sushiv1.SearchReq{Query: "tenet"})
	if err != nil {
		t.Fatalf("marshal SearchReq: %v", err)
	}
	handler.Handle(context.Background(), &adaptertg.Message{
		Chat: adaptertg.Chat{ID: 55},
		Text: "/search 1 " + encodeArgsForTest(args),
	})

	sent := sender.texts()
	if len(sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(sent))
	}
	env := decodeOnly(t, sent[0])
	if env.Type != uint64(sushiv1.MsgType_MSG_TYPE_SEARCH_RES) {
		t.Errorf("type = %d, want SEARCH_RES", env.Type)
	}
	var res sushiv1.SearchRes
	if err := proto.Unmarshal(env.Payload, &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(res.GetRows()) == 0 {
		t.Fatal("no rows in the answer")
	}
}

// stubRequests is the smallest api.RequestStore that lets a /request round-trip through the
// transport. Request's own logic is api_test.go's job (R-API-3); this only proves the command is
// wired and the answer comes back typed.
type stubRequests struct{}

func (stubRequests) CatalogPlayable(context.Context, int64, uint64, catalog.Kind) (bool, error) {
	return false, nil
}

func (stubRequests) RequestCountSince(context.Context, int64, time.Time) (int, error) { return 0, nil }

func (stubRequests) TitleSnapshot(context.Context, uint64, catalog.Kind) (string, string, error) {
	return "Tenet", "/tenet.jpg", nil
}

func (stubRequests) InsertRequest(context.Context, api.NewRequest) (bool, error) { return true, nil }

func TestHandleDispatchesRequest(t *testing.T) {
	t.Parallel()

	sender := &fakeSender{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := api.New(stubRails{}, stubDeliveries{}, stubCatalog{}, stubSearch{},
		api.RateLimits{RequestsPerDay: 10}).WithRequests(stubRequests{})
	handler := transport.NewHandler(transport.NewAPIDispatcher(service), sender, fixedResolver{}, "exampleMainBot", log)

	args, err := proto.Marshal(&sushiv1.RequestReq{TmdbId: 577922, Kind: sushiv1.Kind_KIND_MOVIE})
	if err != nil {
		t.Fatalf("marshal RequestReq: %v", err)
	}
	handler.Handle(context.Background(), &adaptertg.Message{
		Chat: adaptertg.Chat{ID: 55},
		Text: "/request 1 " + encodeArgsForTest(args),
	})

	sent := sender.texts()
	if len(sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(sent))
	}
	env := decodeOnly(t, sent[0])
	if env.Type != uint64(sushiv1.MsgType_MSG_TYPE_REQUEST_RES) {
		t.Errorf("type = %d, want REQUEST_RES", env.Type)
	}
	var res sushiv1.RequestRes
	if err := proto.Unmarshal(env.Payload, &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.GetOutcome() != sushiv1.RequestOutcome_REQUEST_OUTCOME_ACCEPTED {
		t.Errorf("outcome = %v, want ACCEPTED", res.GetOutcome())
	}
}

type stubGeminiKeys struct {
	key string
	ok  bool
}

func (s stubGeminiKeys) GeminiAPIKey(context.Context, int64) (string, bool, error) {
	return s.key, s.ok, nil
}

func TestHandleDispatchesPrefs(t *testing.T) {
	t.Parallel()

	sender := &fakeSender{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := api.New(stubRails{}, stubDeliveries{}, stubCatalog{}, stubSearch{},
		api.RateLimits{RequestsPerDay: 10}).WithGeminiKeys(stubGeminiKeys{
		key: "AIzaSyDummyKeyForTestsOnly1234567890AB",
		ok:  true,
	})
	handler := transport.NewHandler(transport.NewAPIDispatcher(service), sender, fixedResolver{}, "exampleMainBot", log)

	handler.Handle(context.Background(), &adaptertg.Message{
		Chat: adaptertg.Chat{ID: 55},
		Text: "/prefs 1",
	})

	sent := sender.texts()
	if len(sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(sent))
	}
	env := decodeOnly(t, sent[0])
	if env.Type != uint64(sushiv1.MsgType_MSG_TYPE_PREFS_RES) {
		t.Errorf("type = %d, want PREFS_RES", env.Type)
	}
	var res sushiv1.PrefsRes
	if err := proto.Unmarshal(env.Payload, &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !res.GetHasGeminiKey() || res.GetGeminiApiKey() != "AIzaSyDummyKeyForTestsOnly1234567890AB" {
		t.Errorf("has=%v key=%q", res.GetHasGeminiKey(), res.GetGeminiApiKey())
	}
}

// A rate limit is a normal, expected outcome (R-SEC-11), not an internal failure, and docs/02 §6
// forbids answering ERR_RATE with retry_after_ms = 0 -- that value means "do not retry at all".
func TestHandleMapsRateLimitToErrRateWithARetryHint(t *testing.T) {
	t.Parallel()

	handler, sender := newHandlerWithAPI(t, fixedResolver{}, stubDeliveries{pending: 5}, api.RateLimits{PerHour: 30, MaxConcurrent: 1})
	args, err := proto.Marshal(&sushiv1.PlayReq{FileId: 1})
	if err != nil {
		t.Fatalf("marshal PlayReq: %v", err)
	}
	handler.Handle(context.Background(), &adaptertg.Message{
		Chat: adaptertg.Chat{ID: 55},
		Text: "/play 1 " + encodeArgsForTest(args),
	})

	sent := sender.texts()
	if len(sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(sent))
	}
	env := decodeOnly(t, sent[0])
	var e sushiv1.Err
	if err := proto.Unmarshal(env.Payload, &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if e.GetCode() != sushiv1.ErrCode_ERR_CODE_RATE {
		t.Fatalf("code = %v, want ERR_CODE_RATE", e.GetCode())
	}
	if e.GetRetryAfterMs() == 0 {
		t.Error("retry_after_ms = 0, which the client reads as \"never retry\" -- never send that for a real rate limit")
	}
}

type fixedAccess struct{ allowed bool }

func (a fixedAccess) Authorized(context.Context, int64) (bool, error) { return a.allowed, nil }

func TestStartTellsARefusedPersonTheAppIsNotPublic(t *testing.T) {
	t.Parallel()
	handler, sender := newHandler(t, fixedResolver{})
	handler.WithAccess(fixedAccess{allowed: false})
	handler.Handle(context.Background(), &adaptertg.Message{
		From: &adaptertg.User{ID: 987654}, Chat: adaptertg.Chat{ID: 987654}, Text: "/start",
	})

	sent := sender.texts()
	if len(sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(sent))
	}
	if !strings.Contains(sent[0], "not open for public use") || !strings.Contains(sent[0], "987654") {
		t.Errorf("refusal = %q", sent[0])
	}
	if strings.Contains(sent[0], "@") {
		t.Errorf("refusal points at a bot: %q", sent[0])
	}
}

func TestStartKeepsItsNormalAnswerForAnAuthorizedPerson(t *testing.T) {
	t.Parallel()
	handler, sender := newHandler(t, fixedResolver{})
	handler.WithAccess(fixedAccess{allowed: true})
	handler.Handle(context.Background(), &adaptertg.Message{
		From: &adaptertg.User{ID: 5}, Chat: adaptertg.Chat{ID: 5}, Text: "/start",
	})

	sent := sender.texts()
	if len(sent) != 1 || strings.Contains(sent[0], "not open for public use") {
		t.Errorf("sent = %q", sent)
	}
}

func TestStartGetsAHumanAnswer(t *testing.T) {
	t.Parallel()

	// Telegram puts a START button on every bot chat, so somebody will press it here. An error
	// envelope is technically correct and useless to a person.
	handler, sender := newHandler(t, fixedResolver{})
	handler.Handle(context.Background(), &adaptertg.Message{
		Chat: adaptertg.Chat{ID: 55}, Text: "/start",
	})

	sent := sender.texts()
	if len(sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(sent))
	}
	if strings.HasPrefix(sent[0], "!") {
		t.Errorf("answered a human with a protocol envelope: %q", sent[0])
	}
	if !strings.Contains(sent[0], "@exampleMainBot") {
		t.Errorf("answer does not point at main-bot: %q", sent[0])
	}
}
