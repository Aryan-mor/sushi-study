package tg

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/proto"

	"github.com/Aryan-mor/sushi-study/be/internal/app/api"
	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
)

// MessageCopier is the Telegram half of /appupdate (ADR 0019). The API looks up the shelf;
// this copies into the protocol chat. Declared here because only the transport talks to Telegram.
type MessageCopier interface {
	CopyMessage(ctx context.Context, destChat, sourceChat, messageID int64, caption string) (int64, error)
}

// WithAppCopy wires the bot that received the request as the copier. botID is that bot's
// Telegram user id (getMe), which the client uses to fetch the copied document.
func (d *APIDispatcher) WithAppCopy(copier MessageCopier, botID int64) *APIDispatcher {
	d.copier = copier
	d.botID = botID
	return d
}

func (d *APIDispatcher) dispatchAppUpdate(ctx context.Context, p api.Principal, args []byte) (sushiv1.MsgType, proto.Message, error) {
	var in sushiv1.AppUpdateReq
	if err := unmarshalArgs(args, &in); err != nil {
		return 0, nil, err
	}
	info, err := d.api.LookupAppRelease(ctx, in.GetPlatform(), p)
	if err != nil {
		return 0, nil, err
	}
	if d.copier == nil {
		return 0, nil, fmt.Errorf("transport: appupdate copier not configured")
	}
	locator := api.AppUpdateLocator(in.GetPlatform(), info.Version)
	destID, err := d.copier.CopyMessage(ctx, p.ReaderChatID, info.ChannelID, info.MessageID, locator)
	if err != nil {
		return 0, nil, fmt.Errorf("transport: copy app release: %w", err)
	}
	return sushiv1.MsgType_MSG_TYPE_APP_UPDATE_RES, &sushiv1.AppUpdateRes{
		BotId:     d.botID,
		MessageId: destID,
		Version:   info.Version,
		FileName:  info.FileName,
		Locator:   locator,
	}, nil
}
