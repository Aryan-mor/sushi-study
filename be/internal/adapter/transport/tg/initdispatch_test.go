package tg_test

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/protobuf/proto"

	transport "github.com/Aryan-mor/sushi-study/be/internal/adapter/transport/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/api"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
)

// fakeIdentity is the in-process store InitDispatcher runs against: a fixed binding per chat, and
// an optional pool -- enough to exercise init-bot's own logic without a database (R-API-3's
// reasoning applied to init-bot's Dispatcher, not just apid's).
type fakeIdentity struct {
	bindings map[int64]identity.Binding
	pool     []string
	poolErr  error
	err      error
}

func (f *fakeIdentity) Resolve(_ context.Context, chatID int64) (identity.Binding, error) {
	if f.err != nil {
		return identity.Binding{}, f.err
	}
	b, ok := f.bindings[chatID]
	if !ok {
		return identity.Binding{}, identity.ErrNoBinding
	}
	return b, nil
}

func (f *fakeIdentity) Pool(context.Context) ([]string, error) {
	if f.poolErr != nil {
		return nil, f.poolErr
	}
	return f.pool, nil
}

func TestInitDispatcherAnswersInitbotWithTheExistingBinding(t *testing.T) {
	t.Parallel()

	svc := &fakeIdentity{
		bindings: map[int64]identity.Binding{
			55: {ProviderID: 1, Token: []byte("tok"), Epoch: 3},
		},
		pool: []string{"api1_bot", "api2_bot"},
	}
	d := transport.NewInitDispatcher(svc, nil)

	msgType, msg, err := d.Dispatch(context.Background(), api.Principal{ReaderChatID: 55}, transport.Request{Command: "initbot"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if msgType != sushiv1.MsgType_MSG_TYPE_ASSIGNMENT {
		t.Errorf("type = %v, want ASSIGNMENT", msgType)
	}
	assignment, ok := msg.(*sushiv1.Assignment)
	if !ok {
		t.Fatalf("payload = %T, want *sushiv1.Assignment", msg)
	}
	if assignment.GetApiBotUsername() != "api1_bot" || assignment.GetEpoch() != 3 {
		t.Errorf("assignment = %+v, want pool[0]=api1_bot/epoch 3", assignment)
	}
	if len(assignment.GetPool()) != 2 {
		t.Errorf("pool = %v, want 2 entries", assignment.GetPool())
	}
}

// Re-running /initbot must return the same answer, unchanged -- no epoch bump, no new binding
// (docs/02 §1: idempotent unless the binding is actually broken).
func TestInitDispatcherIsIdempotent(t *testing.T) {
	t.Parallel()

	svc := &fakeIdentity{
		bindings: map[int64]identity.Binding{55: {Epoch: 1}},
		pool:     []string{"api1_bot"},
	}
	d := transport.NewInitDispatcher(svc, nil)
	ctx := context.Background()

	_, first, err := d.Dispatch(ctx, api.Principal{ReaderChatID: 55}, transport.Request{Command: "initbot"})
	if err != nil {
		t.Fatalf("first Dispatch: %v", err)
	}
	_, second, err := d.Dispatch(ctx, api.Principal{ReaderChatID: 55}, transport.Request{Command: "initbot"})
	if err != nil {
		t.Fatalf("second Dispatch: %v", err)
	}
	firstBytes, err := proto.Marshal(first)
	if err != nil {
		t.Fatalf("marshal first: %v", err)
	}
	secondBytes, err := proto.Marshal(second)
	if err != nil {
		t.Fatalf("marshal second: %v", err)
	}
	if string(firstBytes) != string(secondBytes) {
		t.Error("a repeated /initbot produced a different answer")
	}
}

func TestInitDispatcherSurfacesAPoolListFailure(t *testing.T) {
	t.Parallel()

	svc := &fakeIdentity{
		bindings: map[int64]identity.Binding{55: {Epoch: 1}},
		poolErr:  errors.New("database is down"),
	}
	d := transport.NewInitDispatcher(svc, nil)

	_, _, err := d.Dispatch(context.Background(), api.Principal{ReaderChatID: 55}, transport.Request{Command: "initbot"})
	if err == nil {
		t.Fatal("want an error when the pool cannot be listed")
	}
}

func TestInitDispatcherRejectsAnEmptyPool(t *testing.T) {
	t.Parallel()

	svc := &fakeIdentity{bindings: map[int64]identity.Binding{55: {Epoch: 1}}}
	d := transport.NewInitDispatcher(svc, nil)

	_, _, err := d.Dispatch(context.Background(), api.Principal{ReaderChatID: 55}, transport.Request{Command: "initbot"})
	if err == nil {
		t.Fatal("want an error when no API bot is registered")
	}
}

// A reader's first play always fails `400 chat not found` against whichever delivery bot
// deliveryd's round-robin picks, unless the client has pre-started it -- so every Assignment must
// carry the delivery pool the same way it carries the API bot pool (docs/05 §6, docs/10 Q3).
func TestInitDispatcherIncludesTheDeliveryBotPool(t *testing.T) {
	t.Parallel()

	svc := &fakeIdentity{
		bindings: map[int64]identity.Binding{55: {Epoch: 1}},
		pool:     []string{"api1_bot"},
	}
	d := transport.NewInitDispatcher(svc, []string{"dl1_bot", "dl2_bot", "dl3_bot"})

	_, msg, err := d.Dispatch(context.Background(), api.Principal{ReaderChatID: 55}, transport.Request{Command: "initbot"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	assignment, ok := msg.(*sushiv1.Assignment)
	if !ok {
		t.Fatalf("payload = %T, want *sushiv1.Assignment", msg)
	}
	if got := assignment.GetDeliveryBots(); len(got) != 3 || got[0] != "dl1_bot" {
		t.Errorf("deliveryBots = %v, want [dl1_bot dl2_bot dl3_bot]", got)
	}
}

func TestInitDispatcherRejectsAnyOtherCommand(t *testing.T) {
	t.Parallel()

	d := transport.NewInitDispatcher(&fakeIdentity{}, nil)
	_, _, err := d.Dispatch(context.Background(), api.Principal{ReaderChatID: 55}, transport.Request{Command: "home"})
	if err == nil {
		t.Fatal("want an error; init-bot only ever answers /initbot")
	}
}

func TestInitDispatcherSurfacesNoBinding(t *testing.T) {
	t.Parallel()

	d := transport.NewInitDispatcher(&fakeIdentity{bindings: map[int64]identity.Binding{}}, nil)
	_, _, err := d.Dispatch(context.Background(), api.Principal{ReaderChatID: 999}, transport.Request{Command: "initbot"})
	if !errors.Is(err, identity.ErrNoBinding) {
		t.Errorf("err = %v, want identity.ErrNoBinding", err)
	}
}

func TestBoundResolverMapsABindingOntoAPrincipal(t *testing.T) {
	t.Parallel()

	svc := &fakeIdentity{bindings: map[int64]identity.Binding{
		55: {UserID: 9, ProviderID: 1, ReaderChatID: 55, Role: identity.RoleAdmin},
	}}
	r := transport.BoundResolver{Identity: svc}

	p, err := r.Resolve(context.Background(), 55)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.UserID != 9 || p.ReaderChatID != 55 {
		t.Errorf("principal = %+v, want user 9 reader 55", p)
	}
	if p.Role != identity.RoleAdmin {
		t.Errorf("principal role = %q, want it carried from the binding", p.Role)
	}
}

func TestBoundResolverSurfacesNoBinding(t *testing.T) {
	t.Parallel()

	r := transport.BoundResolver{Identity: &fakeIdentity{bindings: map[int64]identity.Binding{}}}
	if _, err := r.Resolve(context.Background(), 55); !errors.Is(err, identity.ErrNoBinding) {
		t.Errorf("err = %v, want identity.ErrNoBinding", err)
	}
}
