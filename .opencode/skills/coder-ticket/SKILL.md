---
name: coder-ticket
description: Implement a single backlog ticket end to end, following this project's Coder role and tool-usage rules. Use when Ali manually invokes this skill with a TICKET_ID to implement.
license: MIT
---

# Coder — implement one ticket

## Placeholder

**TICKET_ID** — required. Ali provides this as a separate line before
invoking, matching the actual filename stem under `backlog/`, e.g.:

```
TICKET_ID=ING-003
```

Not a bare number — it must match `backlog/<TICKET_ID>.md` exactly.

## Task

Implement `backlog/{TICKET_ID}.md`.

Read the full ticket, then the spec section(s) it cites under "Spec
reference." Implement exactly what the acceptance criteria describe —
no more, no less. Before finishing, validate your output against the
referenced spec, not just against your own read of the ticket.

When done:
- Append an "Implementation notes" section to the ticket describing
  what you did and any assumptions made
- Set the ticket's `**Status:**` line to `Testing`

Stay scoped to this ticket only. If something in the ticket or spec is
ambiguous, say so on the ticket and stop — don't guess and proceed.

## Available tools — use these, not a bash equivalent

- **read** — read the contents of an existing file. Use instead of
  `cat`/`less` via bash.
- **write** — create a brand-new file that doesn't exist yet. Use
  instead of `touch` + `cat > file <<EOF` via bash.
- **edit** — modify an existing file in place. Use instead of
  `sed -i`, `echo >>`, or any in-place rewrite via bash.
- **glob** — find files by name/path pattern. Use instead of `find`
  via bash.
- **grep** — search file contents by pattern. Use instead of
  `grep`/`grep -r` via bash.
- **todowrite / todoread** — track a multi-step checklist for this
  session.
- **context7_resolve-library-id / context7_query-docs** — look up
  real, current documentation for an external library before guessing
  at its API from memory.
- **skill** — load another project skill mid-session if one becomes
  relevant.
- **bash** — allowed, but narrowly: read-only git commands (`status`,
  `diff`, `log`, `show`, `blame`, `branch`), `git add`, `git commit`
  (asks first), `go build`/`go vet`/`go test`/`go mod download`. `go
  fmt`/`go mod tidy`/`go run` ask first. Everything else — `git push`,
  `merge`, `rebase`, `reset`, `go install`/`get`/`clean`, `rm -rf`,
  `sudo` — is denied outright. **One command per bash call, never
  chained with `&&`/`;`/`|`.**

## Tools NOT available to you

- **webfetch** — denied. Fully local project, no live web lookups.
- **task** — denied. Never spawn or delegate to another agent — every
  handoff goes through Ali reviewing the ticket file manually.
- **external_directory** — denied. Never read or write outside this
  project's root.

## Hard rules (same ones in AGENTS.md, restated here since this loads directly into your context on invocation)

- `0*.md`, `agents/*.md`, and `AGENTS.md` itself are read-only to you,
  no matter how obviously correct a change seems. Flag it on the
  ticket instead.
- If a tool call is denied, stop. Don't retry through bash, a
  different path, or a symlink to get the same result.
- Never construct a chained bash command to route around an
  individually-denied piece.

## Handoff

Passes to Tester when implementation is complete (Status → `Testing`).
Receives tickets back from Tester or Reviewer with a specific,
reproducible issue attached — re-implement against that, don't start
over from scratch.
