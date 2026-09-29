# Triage Labels

The skills speak in terms of five canonical triage roles. This file maps those
roles to the label strings used in this repo's tracker.

| Label in mattpocock/skills | Label in our tracker | Meaning                                  |
| -------------------------- | -------------------- | ---------------------------------------- |
| `needs-triage`             | `needs-triage`       | Maintainer needs to evaluate this issue  |
| `needs-info`               | `needs-info`         | Waiting on reporter for more information |
| `ready-for-agent`          | `ready-for-agent`    | Fully specified, ready for an AFK agent  |
| `ready-for-human`          | `ready-for-human`    | Requires human implementation            |
| `wontfix`                  | `wontfix`            | Will not be actioned                     |

## Where the label is recorded

On a `**Triage:**` line near the top of the ticket file in `backlog/`, e.g.
`**Triage:** ready-for-agent`. This is separate from the board `**Status:**`
line (`Backlog | Ready | In progress | Testing | Review | Done`), which stays
owned by the Planner/Coder/Tester/Reviewer workflow.

`ready-for-agent` roughly means a ticket is ready to be set to
`**Status:** Ready`; do not set both automatically. Ali decides.

Tickets authored by the Planner are already agent-ready; do not triage them.
