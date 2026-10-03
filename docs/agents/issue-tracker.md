# Issue tracker: Local Markdown (`backlog/`)

Tickets live as markdown files in `backlog/`. This is the project's existing
Planner/Coder/Tester/Reviewer board (`02-agile-process.md`); the Matt skills
plug into it instead of creating a second tracker.

## Conventions

- One ticket per file: `backlog/<TICKET-ID>.md` (e.g. `ING-050.md`). Match the
  prefix and next number already in use; look at the newest files first.
- Ticket body format is the existing one: `### [ID] Title`, the
  As a / I want / So that lines, then Gherkin acceptance criteria.
- Board state is the `**Status:**` line: `Backlog | Ready | In progress |
  Testing | Review | Done`. Do not invent other values there. Agents never set
  `Done`; Ali does.
- Triage state (from `/triage`) goes on a separate `**Triage:**` line, using
  the strings in `triage-labels.md`. It never replaces `**Status:**`.
- Comments and handoff notes append to the bottom of the ticket file, as the
  Coder/Tester/Reviewer roles already do.

## Specs

Numbered specs `00`-`07`, `06-decisions.md`, `agents/*.md` and `AGENTS.md` are
protected (see `AGENTS.md` section 3). Matt skills must not edit them.

- `/to-spec` writes drafts to `.scratch/<feature>/spec.md`. A draft is
  non-authoritative until Ali folds it into the numbered specs.
- ADRs from `/domain-modeling` go in `docs/adr/`, not `06-decisions.md`.

## When a skill says "publish to the issue tracker"

Create `backlog/<TICKET-ID>.md` in the existing ticket format with
`**Status:** Backlog`. Prefer the `planner-task` skill for ticket authoring
when working from an approved spec.

## When a skill says "fetch the relevant ticket"

Read `backlog/<TICKET-ID>.md`. The user will normally pass the ID.

## Wayfinding operations

Used by `/wayfinder`. Keep maps out of `backlog/` so the board holds only
buildable tickets.

- **Map**: `.scratch/<effort>/map.md` (Notes / Decisions-so-far / Fog).
- **Child ticket**: `.scratch/<effort>/issues/NN-<slug>.md`, numbered from
  `01`, question in the body. `Type:` line (`research`/`prototype`/
  `grilling`/`task`); `Status:` line `claimed`/`resolved`.
- **Blocking**: a `Blocked by: NN, NN` line near the top. Unblocked when every
  listed file is `resolved`.
- **Frontier**: files in `.scratch/<effort>/issues/` that are open, unblocked
  and unclaimed; lowest number first.
- **Claim**: set `Status: claimed` and save before any work.
- **Resolve**: append the answer under `## Answer`, set `Status: resolved`,
  then add a gist + link to the map's Decisions-so-far.

Note: `temp/` is this repo's scratch dir (`AGENTS.md` section 10);
`.scratch/` is for Matt-skill artifacts only. Add it to `.gitignore` if you do
not want them committed.
