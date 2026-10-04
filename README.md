# Wardrobe

A personal wardrobe app that runs on your own machine. Photograph a garment and
a local vision model tags it (category, colors, pattern, warmth, formality).
Then ask for outfit ideas: the app checks today's weather and has a local
language model pick three color-coordinated outfits from your catalog.

- **Catalog**: one photo per garment in, a tagged catalog item out. You review
  and correct the tags before anything is saved.
- **Weather**: current conditions and today's forecast for a city you choose.
- **Outfit suggestions**: three outfits for today, filtered by the weather and
  an optional formality, each with a one-line reason.

Everything runs locally. Your photos and catalog stay in a SQLite file and a
folder on your disk, and photo tagging goes to a model server you run
yourself. The outfit-picking LLM is local by default; you can point `LLM_URL`
at a hosted endpoint instead, which then receives the weather, your note and
the candidate garments' tags (never photos or your location). The only other
outside service is [Open-Meteo](https://open-meteo.com/) for weather, which
needs no API key and is called from the backend only.

---

## Contents

- [How it works](#how-it-works)
- [Requirements](#requirements)
- [Install a release](#install-a-release)
- [Run with Docker](#run-with-docker)
- [Quick start](#quick-start)
- [Setting up the models](#setting-up-the-models)
- [Using the app](#using-the-app)
- [Bulk import from the command line](#bulk-import-from-the-command-line)
- [Configuration](#configuration)
- [How outfits are chosen](#how-outfits-are-chosen)
- [Your data](#your-data)
- [Troubleshooting](#troubleshooting)
- [Development](#development)
- [Project layout](#project-layout)
- [Specs and contributing](#specs-and-contributing)
- [License](#license)

---

## How it works

```
                    ┌────────────────────────────────────────────┐
  browser  ───────▶ │  wardrobe server (one Go binary)           │
  localhost:8080    │  • React UI (embedded)                     │
                    │  • REST API (/api/...)                     │
                    │  • SQLite: data/wardrobe.db                │
                    │  • photos:  data/photos/                   │
                    └──────┬──────────────┬──────────────┬───────┘
                           │              │              │
                 tag photo │   pick       │   forecast   │
                           ▼   outfits    ▼              ▼
                   VLM server       LLM server       Open-Meteo
                   (VLM_URL)        (LLM_URL)        (internet)
                   e.g. Qwen3-VL    any local text
                   via llama.cpp    model
```

- **Backend:** Go, with Gin, GORM, and SQLite. The SQLite driver is pure Go,
  so you don't need a C compiler.
- **Frontend:** React, Vite, and Tailwind v4. The built UI is embedded into
  the Go binary, so one executable serves both the UI and the API.
- **Models:** two model servers, both OpenAI-compatible
  (`/v1/chat/completions`). A vision model tags photos and a text model picks
  outfits. They can be the same server if one model handles both jobs.

---

## Requirements

| What | Version / notes |
|---|---|
| [Go](https://go.dev/dl/) | 1.26.6 or newer (from `go.mod`). With Go 1.21 or newer installed, `go` downloads the right toolchain for you. |
| [Node.js](https://nodejs.org/) and npm | Node 22.12 or newer. Only needed to build the UI. |
| A vision-language model server | OpenAI-compatible `/v1/chat/completions` that accepts images. The project is built around **Qwen3-VL-8B (Q4)** on **llama.cpp** `llama-server`. |
| A text LLM server | Any OpenAI-compatible `/v1/chat/completions`, such as llama.cpp or Ollama. |
| Internet access | Only for Open-Meteo (weather and outfit suggestions). The catalog works offline. |

The app runs on Linux, macOS, and Windows.

---

## Install a release

Prebuilt archives for Linux, macOS and Windows are on the
[Releases page](https://github.com/aligh5331/wardrobe/releases). Each one
holds the `wardrobe` server, the `ingest` CLI, this README, the license and
`.env.example`. The UI is already built in; you need neither Go nor Node.

1. Download and unpack the archive for your system.
2. In the unpacked folder, copy `.env.example` to `.env` and set `VLM_URL` and
   `LLM_URL` ([Setting up the models](#setting-up-the-models)).
3. Run `./wardrobe` (Windows: `.\wardrobe.exe`) from that folder and open
   **http://localhost:8080**.

`data/` and `logs/` are created next to the binary's working directory. The
binaries are unsigned: on macOS, clear the quarantine flag once with
`xattr -d com.apple.quarantine wardrobe ingest`.

Releases use `v0.x` versions: later versions add features and may change how
things work. Your catalog carries forward; the database schema migrates
itself on startup.

---

## Run with Docker

Images for `linux/amd64` and `linux/arm64` are published to
`ghcr.io/aligh5331/wardrobe` (tags `latest` and each version, such as
`0.1.0`). The image runs the server only; your VLM and LLM servers run
outside it.

```bash
git clone https://github.com/aligh5331/wardrobe.git
cd wardrobe
cp .env.example .env    # set VLM_URL and LLM_URL
docker compose up -d
```

Open **http://localhost:8080**.

- **Model servers on the same machine:** inside the container, `localhost` is
  the container. Use `http://host.docker.internal:<port>` in `.env`.
- **Port:** `docker-compose.yml` publishes on `127.0.0.1` only, because the
  app has no login. Change it only if you know who can reach the port.
- **Data:** `./data` and `./logs` are mounted into the container. The
  container runs as a non-root user (uid 65532). On Linux, if it cannot write
  them, run `sudo chown -R 65532:65532 data logs` once.
- **Bulk import:** put photos under `./data`, then
  `docker compose run --rm --entrypoint ingest wardrobe /app/data/<photo>`.

---

## Quick start

Build from source. You need Go and Node (see [Requirements](#requirements)).

```bash
# 1. Clone
git clone https://github.com/aligh5331/wardrobe.git
cd wardrobe

# 2. Configure: copy the template, then set VLM_URL and LLM_URL
cp .env.example .env

# 3. Build the UI (writes frontend/dist/, which gets embedded into the binary)
cd frontend
npm ci
npm run build
cd ..

# 4. Build and run the server from the repo root
go build -o wardrobe ./cmd/server
./wardrobe                      # Windows: go build -o wardrobe.exe ./cmd/server, then .\wardrobe.exe
```

Open **http://localhost:8080**.

Notes:

- **Run it from the repo root.** `.env`, `data/`, and `logs/` are all resolved
  relative to the current working directory. `data/` and `logs/` are created
  on first run.
- **Changing the port or interface:** `./wardrobe -addr 127.0.0.1:9000`. The
  default `:8080` listens on all network interfaces. The app has no login, so
  use `127.0.0.1:<port>` if other devices on your network shouldn't reach it.
- **Rebuild after UI changes.** The UI is embedded at `go build` time, so
  after `npm run build` you also need to run `go build` again.
- If you skip step 3, the page says *"Frontend not built"*. Build the UI, then
  rebuild the server.
- You can skip the separate build with `go run ./cmd/server`.
- To quiet Gin's debug route listing at startup, set `GIN_MODE=release`.

> **Keeping `git status` clean:** `frontend/dist/index.html` is a tracked
> placeholder, so that a fresh checkout compiles before the UI is built.
> `npm run build` overwrites it. To stop git from showing it as modified, run
> this once:
>
> ```bash
> git update-index --skip-worktree frontend/dist/index.html
> ```
>
> Don't commit the built `index.html`. It points to hashed asset files that
> aren't in git.

---

## Setting up the models

The app doesn't download or run models itself. It calls the two URLs you set
in `.env`. Point `VLM_URL` and `LLM_URL` at the server's base address, without
`/v1`. The app adds `/v1/chat/completions` itself.

### Vision model (tagging): llama.cpp + Qwen3-VL-8B

Download a Qwen3-VL-8B GGUF (Q4 quantization) **and** its matching `mmproj`
file, then start `llama-server`:

```bash
llama-server \
  -m  /path/to/Qwen3-VL-8B-Instruct-Q4_K_M.gguf \
  --mmproj /path/to/mmproj-Qwen3-VL-8B-Instruct-F16.gguf \
  --host 127.0.0.1 --port 8081
```

```dotenv
VLM_URL=http://127.0.0.1:8081
```

The VLM only needs to run while you're adding garments. If your server
mishandles parallel requests (for example
[llama.cpp #17200](https://github.com/ggml-org/llama.cpp/issues/17200) with
Qwen3-VL), set `VLM_SERIALIZE_REQUESTS=true`. You can also set
`VLM_REQUEST_DELAY_MS`.

### Text model (outfit suggestions)

Any OpenAI-compatible chat server works. Two examples:

```dotenv
# llama.cpp llama-server: leave LLM_MODEL empty
LLM_URL=http://127.0.0.1:8082
LLM_MODEL=

# Ollama: the model name is required
LLM_URL=http://127.0.0.1:11434
LLM_MODEL=qwen3:8b
```

The server can't start without `LLM_URL`. The text model is only called when
you click **Suggest outfits**. The app always sends `"stream": false` and
expects a single JSON reply, so streaming-only (SSE) gateways aren't supported.

---

## Using the app

The page has two sections: **Today** (weather and outfit suggestions) and
**Your wardrobe** (the catalog).

### Add a garment

1. Click **Add garment** and choose a photo. Use one garment per photo, laid
   flat or on a hanger. Accepted formats are `.jpg`, `.jpeg`, `.png`, and
   `.webp`, up to 20 MiB.
2. Click **Upload & tag**. The local vision model suggests the tags.
3. Review the draft and fix any field you disagree with. You can also add
   free-text notes.
4. Click **Save**. Nothing is added to the catalog until you do. **Cancel**
   closes the form without saving.

If the model returns invalid output twice in a row, you'll see an error and
can retry or pick another photo.

### Edit a garment

Click **Edit** on any item to change its tags or notes. The photo, id, and
date added can't be changed. Items can't be deleted from the UI yet.

### Weather

The weather panel shows the current temperature, feels-like temperature, the
conditions, today's high and low, and the chance of rain. The default location
is **Tehran**. To use your own city, click **Change location**, search for it,
and pick a result. The choice is saved in the database. A value Open-Meteo
doesn't have is shown as `-`.

### Outfit suggestions

1. Optionally pick a **Formality** (`any`, `casual`, `smart-casual`, `formal`).
2. Optionally write a **Note**, up to 500 characters. For example: *"dinner
   with friends, walking there"*.
3. Click **Suggest outfits**.

You get three outfits, shown as rows of garment photos, each with a short
reason. Every outfit has a top, a bottom, and footwear. It also has outerwear
when it's cold, and may include headwear or accessories. If your catalog can't
fill a required slot for today's weather and formality, the app tells you
which slot is missing instead of calling the model.

---

## Bulk import from the command line

To catalog many photos without the UI, use the `ingest` CLI. It tags each
photo with the same VLM and validation as the UI, but saves **without** a
review step.

```bash
go build -o ingest ./cmd/ingest

./ingest path/to/shirt.jpg                 # one photo
./ingest path/to/photo-folder/             # every .jpg/.jpeg/.png/.webp in the folder (not recursive)
./ingest a.jpg b.png more-photos/          # any mix
```

- Run it from the repo root, so it uses the same `.env` and `data/` as the
  server. `LLM_URL` isn't required for `ingest`.
- **stdout** gets one JSON object per photo, so you can pipe it to `jq` or a
  file:

  ```json
  {"item_id":"…","photo_path":"…","category":"top","subcategory":"shirt","dominant_color":"navy","secondary_colors":["white"],"pattern":"striped","warmth_tier":"light","formality":"smart-casual","flagged":false}
  ```

- The photo is **copied** into `data/photos/`. Your original is never
  modified.
- If the model gives invalid output twice, the photo is marked
  `"flagged": true` and **not saved**. Add it through the UI instead, where
  you can fix the tags by hand.
- Logs and errors go to stderr and `logs/app.log`. Ingest stops at the first
  hard error, such as an unreachable VLM.

---

## Configuration

All settings come from environment variables. `.env` in the working directory
is loaded automatically; see `.env.example` for a commented template. Values
already set in the shell win over `.env`.

| Variable | Required | Default | Purpose |
|---|---|---|---|
| `VLM_URL` | **yes** | – | Base URL of the vision model server, e.g. `http://127.0.0.1:8081`. Include `http://` or `https://`. |
| `VLM_API_KEY` | no | empty | Sent as a bearer token when set. |
| `VLM_TEMPERATURE` | no | `0.4` | Sampling temperature for tagging, `0.0` to `1.0`. Kept above zero so the one retry gives a different answer. |
| `VLM_SERIALIZE_REQUESTS` | no | `false` | `true` sends VLM requests one at a time, for servers that break under parallel load. |
| `VLM_REQUEST_DELAY_MS` | no | `0` | Pause after each VLM response. Only used when `VLM_SERIALIZE_REQUESTS=true`. Otherwise you get a startup warning. |
| `LLM_URL` | **server only** | – | Base URL of the text model server. `ingest` doesn't need it. |
| `LLM_API_KEY` | no | empty | Sent as a bearer token when set. |
| `LLM_MODEL` | no | empty | Model name sent with each request. Leave empty for llama.cpp. Set it for servers that require one, such as Ollama. |
| `LLM_TEMPERATURE` | no | `0.4` | Sampling temperature for outfit picks, `0.0` to `1.0`. |
| `LOG_LEVEL` | no | `info` | `debug`, `info`, `warn`, or `error`. |
| `LOG_FORMAT` | no | `text` | `text` or `json`. |

An invalid value, such as `VLM_TEMPERATURE=2` or `LOG_LEVEL=verbose`, stops
startup with an error naming the variable.

The server's only command-line flag is `-addr`, the listen address
(default `:8080`).

---

## How outfits are chosen

Suggestions happen in two steps:

1. **A rule filter** picks candidate garments based on today's weather and,
   if you chose one, the formality.
2. **The local LLM** sees only those candidates' tags (no photos, no notes),
   the weather, and your note. It picks three outfits, mainly for color
   coordination and to fit your note.

The weather rule is based on the feels-like temperature:

| Feels like | Allowed warmth tiers (top, bottom, outerwear, footwear) | Outerwear |
|---|---|---|
| below 10 °C | `medium`, `heavy` | required |
| 10–20 °C | `light`, `medium` | optional |
| above 20 °C | `light` | not allowed |

- If today's low-to-high range crosses a threshold, the tiers from both bands
  are allowed, so you can layer. The outerwear rule still follows the
  feels-like band.
- Headwear and accessories are never filtered by warmth.
- A chance of rain of 50% or more is passed to the model as a hint, not as a
  hard rule.
- If the temperature is unknown, no warmth filter is applied.

The model's answer is checked: three outfits, only candidate items, one top,
one bottom, one footwear, and correct outerwear for the weather. If the answer
is invalid, the model is asked once more. If the second answer is also
invalid, you get an error.

### Tag vocabulary

Tags come from a fixed list (`03-taxonomy.md`):

| Field | Values |
|---|---|
| Category: subcategories | **top**: t-shirt, polo, shirt, sweater, hoodie, sweatshirt, tank-top · **bottom**: jeans, chinos, dress-pants, shorts, sweatpants · **outerwear**: jacket, coat, blazer, vest · **footwear**: sneakers, boots, dress-shoes, sandals, loafers · **headwear**: cap, beanie, hat · **accessory**: belt, scarf, tie, bag, watch, sunglasses, gloves |
| Colors | black, white, gray, navy, blue, red, green, olive, brown, tan, beige, burgundy, pink, purple, yellow, orange |
| Pattern | solid, striped, plaid, print |
| Warmth tier | light, medium, heavy |
| Formality | casual, smart-casual, formal |

Each item has one dominant color and zero or more secondary colors.

---

## Your data

Everything personal is kept under directories that git ignores:

```
data/
├── wardrobe.db          # SQLite catalog and saved weather location
├── photos/              # your garment photos, named <item-id>.<ext>
└── ingest-staging/      # temporary UI uploads; a cancelled draft's file can be left here
logs/
├── app.log              # app log: requests, startup, LLM and weather calls (not rotated)
└── vlm-attempts.jsonl   # every VLM tagging attempt, including raw model output
.env                     # your config and any API keys
```

- **Back up** by copying the `data/` folder while the server is stopped.
- **Start fresh** by stopping the server and deleting `data/`.
- `logs/app.log` grows without limit. Delete or trim it whenever you like.
- Files left in `data/ingest-staging/` by cancelled uploads are safe to delete
  while the server is stopped.
- Never commit `data/`, `logs/`, or `.env`.

---

## Troubleshooting

| Symptom | Likely cause / fix |
|---|---|
| `startup: missing required environment variable: VLM_URL` or `LLM_URL` | `.env` is missing, or you're not running from the repo root. Copy `.env.example` to `.env` and fill it in. |
| Page says *"Frontend not built"* | Run `npm ci && npm run build` in `frontend/`, then `go build` again. |
| Tagging fails with "VLM unreachable" | The VLM server isn't running, or `VLM_URL` is wrong. It needs `http://`, and no `/v1` suffix. |
| Tagging keeps failing or hangs with llama.cpp + Qwen3-VL | Set `VLM_SERIALIZE_REQUESTS=true`. You can add `VLM_REQUEST_DELAY_MS=15`. |
| Outfit request returns a 502 that says to set `LLM_MODEL` | Your LLM server needs a model name. Set `LLM_MODEL`, e.g. the Ollama tag. |
| Outfit request returns a 502 about invalid JSON | The model's reply wasn't usable twice in a row. Try again, try a stronger model, or check that the server isn't streaming. |
| *"cannot build outfits, missing: …"* when suggesting outfits | No garment in your catalog fits a required slot (top, bottom, footwear, or outerwear in the cold) for today's weather and formality. Add more items or choose `any` formality. |
| Weather panel shows an error | The backend can't reach Open-Meteo. Check your internet connection. The catalog keeps working. |
| `listen tcp :8080: bind: address already in use` | Use another port: `./wardrobe -addr :9000`. |

Each API response has an `X-Request-ID` header. Search `logs/app.log` for that
id to find the matching log lines. For more detail, set `LOG_LEVEL=debug`.

---

## Development

### Run the UI with hot reload

```bash
# terminal 1: API server (from the repo root)
go run ./cmd/server

# terminal 2: Vite dev server, proxies /api to localhost:8080
cd frontend
npm run dev
```

Open the URL Vite prints, usually http://localhost:5173.

### Tests

```bash
# Go unit and acceptance tests
go test ./...

# Go process tests: compile and boot the real binary (slower)
go test -tags=integration ./tests/...

# Live tagging test against a real VLM. Put a sample garment photo in temp/ first.
VLM_URL=http://127.0.0.1:8081 go test -tags=integration ./tests/ -run ING004

# Frontend tests (Vitest + Testing Library + jsdom)
cd frontend
npm test
```

The default Go tests and the frontend tests never call a real model or
Open-Meteo.

### API overview

All routes are local and have no authentication.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/items` | List the catalog |
| `GET` | `/api/items/:id` | One item |
| `PUT` | `/api/items/:id` | Update tags and notes |
| `POST` | `/api/items/photo` | Upload a photo (multipart) and get a tagged draft. Nothing is saved yet. |
| `POST` | `/api/items` | Save a confirmed draft |
| `GET` | `/api/photos/:filename` | Serve a stored photo |
| `GET` | `/api/taxonomy` | The allowed tag values |
| `GET` | `/api/weather` | Current conditions and today's forecast |
| `GET` | `/api/weather/cities?q=` | City search |
| `GET` / `PUT` | `/api/weather/location` | Read or save the weather location |
| `POST` | `/api/recommendations` | `{"formality"?, "note"?}` → 3 outfits |

`07-architecture.md` has the full request and response shapes and the error
codes.

---

## Project layout

```
cmd/
  server/          the web server (API and embedded UI)
  ingest/          the bulk-import CLI
internal/
  api/             HTTP handlers and the access log
  catalog/         saves a photo and its catalog row (shared by the CLI and the API)
  config/          environment variable loading and validation
  logging/         slog setup (stderr and logs/app.log)
  recommend/       weather → warmth rules, candidate filter, LLM outfit picker
  store/           GORM models and SQLite access
  tagging/         VLM client, prompt, output validation, retry policy
  weather/         Open-Meteo forecast and geocoding client
frontend/          React + Vite + Tailwind UI (embed.go embeds dist/)
tests/             Go acceptance tests, one file per backlog ticket
backlog/           tickets (ING-xxx.md)
00–07-*.md         project specs (see below)
```

---

## Specs and contributing

This project is built spec-first, with AI coding agents working through a
ticket backlog. If you plan to change code, read these first:

| File | What it covers |
|---|---|
| `00-overview.md` | Vision, current phase, hard constraints |
| `03-taxonomy.md` | The fixed tag vocabulary |
| `04-data-schema.md` | Catalog item and settings fields |
| `05-vlm-tagging-spec.md` | Tagging prompt, model, output validation |
| `06-decisions.md` | Decision log: why things are the way they are |
| `07-architecture.md` | Stack, API contract, env vars, logging |
| `01-agentic-workflow.md`, `02-agile-process.md` | Planner, Coder, Tester, and Reviewer roles; ticket format; branch workflow |
| `AGENTS.md` | Rules for every agent working in the repo |
| `later-ideas.md` | Parked ideas. Not planned and not authoritative. |

Ground rules:

- **Stay local.** No cloud inference, hosted model APIs, or fine-tuning.
  Two scoped exceptions: Open-Meteo for weather, and a hosted `LLM_URL` when
  the owner chooses one (`06-decisions.md`).
- **Don't invent tag values.** Changes to the taxonomy go through
  `03-taxonomy.md` and its dependent specs together.
- **Keep personal data out of git**: `data/`, `logs/`, `.env`, and real
  photos.
- Work happens on `sprint/<slug>` branches, one ticket per commit where
  practical. See `02-agile-process.md`.

Issues are welcome. Pull requests are not expected: changes go through the
spec → ticket → agent process above.

---

## License

[Apache License 2.0](LICENSE).
