package main

import (
	"context"
	"fmt"
	"strconv"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/contentnotify"
)

// dmSender adapts *tg.Client to contentnotify.Messenger — the reconciler never needs the sent
// message back, only whether it went.
type dmSender struct{ client *tg.Client }

func (d dmSender) SendHTML(ctx context.Context, chatID int64, text string) error {
	_, err := d.client.SendHTML(ctx, chatID, text)
	return err
}

func (d dmSender) SendPhotoHTML(ctx context.Context, chatID int64, photoURL, caption string) error {
	_, err := d.client.SendPhotoHTML(ctx, chatID, photoURL, caption)
	return err
}

func (d dmSender) PostAlert(ctx context.Context, p contentnotify.AlertPost) (int64, error) {
	kb := requestAlertKeyboard(p.RequestID, p.Locale)
	if p.PhotoURL != "" {
		msg, err := d.sendAlertPhoto(ctx, p, kb)
		if err == nil {
			return msg.ID, nil
		}
	}
	msg, err := d.sendAlertText(ctx, p, kb)
	if err != nil {
		return 0, err
	}
	return msg.ID, nil
}

func (d dmSender) sendAlertPhoto(ctx context.Context, p contentnotify.AlertPost, kb tg.InlineKeyboard) (tg.Message, error) {
	if p.ThreadID > 0 {
		return d.client.SendPhotoKeyboardThread(ctx, p.ChatID, p.ThreadID, p.PhotoURL, p.Caption, kb)
	}
	return d.client.SendPhotoKeyboard(ctx, p.ChatID, p.PhotoURL, p.Caption, kb)
}

func (d dmSender) sendAlertText(ctx context.Context, p contentnotify.AlertPost, kb tg.InlineKeyboard) (tg.Message, error) {
	if p.ThreadID > 0 {
		return d.client.SendKeyboardThread(ctx, p.ChatID, p.ThreadID, p.Caption, kb)
	}
	return d.client.SendKeyboard(ctx, p.ChatID, p.Caption, kb)
}

func (d dmSender) CloseAlert(ctx context.Context, chatID, messageID int64, outcome string, threadID int64) error {
	if threadID > 0 {
		return d.client.MoveToForumTopic(ctx, chatID, messageID, threadID, outcome)
	}
	empty := tg.InlineKeyboard{Rows: [][]tg.InlineButton{}}
	if err := d.client.EditCaptionKeyboard(ctx, chatID, messageID, outcome, empty); err == nil {
		return nil
	}
	return d.client.EditKeyboard(ctx, chatID, messageID, outcome, empty)
}

func registerMenus(ctx context.Context, client *tg.Client) error {
	for _, locale := range []string{"", "en", "fa"} {
		lang := locale
		if lang == "" {
			lang = "en"
		}
		if err := client.SetMyCommands(ctx, menuCommands(lang), locale); err != nil {
			return fmt.Errorf("setMyCommands %q: %w", locale, err)
		}
	}
	return nil
}

func requestAlertKeyboard(id int64, locale string) tg.InlineKeyboard {
	return tg.InlineKeyboard{Rows: [][]tg.InlineButton{{
		{Text: reqBtnReject.pick(locale), Data: cbReqAlertRejectPrefix + strconv.FormatInt(id, 10)},
		{Text: reqBtnRecheck.pick(locale), Data: cbReqAlertRecheckPrefix + strconv.FormatInt(id, 10)},
	}, {}}}
}
