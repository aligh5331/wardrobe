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