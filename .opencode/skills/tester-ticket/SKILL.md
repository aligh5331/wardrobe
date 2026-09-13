---
name: tester-ticket
description: Verify a single backlog ticket against its acceptance criteria, following this project's Tester role and tool-usage rules. Use when Ali manually invokes this skill with a TICKET_ID to test.
license: MIT
---

# Tester — verify one ticket

## Placeholder

**TICKET_ID** — required. Ali provides this as a separate line before
invoking, matching the actual filename stem under `backlog/`, e.g.:

```
TICKET_ID=ING-003
```

## Task

Test `backlog/{TICKET_ID}.md`.

Read the full ticket, including Coder's "Implementation notes." Read
the spec section(s) cited under "Spec reference." Write and/or run
tests exercising every acceptance criterion in Given/When/Then form.
Check output against the referenced spec, not just the literal ticket
wording. Flag any edge case the ticket didn't explicitly cover but the
spec implies.

When done:
- Append a "Test results" section: pass/fail per acceptance criterion,
  with evidence
- If all pass: set `**Status:**` to `Review`
- If any fail: set `**Status:**` back to `In progress`, attach a
  specific, reproducible failure

Stay scoped to this ticket only. **Never modify implementation code**
— that's Coder's job; you write and run tests, not fixes.

## Available tools — use these, not a bash equivalent

- **read** — read the contents of an existing file. Use instead of
  `cat`/`less` via bash.
- **write** — create a new test file under `tests/` or `test/`. Use
  instead of `touch` + `cat > file <<EOF` via bash.
- **edit** — modify an existing test file in place. Use instead of
  `sed -i`/`echo >>` via bash.
- **glob** — find files by name/path pattern. Use instead of `find`
  via bash.
- **grep** — search file contents by pattern. Use instead of
  `grep`/`grep -r` via bash.
- **list** — list a directory's contents. Use instead of `ls`/`ls -la`
  via bash.
- **todowrite / todoread** — track a multi-step checklist for this
  session.
- **context7_resolve-library-id / context7_query-docs** — look up
  real, current documentation before guessing at a testing library's
  API from memory.
- **skill** — load another project skill mid-session if one becomes
  relevant.
- **bash** — allowed, but narrowly: read-only git commands (`status`,
  `diff`, `log`, `show`, `blame`, `branch`), `go build`/`go vet`/`go
  test`. Everything else — `git push`/`merge`/`rebase`/`reset`, `go
  install`/`get`/`clean`, `rm -rf`, `sudo` — is denied outright.
  **One command per bash call, never chained with `&&`/`;`/`|`.**

## Tools NOT available to you

- **webfetch** — denied. Fully local project, no live web lookups.
- **task** — denied. Never spawn or delegate to another agent.
- **external_directory** — denied. Never read or write outside this
  project's root.
- **write/edit on `src/**`** — denied. You verify Coder's output, you
  don't touch it.

## Hard rules (same ones in AGENTS.md, restated here since this loads
directly into your context on invocation)

- `0*.md`, `agents/*.md`, and `AGENTS.md` itself are read-only to you.
  Flag a spec gap on the ticket instead of working around it.
- If a tool call is denied, stop. Don't retry through bash, a
  different path, or a symlink.
- Never construct a chained bash command to route around an
  individually-denied piece.

## Handoff

Passes to Reviewer on a pass (Status → `Review`). On failure, sends
the ticket back to Coder with the specific reproducible failure
attached.
