# Agile process

Lightweight — this is a one-person-plus-agents project, not a team of ten.
Keep the ceremony that earns its keep and drop the rest.

## Backlog item format
```
### [TICKET-ID] Short title

**As a** (role, usually "wardrobe owner")
**I want** (capability)
**So that** (benefit)

**Acceptance criteria** (Gherkin):
Given <context>
When <action>
Then <expected result>

**Spec reference:** link to the relevant section of 03/04/05
```

## Backlog storage
One file per ticket, at `backlog/<TICKET-ID>.md`, containing the format
above plus a status line:

```
**Status:** Backlog | Ready | In progress | Testing | Review | Done
```

The board column (below) is this `Status` line, not a directory — a
ticket is edited in place, never moved between folders. This keeps every
agent's filesystem permission for the backlog a single `backlog/**` glob
instead of a moving target, and keeps each ticket's full history in one
git-trackable file rather than scattered across column directories.

## Board columns
`Backlog -> Ready -> In progress -> Testing -> Review -> Done`

- **Ready** means Definition of Ready is met (below) — don't let Planner push
  a ticket to Ready without it.
- **Testing** and **Review** are separate columns because Tester and
  Reviewer are separate agent roles with separate jobs — don't collapse them.

## Definition of ready
A ticket can move to Ready only if:
- Acceptance criteria are written in Given/When/Then form
- It references a specific section of an approved spec file
- It's small enough for one Coder pass (if not, Planner splits it)

## Definition of done
A ticket can move to Done only if:
- All acceptance criteria pass (Tester confirmed)
- Reviewer approved against project standards and the spec
- Ali did final review

## Sprint cadence
No fixed calendar sprint — a "sprint" here is one coherent slice of a layer
(e.g. "ingestion: photo → tagged JSON" is one sprint; "ingestion: JSON →
DB row + grid UI" is the next). Close a sprint by actually using the result
before starting the next one — that's the checkpoint that catches schema
gaps early (see `00-overview.md` phase note).

## Git workflow
Each sprint gets its own branch off `main`:

```
sprint/<slug>          e.g. sprint/ingestion-photo-to-json
```

- Ali creates and merges sprint branches — that's an infra/tooling
  decision, not an agent one (`00-overview.md` roles table).
- Coder commits to the current sprint branch as tickets complete, one
  commit per ticket where practical.
- Agents never create branches, push, merge, or force-push. Those stay
  manual/Ali-only until there's a specific reason to automate them —
  enforced structurally via each agent's `permissions` block in
  `.opencode/agents/`.
- A sprint branch merges to `main` only once the sprint is actually
  closed per the cadence above — every ticket's Definition of Done met,
  and the result used for real before starting the next sprint.
