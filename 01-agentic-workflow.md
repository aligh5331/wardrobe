# Agentic workflow

This describes the **development-process** agents — the ones that build the
wardrobe system — not any AI inside the wardrobe app itself.

## Where each role lives
- **This Claude Project** is where specs, backlog items, and architecture
  decisions get written, with Claude acting as a design partner.
- **The coding agents** run outside this project (Ali's own agent harness or
  Claude Code), and execute one backlog ticket at a time against a spec
  produced here.

## Roles
Each role has a base definition file in `agents/`. A role file is the
"system prompt" for that agent — its inputs, responsibilities, outputs, and
definition of done.

| Role | File | Job |
|---|---|---|
| Planner | `agents/planner.md` | Breaks an approved spec into backlog tickets; maintains the backlog |
| Coder | `agents/coder.md` | Implements a single ticket against its acceptance criteria |
| Tester | `agents/tester.md` | Writes/runs tests against a ticket's acceptance criteria; verifies it's actually done |
| Reviewer | `agents/reviewer.md` | Reviews a ticket's diff against the spec and project standards; approves or sends back |

*(Only `agents/tester.md` exists so far — see the generation prompt at the
end of this doc set to create the rest.)*

## Flow for one ticket
1. Planner turns an approved spec (or a piece of one) into a ticket in the backlog (`02-agile-process.md`)
2. Coder implements it
3. Tester verifies it against the ticket's acceptance criteria
4. Reviewer checks it against project standards and the original spec
5. Ali does final review and merges

A ticket doesn't move to the next stage until the current stage's
definition of done is met — no skipping straight to "done."

## Handoff contract
Every stage hands off in writing: the ticket itself carries context forward
(spec reference, acceptance criteria, what was done, what was found). No
role should need a live conversation with another role to know what to do
next — that's the point of writing it down.
