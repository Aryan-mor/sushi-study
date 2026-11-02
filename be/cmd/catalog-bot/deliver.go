package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/pg"
	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

const (
	ephemeralLifetime   = time.Minute
	bulkDeliverInterval = time.Second
)

func (b *bot) deliverFile(ctx context.Context, chatID, fileID int64, log *slog.Logger) {
	b.deliverFiles(ctx, chatID, catalog.Item{}, []int64{fileID}, false, log)
}

func (b *bot) deliverFiles(ctx context.Context, chatID int64, item catalog.Item, fileIDs []int64, bulk bool, log *slog.Logger) {
	b.announceBulk(ctx, chatID, fileIDs, bulk, log)
	sentIDs, last, item := b.copyAll(ctx, chatID, item, fileIDs, bulk, log)
	if len(sentIDs) == 0 {
		b.sendText(ctx, chatID, noCopy, homeKeyboard(), log)
		return
	}
	notice := deliveryNotice(item, last, bulk)
	if _, err := b.client.SendReplyHTML(ctx, chatID, sentIDs[len(sentIDs)-1], notice); err != nil {
		log.Debug("delivery notice", "err", err)
	}
	b.scheduleDeletes(ctx, chatID, sentIDs)
}

func (b *bot) announceBulk(ctx context.Context, chatID int64, fileIDs []int64, bulk bool, log *slog.Logger) {
	if !bulk || len(fileIDs) <= 1 {
		return
	}
	if _, err := b.client.SendKeyboard(ctx, chatID, fmt.Sprintf(sendingBulk, len(fileIDs)), tg.InlineKeyboard{}); err != nil {
		log.Debug("bulk sending", "err", err)
	}
}

func (b *bot) copyAll(
	ctx context.Context, chatID int64, item catalog.Item, fileIDs []int64, bulk bool, log *slog.Logger,
) (sentIDs []int64, last pg.FileOrigin, card catalog.Item) {
	card = item
	for i, id := range fileIDs {
		if id == 0 {
			continue
		}
		if bulk && i > 0 {
			time.Sleep(bulkDeliverInterval)
		}
		origin, ok, err := b.store.FileOrigin(ctx, id)
		if err != nil || !ok {
			log.Warn("file origin", "err", err, "file", id)
			continue
		}
		card = b.resolveDeliveryItem(ctx, card, origin)
		msgID, err := b.copyFromLocations(ctx, chatID, id, deliveryCaption(card, origin), log)
		if err != nil {
			log.Warn("copy", "err", err, "file", id)
			continue
		}
		sentIDs = append(sentIDs, msgID)
		last = origin
	}
	return sentIDs, last, card
}

func (b *bot) resolveDeliveryItem(ctx context.Context, item catalog.Item, origin pg.FileOrigin) catalog.Item {
	if item.TMDBID == 0 {
		loaded, found, err := b.store.Item(ctx, origin.TMDBID, origin.Kind)
		if err == nil && found {
			item = loaded
		} else {
			item = origin.ItemStub()
		}
	}
	return b.withIMDB(ctx, item)
}

func (b *bot) copyFromLocations(ctx context.Context, chatID, fileID int64, caption string, log *slog.Logger) (int64, error) {
	copies, err := b.store.FileCopies(ctx, fileID)
	if err != nil {
		return 0, err
	}
	var last error
	for _, cpy := range copies {
		id, err := b.client.CopyMessage(ctx, chatID, cpy.ChannelID, cpy.MessageID, caption)
		if err == nil {
			return id, nil
		}
		last = err
		log.Debug("copy try", "err", err, "role", cpy.Role, "file", fileID)
	}
	if last == nil {
		return 0, errors.New("no copies")
	}
	return 0, last
}

func (b *bot) scheduleDeletes(parent context.Context, chatID int64, messageIDs []int64) {
	ids := append([]int64(nil), messageIDs...)
	go func() {
		t := time.NewTimer(ephemeralLifetime)
		defer t.Stop()
		select {
		case <-t.C:
			ctx, cancel := context.WithTimeout(parent, 20*time.Second)
			defer cancel()
			for _, id := range ids {
				if err := b.client.DeleteMessage(ctx, chatID, id); err != nil {
					b.log.Debug("ephemeral delete", "err", err, "msg", id)
				}
			}
		case <-parent.Done():
		}
	}()
}
