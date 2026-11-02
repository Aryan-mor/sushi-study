package tg_test

import (
	"context"
	"testing"

	"google.golang.org/protobuf/proto"

	transport "github.com/Aryan-mor/sushi-study/be/internal/adapter/transport/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/api"
	sushiv1 "github.com/Aryan-mor/sushi-study/be/internal/protocol/sushi/v1"
)

type stubAppCopier struct {
	dest, srcChat, srcMsg int64
	caption               string
	destID                int64
}

func (c *stubAppCopier) CopyMessage(_ context.Context, dest, srcChat, srcMsg int64, caption string) (int64, error) {
	c.dest, c.srcChat, c.srcMsg, c.caption = dest, srcChat, srcMsg, caption
	return c.destID, nil
}

func TestDispatchAppUpdateCopiesIntoTheReaderChat(t *testing.T) {
	t.Parallel()

	svc := api.New(&stubRails{}, stubDeliveries{}, stubCatalog{}, stubSearch{}, api.RateLimits{
		PerHour: 30, MaxConcurrent: 2, SearchPerHour: 60,
	}).WithAppReleases(stubReleases{info: api.AppReleaseInfo{
		ChannelID: -1009900000001, MessageID: 44, Version: "1.2.0", FileName: "sushi.apk",
	}, ok: true})
	copier := &stubAppCopier{destID: 88}
	d := transport.NewAPIDispatcher(svc).WithAppCopy(copier, 8271796073)

	args, err := proto.Marshal(&sushiv1.AppUpdateReq{Platform: "android_new"})
	if err != nil {
		t.Fatal(err)
	}
	msgType, payload, err := d.Dispatch(context.Background(), api.Principal{ReaderChatID: 55}, transport.Request{
		Command: "appupdate", Args: args,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if msgType != sushiv1.MsgType_MSG_TYPE_APP_UPDATE_RES {
		t.Fatalf("type = %v", msgType)
	}
	res, ok := payload.(*sushiv1.AppUpdateRes)
	if !ok {
		t.Fatalf("payload %T", payload)
	}
	if res.GetBotId() != 8271796073 || res.GetMessageId() != 88 || res.GetVersion() != "1.2.0" {
		t.Fatalf("res = %+v", res)
	}
	if res.GetLocator() != "app_android_new_1.2.0_" {
		t.Fatalf("locator = %q", res.GetLocator())
	}
	if copier.dest != 55 || copier.srcChat != -1009900000001 || copier.srcMsg != 44 || copier.caption != "app_android_new_1.2.0_" {
		t.Fatalf("copy dest=%d src=%d/%d caption=%q", copier.dest, copier.srcChat, copier.srcMsg, copier.caption)
	}
}

type stubReleases struct {
	info api.AppReleaseInfo
	ok   bool
}

func (s stubReleases) AppReleaseInfo(_ context.Context, _, track string) (api.AppReleaseInfo, bool, error) {
	if track != api.AppReleaseStable {
		return api.AppReleaseInfo{}, false, nil
	}
	return s.info, s.ok, nil
}
