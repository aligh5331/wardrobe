# AGENTS.md

Cross-cutting instructions for every agent session in this project —
Planner, Coder, Tester, Reviewer — loaded automatically by opencode
regardless of which role is active. Role-specific responsibilities,
inputs/outputs, and definition of done live in `agents/<role>.md`; this
file is only for things that apply no matter which role is running.

## Use the tool built for the job, not a shell command that mimics it

Before reading, searching, or editing anything, check which dedicated
tool covers the action (read, glob, grep, edit) and use that instead of
an equivalent shell command (`cat`, `ls`, `grep`, `find`, `echo >>`,
`sed -i`, etc.) via bash.

This is not a style preference. Dedicated tools and bash commands are
governed by *separate* permission rules in `.opencode/agents/*.md`. A
file this project protects from editing (e.g. `03-taxonomy.md`,
`agents/*.md`) is protected via the `edit` permission block — but a
bash command that achieves the same result is evaluated under the
*bash* permission block instead, which does not necessarily have an
equivalent deny rule. Using bash to touch files is a way to
accidentally bypass a permission boundary that looks airtight on
paper. Treat this as load-bearing, not a nicety.

Do this check every time, on every task — don't rely on remembering
that a dedicated tool exists for this. Make checking itself a required
first step, separate from judging whether it's worth using.

## When a tool call is denied, stop — don't route around it

If a dedicated tool call gets denied, that's the answer. Don't retry
the same action through bash, a different working directory, or a
symlink to achieve the same result. If the denial seems wrong for the
task at hand, say so and ask Ali — don't self-correct by finding a
path the permission system didn't anticipate.

## Spec files are read-only to every agent

`00`–`07` numbered files and `agents/*.md` are the contract every
ticket, test, and review is measured against. No agent edits them
directly, regardless of role or how confident the agent is that a
change is obviously correct. Flag the discrepancy on the ticket
instead, for Ali to resolve.

## This file is also read-only to every agent

`AGENTS.md` defines how every agent behaves. An agent that can rewrite
the rules governing its own behavior isn't meaningfully bound by them.
Enforced via `"AGENTS.md": deny` in each role's `edit` permission
block — if you're reading this and considering editing it, that's the
signal to stop and flag it to Ali instead.

## Prefer one command per bash call

opencode parses each bash invocation and checks every distinct
sub-command it finds against permission rules independently — chaining
with `&&`/`;`/`|` does not let an unapproved command hide behind an
approved one. But it does mean a single chained call is only as
"approved" as its least-approved piece: if one sub-command in the
chain has no matching rule, the whole call needs manual approval, even
if the other pieces are already allow-listed.

Prefer separate calls anyway, for two practical reasons, not a
security one:
- A denied or ask-gated piece in the middle of a chain can abort the
  whole sequence partway through, leaving things in an unclear state.
- Keeping calls atomic makes it obvious on review which specific
  command needed approval and why.

This is a workflow preference, not a workaround for a permission gap.