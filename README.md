<p align="center">
  <img src="brand/sushi.png" width="220" alt="Sushi mark: a clay maki roll with a play-button core">
</p>

<h1 align="center">Sushi</h1>

<p align="center">
  A test and study project: how far can a media app go when Telegram is its only transport?
</p>

> [!IMPORTANT]
> **For testing and education.** Sushi is not a product or a service. See
> [Purpose and disclaimer](#purpose-and-disclaimer).

## What this project does

This is the backend half of an experiment. The client app talks to no HTTP API and no website. Every
request is a Telegram message to a bot, and every reply comes back the same way. A video is delivered
by copying a Telegram message into a chat the client can read, and the client plays it over MTProto.

The study questions are about engineering:

- Can a pool of Telegram bots stand in for an API, with dense protobuf envelopes instead of JSON?
- How do you design caching and sync around a budget of one message per user action?
- What does a clean Go service layout look like when the transport is someone else's chat network?

```text
app ──Telegram message──► API bot (apid) ──protobuf envelope──► app
app ──/play──► apid ──queue──► deliveryd ──copyMessage──► the user's chat ──MTProto──► player
```

The Flutter client is in [sushi-study-app](https://github.com/Aryan-mor/sushi-study-app), a fork of
[Fladder](https://github.com/DonutWare/Fladder).

**This repository is source code only.** It has no media, no catalog data, no releases and no tool
that finds or fetches media. People can add their own videos by sending them to the main bot.

## What's in the repository

| Path | What it is |
|------|------------|
| `be/cmd/main-bot` | Sign-up, admin panel, personal videos, news |
| `be/cmd/init-bot` | First contact and login from the app |
| `be/cmd/apid` | The API, served by a pool of bots |
| `be/cmd/catalog-bot` | Browse and search inside Telegram |
| `be/cmd/deliveryd` | Copies videos into the user's chat |
| `be/cmd/backupd` | Copies videos into backup channels |
| `db/` | Postgres migrations and queries |
| `proto/` | The wire schema |
| `tools/` | Pinned versions of `sqlc`, `buf` and `goose` |

Code comments mention design notes such as "ADR 0015" or "docs/05". Those notes are private and are
not part of this repository. Some database tables also remain from features that were removed, and
they stay empty.

## Setup

### 1. What you need

- Go 1.25 or newer
- Docker, or your own Postgres 16 with `wal_level=logical`
- `psql` and `bash`, for the catalog subscription script
- A [TMDB API key](https://www.themoviedb.org/settings/api), for titles and posters
- Telegram bots from [@BotFather](https://t.me/botfather), and private channels you create yourself

Every bot and channel name in this repository is an example, such as `exampleMainBot`. None of them
is a real account, so use your own.

### 2. Create the Telegram bots and channels

| Setting | What to create | Format |
|---------|----------------|--------|
| `MAIN_BOTS` | The bot people talk to | `username\|token` |
| `INIT_BOTS` | The bot the app contacts first | `username\|token` |
| `API_BOTS` | One or more bots for the API | `username\|token,username\|token` |
| `DELIVERY_BOTS` | One or more bots that send videos | `username\|token,username\|token` |
| `CATALOG_BOT` | The browse and search bot | `username\|token` |
| `BACKUP_BOTS` | One bot per backup channel | `username\|token\|channelId` |
| `LOGIN_POST_CHANNEL` | A public channel used during bot login | `username\|channelId` |
| `CHANNELS` | Your private storage channels | `clone\|-100…,backup1\|-100…,personal_clone\|-100…` |

Add each bot as an admin of the channels it uses. Turn on Bot-to-Bot mode in BotFather for the bots
that talk to each other.

### 3. Start and prepare Postgres

```bash
export POSTGRES_PASSWORD=choose-a-password
docker compose up -d
docker compose exec -T postgres psql -U sushi_app -d postgres -f - < db/bootstrap.sql
```

### 4. Run the migrations

```bash
LIB="postgres://sushi_app:$POSTGRES_PASSWORD@127.0.0.1:5434/sushi_library?sslmode=disable"
ENVDB="postgres://sushi_app:$POSTGRES_PASSWORD@127.0.0.1:5434/sushi_dev?sslmode=disable"

(cd tools/goose && go build -o goose github.com/pressly/goose/v3/cmd/goose)
tools/goose/goose -dir db/migrations/library postgres "$LIB" up
tools/goose/goose -dir db/migrations/env postgres "$ENVDB" up
```

### 5. Connect the catalog to the environment database

The environment database receives the catalog through logical replication. Run the script from
inside the container, so that both databases use an address the Postgres server can reach:

```bash
docker compose exec -T \
  -e DATABASE_URL="postgres://sushi_app:$POSTGRES_PASSWORD@127.0.0.1:5432/sushi_dev" \
  -e LIBRARY_URL="postgres://sushi_app:$POSTGRES_PASSWORD@127.0.0.1:5432/sushi_library" \
  postgres sh -c 'apk add -q bash && bash' < scripts/subscribe-catalog.sh
```

### 6. Configure

```bash
cp .env.example .env
```

Fill in `.env` using the table in step 2. Put your own numeric Telegram ID in `SUPERADMIN_IDS`, or you
will have no admin who can authorize anyone. Never commit `.env`.

### 7. Run the bots

Run each one in its own terminal, from the `be` folder:

```bash
cd be
export SUSHI_ENV_FILE=../.env
go run ./cmd/main-bot
go run ./cmd/init-bot
go run ./cmd/apid
go run ./cmd/deliveryd
go run ./cmd/catalog-bot
go run ./cmd/backupd
```

Each bot stops at startup and lists any setting it is missing. `be/Dockerfile` builds any of them into
an image with `--build-arg CMD=<name>`.

### 8. Check it

```bash
cd be
go test ./...
```

Send `/start` to your main bot. Then build the client by following the setup steps in
[sushi-study-app](https://github.com/Aryan-mor/sushi-study-app).

## Access modes

New users are closed by default. `SUSHI_ACCESS_MODE` decides who may register:

| Value | Behaviour |
|-------|-----------|
| `testing` (default, also used when unset) | No new users on any bot or in the app. A stranger who sends `/start` is told the app is not for public use and is shown their Telegram ID. |
| `public` | Anyone can register. |

Existing accounts keep working in both modes. In `testing` mode an admin can let a specific person in:

1. The person sends `/start` and receives their numeric Telegram ID.
2. The admin opens `/admin` → **Authorize users** → **Add a user** and replies with that ID.
3. The person sends `/start` again and registers normally.

An invite can be withdrawn from the same panel until it has been used.

## Purpose and disclaimer

- **Testing and education.** This is a personal study project. We use it to test ideas and to learn,
  and we run no business with it.
- **No responsibility.** If you use this code, for a personal project or a business, you do so entirely
  at your own risk. As the license states, it comes with no warranty and the authors accept no
  liability, to the extent the law allows.
- **No service and no support.** We offer no hosted service, no uptime and no help with running it.
- **Your content is your responsibility.** Follow the law where you live and the terms of every service
  involved, including Telegram's. Only use media you have the right to use.

## License

[GNU Affero General Public License v3.0](LICENSE). See [`NOTICE`](NOTICE).

In short, you may use this code for free, including in a business. If you change it and distribute it,
or let people use your changed version over a network, you must publish your source code under the
same license. The license text is what counts, not this summary.
