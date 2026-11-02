package tg

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/proto"

	"github.com/Aryan-mor/sushi-study/be/internal/app/api"
	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
)

// initIdentity is what InitDispatcher needs from identity.Service -- declared by the consumer, and
// small (R-GO-5).
type initIdentity interface {
	identityResolver
	Pool(ctx context.Context) ([]string, error)
}

// InitDispatcher is init-bot's Dispatcher: the machine half of /initbot (docs/02 §1, ADR 0004) --
// "who am I, which API bots are in the pool?", answered with the Assignment envelope and nothing else.
//
// It never creates a binding itself: by the time a chat reaches Dispatch, Handle's pre-dispatch
// resolver gate has already required a live one to exist, and that binding was written either by
// main-bot at the end of onboarding (session) or by init-bot's own token-paste enrolment (bot
// login, docs/10 Q3). So Dispatch only ever reads, which makes re-running /initbot genuinely
// idempotent (docs/02 §1) -- unlike the single call this replaced, which minted a fresh epoch on
// every re-run.
type InitDispatcher struct {
	identity initIdentity
	// deliveryBots rides along in every Assignment so the client can pre-start them (docs/05 §6).
	// Static config, not identity's concern -- unlike the API bot pool, it never changes per user.
	deliveryBots []string
}

// NewInitDispatcher wraps an identity service for init-bot's Handler. deliveryBots is config.
// InitBot.DeliveryBots -- may be empty (e.g. unparsable DELIVERY_BOTS), in which case Assignment
// just carries none. An empty API pool is a hard error: the client has nothing to round-robin.
func NewInitDispatcher(svc initIdentity, deliveryBots []string) *InitDispatcher {
	return &InitDispatcher{identity: svc, deliveryBots: deliveryBots}
}

func (d *InitDispatcher) Dispatch(ctx context.Context, p api.Principal, req Request) (sushiv1.MsgType, proto.Message, error) {
	if req.Command != "initbot" {
		return 0, nil, fmt.Errorf("transport: unknown command %q", req.Command)
	}

	binding, err := d.identity.Resolve(ctx, p.ReaderChatID)
	if err != nil {
		return 0, nil, err
	}

	pool, err := d.identity.Pool(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("transport: api bot pool: %w", err)
	}
	if len(pool) == 0 {
		return 0, nil, fmt.Errorf("transport: no API bot is registered")
	}

	return sushiv1.MsgType_MSG_TYPE_ASSIGNMENT, &sushiv1.Assignment{
		ApiBotUsername: pool[0],
		Pool:           pool,
		ProviderId:     binding.ProviderID,
		BindingToken:   binding.Token,
		Epoch:          binding.Epoch,
		DeliveryBots:   d.deliveryBots,
	}, nil
}
