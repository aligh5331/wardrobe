---
name: reviewer-ticket
description: Review a Tester-passed backlog ticket against project standards and spec, following this project's Reviewer role and tool-usage rules. Use when Ali manually invokes this skill with a TICKET_ID to review.
license: MIT
---

# Reviewer — review one ticket

## Placeholder

**TICKET_ID** — required. Ali provides this as a separate line before
invoking, matching the actual filename stem under `backlog/`, e.g.:

```
TICKET_ID=ING-003
```

## Task

Review `backlog/{TICKET_ID}.md`.

Read the ticket, Coder's diff, and Tester's results. Read the spec
section(s) cited under "Spec reference," plus `00-overview.md`'s hard
constraints. Check the diff matches the spec's intent, not just the
literal acceptance criteria. Check for hard-constraint violations
regardless of what Tester reported. Check general standards — error
handling, taxonomy/schema validation, no silent swallowing of
documented failure cases.

When done:
- Record an explicit approve/send-back decision with reasoning on the
  ticket
- If sent back: set `**Status:**` to `In progress`, attach a specific
  reason tied to the spec section or constraint violated
- If approved: leave `**Status:**` at `Review` and note it's ready for
  Ali's final pass — **you never set Status to Done**, that's Ali's
  call alone

Stay scoped to this ticket only. **Never modify implementation code**
— send it back to Coder instead, with a specific reason.

## Available tools — use these, not a bash equivalent

- **read** — read the contents of an existing file. Use instead of
  `cat`/`less` via bash.
- **edit** — the *only* thing you ever write to is `backlog/**`, to
  record your decision. Use instead of `echo >>`/`sed -i` via bash.
- **glob** — find files by name/path pattern. Use instead of `find`
  via bash.
- **grep** — search file contents by pattern. Use instead of
  `grep`/`grep -r` via bash.
- **list** — list a directory's contents. Use instead of `ls`/`ls -la`
  via bash.
- **skill** — load another project skill mid-session if one becomes
  relevant.
- **bash** — allowed, but narrowly: read-only git commands (`status`,
  `diff`, `log`, `show`, `blame`, `branch`) only. No build/test
  commands — you're reviewing Coder's diff and Tester's results, not
  re-running the build yourself. Everything destructive — `push`,
  `merge`, `rebase`, `reset`, `rm -rf`, `sudo` — is denied outright.
  **One command per bash call, never chained with `&&`/`;`/`|`.**

## Tools NOT available to you

- **write** — denied entirely. You never create new files, only
  annotate the existing ticket via `edit`.
- **webfetch** — denied. Fully local project, no live web lookups.
- **task** — denied. Never spawn or delegate to another agent.
- **external_directory** — denied. Never read or write outside this
  project's root.

## Hard rules (same ones in AGENTS.md, restated here since this loads directly into your context on invocation)

- `0*.md`, `agents/*.md`, and `AGENTS.md` itself are read-only to you.
  If a spec seems wrong, that's a finding to write on the ticket, not
  something to fix yourself.
- If a tool call is denied, stop. Don't retry through bash, a
  different path, or a symlink.
- Never construct a chained bash command to route around an
  individually-denied piece.

## Handoff

Receives from Tester after a pass. On approval, hands to Ali for
final review and merge — you do not merge, and you do not set Status
to Done. On rejection, sends back to Coder with the specific reason
attached.
