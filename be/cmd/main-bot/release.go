package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Aryan-mor/sushi-study/be/internal/platform/config"
)

// deployNoticeService is main-bot's row in deploy_notices. One row per service, so main-bot
// claiming a tag does not silence another daemon that later wants the same announcement.
const deployNoticeService = "main-bot"

type deployNoticeShelf interface {
	ClaimDeployNotice(ctx context.Context, service, tag string) (fresh bool, err error)
}

// adminNotifier is the one thing announceDeploy needs to reach a superadmin. Declared here rather
// than imported from opsnotify, so this file does not care how the DM is sent (R-GO-5).
type adminNotifier interface {
	NotifyAdmins(ctx context.Context, text string)
}

// announceDeploy DMs the superadmins that a new release is live (docs/02 §7).
//
// Boot is the event: a deploy is a new image starting, and SUSHI_IMAGE_TAG is the git sha
// the deploy script wrote into /run/sushi.env beside the image it loaded (ADR 0016). The
// tag is claimed in Postgres first, so a restart on the same image -- an OOM kill, a `compose up`
// with nothing new -- says nothing.
//
// Best-effort throughout: a superadmin who cannot be reached, or a notices table that is unhappy,
// must not stop main-bot from serving logins.
func announceDeploy(
	ctx context.Context, env config.Env, tag string,
	notices deployNoticeShelf, notify adminNotifier, log *slog.Logger,
) {
	if tag == "" {
		return // dev checkout: no image was deployed, so there is no release to announce
	}
	fresh, err := notices.ClaimDeployNotice(ctx, deployNoticeService, tag)
	if err != nil {
		log.Warn("deploy notice claim failed", "tag", tag, "err", err)
		return
	}
	if !fresh {
		log.Info("release already announced", "tag", tag)
		return
	}
	log.Info("announcing release", "tag", tag, "admins_env", env)
	notify.NotifyAdmins(ctx, deployNotice(env, tag))
}

// deployNotice is the message itself. Plain text, because opsnotify sends with no parse mode, and
// English only: this is a staff DM, not reader-facing prose, and the ops alerts in
// internal/app/health read the same way.
func deployNotice(env config.Env, tag string) string {
	return fmt.Sprintf("🚀 New release deployed to %s.\nmain-bot is now running %s.", env, tag)
}
