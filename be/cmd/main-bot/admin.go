package main

import (
	"context"
	"log/slog"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/app/identity"
)

func isAdmin(role string) bool {
	return role == identity.RoleAdmin || role == identity.RoleSuperadmin
}

func isSuperadmin(role string) bool {
	return role == identity.RoleSuperadmin
}

// openAdmin is /admin — the staff hub (the prototype-shaped), not the request list itself.
func (b *bot) openAdmin(ctx context.Context, s screen, user identity.User, locale string, log *slog.Logger) {
	if !isAdmin(user.Role) {
		b.show(ctx, s, unknown.pick(locale), homeKeyboardFor(locale, user.Role), log)
		return
	}
	b.showAdminMenu(ctx, s, locale, log)
}

func (b *bot) showAdminMenu(ctx context.Context, s screen, locale string, log *slog.Logger) {
	b.show(ctx, s, adminPanelTitle.pick(locale), adminKeyboardFor(locale), log)
}

func (b *bot) routeHubCallback(
	ctx context.Context, q *tg.CallbackQuery, s screen, user identity.User, locale string, log *slog.Logger,
) bool {
	if b.routeAdminCallback(ctx, q, s, user, locale, log) {
		return true
	}
	if b.routeAccessCallback(ctx, q, s, user, locale, log) {
		return true
	}
	if b.routeNewsDraftCallback(ctx, q, s, user, locale, log) {
		return true
	}
	if b.routeActionsCallback(ctx, q, s, user, locale, log) {
		return true
	}
	if b.routeIDsCallback(ctx, q, s, user, locale, log) {
		return true
	}
	if b.routeOnlineCallback(ctx, q, s, user, locale, log) {
		return true
	}
	return b.routeStatsCallback(ctx, q, s, user, locale, log)
}

// routeAdminCallback owns the hub button (m:adm).
func (b *bot) routeAdminCallback(
	ctx context.Context, q *tg.CallbackQuery, s screen, user identity.User, locale string, log *slog.Logger,
) bool {
	if q.Data != cbMenuAdmin {
		return false
	}
	if !isAdmin(user.Role) {
		b.toast(ctx, q, reqToastDenied.pick(locale))
		return true
	}
	b.toast(ctx, q, "")
	b.showAdminMenu(ctx, s, locale, log)
	return true
}

// adminKeyboardFor is the /admin root: Requests, a manual news draft, TMDB↔IMDb, and Actions for
// every admin; Home.
func adminKeyboardFor(locale string) tg.InlineKeyboard {
	rows := [][]tg.InlineButton{
		{{Text: reqPanelBtn.pick(locale), Data: cbMenuRequests}},
		{{Text: newsDraftBtn.pick(locale), Data: cbMenuNews}},
		{{Text: accessBtn.pick(locale), Data: cbMenuAccess}},
	}
	rows = append(
		rows,
		[]tg.InlineButton{{Text: idsBtn.pick(locale), Data: cbMenuIDs}},
		[]tg.InlineButton{{Text: actionsBtn.pick(locale), Data: cbMenuActions}},
		[]tg.InlineButton{{Text: reqBtnHome.pick(locale), Data: cbMenuHome}},
	)
	return tg.InlineKeyboard{Rows: rows}
}

func (b *bot) routeActionsCallback(
	ctx context.Context, q *tg.CallbackQuery, s screen, user identity.User, locale string, log *slog.Logger,
) bool {
	if q.Data != cbMenuActions {
		return false
	}
	if !isAdmin(user.Role) {
		b.toast(ctx, q, reqToastDenied.pick(locale))
		return true
	}
	b.toast(ctx, q, "")
	b.showActionsMenu(ctx, s, user.Role, locale, log)
	return true
}

func (b *bot) showActionsMenu(ctx context.Context, s screen, role, locale string, log *slog.Logger) {
	b.show(ctx, s, actionsTitle.pick(locale), actionsKeyboardFor(locale, role), log)
}

// actionsKeyboardFor is /admin → Actions. Online is every admin. Backup and daily stats are
// superadmin-only. Everyone gets back + Home.
func actionsKeyboardFor(locale, role string) tg.InlineKeyboard {
	rows := [][]tg.InlineButton{
		{{Text: onlineBtn.pick(locale), Data: cbMenuOnline}},
	}
	if isSuperadmin(role) {
		rows = append(rows, []tg.InlineButton{{Text: statsBtn.pick(locale), Data: cbMenuStats}})
	}
	rows = append(rows, []tg.InlineButton{
		{Text: adminBtnBack.pick(locale), Data: cbMenuAdmin},
		{Text: reqBtnHome.pick(locale), Data: cbMenuHome},
	})
	return tg.InlineKeyboard{Rows: rows}
}

// staffNavKeyboard is the footer on a staff panel: back to the hub, or out to Home.
func staffNavKeyboard(locale string) tg.InlineKeyboard {
	return tg.InlineKeyboard{Rows: [][]tg.InlineButton{{
		{Text: adminBtnBack.pick(locale), Data: cbMenuAdmin},
		{Text: reqBtnHome.pick(locale), Data: cbMenuHome},
	}}}
}

// actionsNavKeyboard is the footer after an Actions job (Backup): back to Actions, or Home.
func actionsNavKeyboard(locale string) tg.InlineKeyboard {
	return tg.InlineKeyboard{Rows: [][]tg.InlineButton{{
		{Text: actionsBtnBack.pick(locale), Data: cbMenuActions},
		{Text: reqBtnHome.pick(locale), Data: cbMenuHome},
	}}}
}
