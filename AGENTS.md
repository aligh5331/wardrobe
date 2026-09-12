# AGENTS.md

Cross-cutting instructions for every agent working in this repository.

This file defines rules that apply regardless of role. Role-specific
responsibilities, permissions, inputs/outputs, and definitions of done live in
`agents/*.md` and are enforced by the corresponding files under
`.opencode/agents/`.

## 1. Project context

This project is currently in **Phase 1: the ingestion/cataloging pipeline**.

The current product is a local wardrobe catalog that turns one photo of one
garment into validated structured data. Weather integration and outfit
recommendation are later phases and are not current implementation scope.

The following constraints are architectural decisions, not suggestions:

- Inference and catalog data remain local. Do not introduce cloud inference,
  hosted model APIs, or cloud training.
- Do not introduce model fine-tuning. Phase 1 uses the local Qwen3-VL-8B
  tagging model with constrained prompting.
- Input is one garment per photo, as a flat lay or hanger photo. Outfit-photo
  detection is out of scope.
- Taxonomy values come from `03-taxonomy.md`.
- Wardrobe item fields and types come from `04-data-schema.md`.
- VLM tagging behavior and output validation come from
  `05-vlm-tagging-spec.md`.
- Runtime and stack decisions come from `07-architecture.md`.
- Architectural decisions and their rationale are recorded in
  `06-decisions.md`.

Do not silently reinterpret or weaken these constraints.

## 2. Source-of-truth hierarchy

When instructions conflict, use this order:

1. The current ticket's acceptance criteria, within the scope of the approved
   specs.
2. The approved numbered specification files (`00`–`07`).
3. `06-decisions.md` for the rationale and resolved project decisions.
4. This file for cross-cutting agent behavior.
5. The role-specific agent definition for role responsibilities and permissions.

A ticket must not be used to smuggle in a change to an approved specification.
If the ticket and its referenced spec disagree, stop and flag the discrepancy
to Ali rather than choosing silently.

`later-ideas.md` is explicitly non-authoritative. Ideas in it are not
ticketable or actionable until promoted through the normal specification
process.

## 3. Protected project contracts

The following files are read-only to agents:

- `00-overview.md`
- `01-agentic-workflow.md`
- `02-agile-process.md`
- `03-taxonomy.md`
- `04-data-schema.md`
- `05-vlm-tagging-spec.md`
- `06-decisions.md`
- `07-architecture.md`
- `agents/*.md`
- `AGENTS.md`

These files define the project's contracts and agent behavior. Do not edit,
rewrite, rename, delete, or replace them as part of ordinary ticket work.

If implementation requires changing one of these contracts:

1. Stop the affected work.
2. Explain exactly which contract needs to change and why.
3. Flag it to Ali / the planning process.
4. Do not work around the restriction by modifying the file through another
   tool or command.

## 4. Use the tool built for the job

Before reading, searching, or modifying repository content, check whether a
dedicated tool provides the operation.

Prefer:
- `list` for listing files
- `read` for reading files
- `glob` for locating files by path pattern
- `grep` for searching file contents
- `edit` for targeted file changes
- `write` for creating files
- other dedicated project/agent tools when they directly cover the operation

Do not use shell commands merely as substitutes for dedicated tools, such as
using `cat`, `ls`, `find`, `grep`, `sed`, or redirection to perform work that
the dedicated tools already cover.

This matters because OpenCode permissions are defined separately for different
tools. A permission boundary on `edit`, for example, must not be treated as
permission to accomplish the same modification through an unrelated tool.

## 5. Permission boundaries are real boundaries

If a tool call is denied:

- Stop that operation.
- Do not retry it through bash.
- Do not change the path, working directory, or access method to bypass the
  denial.
- Do not use symlinks, temporary copies, or another tool to accomplish the
  same denied operation.
- Explain the blocker and ask Ali when the task genuinely requires an action
  outside the role's permissions.

A denied operation is a signal about the allowed scope of the current role,
not a puzzle to solve.

## 6. Read before changing

Before editing implementation or test code:

1. Read the ticket and all referenced acceptance criteria.
2. Read the relevant sections of the referenced specification files.
3. Inspect the existing implementation and surrounding code.
4. Check applicable project decisions and architecture.
5. Check the available skills for one relevant to the language, framework, or
   library involved, when the role definition requires that check.
6. Make the smallest change that satisfies the ticket.
7. Verify the result against the ticket and the referenced contracts.

Do not make drive-by refactors, speculative abstractions, or unrelated cleanup
while implementing a ticket.

## 7. Specifications and enums

Never invent a new taxonomy, schema, or VLM enum value during implementation.

For example, values such as category, subcategory, color, pattern, warmth, and
formality must remain consistent with the approved taxonomy. A subcategory
must be valid for its selected category.

If an implementation exposes a genuine gap in the approved contract:

- do not silently extend the enum;
- record the problem on the ticket;
- send it through the appropriate planning/specification process.

Changes to `03-taxonomy.md` and dependent contracts must be coordinated in the
same approved change, as required by the taxonomy specification.

## 8. Scope and phase discipline

Agents work on the current ticket, not on the whole product.

For Phase 1:

- Implement the ingestion/cataloging pipeline.
- Do not start weather integration.
- Do not start the outfit recommender.
- Do not wire the Phase 3 `LLM_URL`/`LLM_API_KEY` into Phase 1 behavior.
- Do not introduce multi-tenant/auth architecture.
- Do not turn parked ideas from `later-ideas.md` into implementation work
  without an approved spec.

If a seemingly useful improvement crosses the current phase or ticket scope,
flag it instead of silently expanding the work.

## 9. Data and personal information

The `data/` directory contains the real wardrobe database and garment photos
and is runtime/personal data. It must remain outside version control.

Do not commit personal wardrobe data, photos, generated catalog databases,
secrets, or `.env` contents.

`.env` is sensitive configuration. Do not expose or copy its values into
tickets, logs, source code, commits, or agent responses.

Use `.env.example` only as the tracked configuration template.

## 10. Scratch space

Temporary artifacts belong in:

`<project root>/temp/`

Only use scratch space when the role's permissions allow it. If the role
requires approval for `temp/**`, ask before creating anything there.

Never use the OS `/tmp` or `/var/tmp` directories for project scratch work.
Never write outside the project root unless the role explicitly permits it
and the action is genuinely required.

Remove unnecessary temporary artifacts after the task when permitted.

## 11. Shell and command execution

Use dedicated tools instead of shell whenever they cover the operation.

When bash is genuinely required:

- Run commands within the role's allowed command set.
- Do not use bash to bypass file/tool permissions.
- Do not execute destructive commands unless explicitly permitted.
- Never use `sudo` or destructive repository commands as a workaround.
- Prefer one logical command per bash call when practical. Separate commands
  make failures, approvals, and review easier to understand.

Command chaining is **not** considered a security bypass by itself. OpenCode
evaluates parsed bash subcommands independently. The preference for separate
calls is therefore a workflow/review rule, not a permission-boundary rule.

## 12. Git safety

The repository workflow is defined by `02-agile-process.md`:

- Work occurs on the current `sprint/<slug>` branch.
- Ali creates and merges sprint branches.
- Agents do not create, push, merge, or force-push branches.
- Agents do not rewrite history.
- Coder may commit completed ticket work when its role permissions allow it;
  commits should normally correspond to a single ticket.
- Do not use `reset`, `rebase`, `restore`, `clean`, cherry-pick, branch
  deletion, or other history/destructive operations to "fix" a problem unless
  the role explicitly permits it and Ali has directed the operation.

Do not treat Git as a way to undo a permission boundary or discard someone
else's work.

## 13. Testing and verification

Verification must be evidence-based.

When a ticket changes behavior:

- Check every acceptance criterion explicitly.
- Run the relevant tests and validation available to the current role.
- Validate outputs against the applicable specification, not merely against
  what the implementation currently happens to produce.
- Pay attention to malformed input, invalid enum values, missing required
  fields, and other edge cases explicitly called out by the specs.
- Do not claim a ticket is complete based solely on compilation or a
  superficial manual inspection.

Tester owns acceptance verification. Reviewer owns final technical/spec
review. Neither role should silently take over another role's responsibilities.

## 14. Ticket and handoff discipline

The backlog is the written handoff mechanism.

Tickets live at:

`backlog/<TICKET-ID>.md`

The board state is the `**Status:**` line in that file; tickets are not moved
between status directories.

When handing work to another role, leave enough written context for the next
agent to continue without a live conversation:

- what was done;
- what was verified;
- assumptions made;
- failures or unresolved issues;
- relevant test evidence;
- any discrepancy with the spec.

Failures should be specific and reproducible, not vague statements such as
"doesn't work."

## 15. Role boundaries

The project uses four development-process roles:

- **Planner** — turns approved specs into appropriately sized backlog tickets.
- **Coder** — implements one ticket.
- **Tester** — verifies acceptance criteria and writes/runs tests.
- **Reviewer** — reviews the tested diff against the specs and project
  standards.

Respect those boundaries. In particular:

- Planner does not implement code.
- Coder does not decide that a ticket is Done.
- Tester does not fix implementation code.
- Reviewer does not fix implementation code.
- No agent unilaterally changes the approved specifications.
- Final sprint merge remains Ali's responsibility.

The detailed permissions and role contracts in `.opencode/agents/*.md` remain
authoritative for what each role can actually do.

## 16. Escalate ambiguity instead of guessing

Stop and surface the issue when:

- the ticket conflicts with an approved spec;
- the task requires a new enum/schema value;
- the requested behavior crosses the current phase;
- the required operation is denied by the current role;
- an architectural decision is unclear;
- a destructive or irreversible operation appears necessary;
- acceptance criteria are insufficient to determine the correct behavior.

State the concrete ambiguity, the relevant file/section, and the smallest
decision needed from Ali.

Do not silently invent a requirement simply to keep moving.

## 17. Definition of a good agent change

A good change is:

- within the current ticket and phase;
- consistent with the approved specs and decisions;
- minimal rather than speculative;
- validated with appropriate evidence;
- safe with respect to personal data and repository history;
- documented sufficiently for the next role;
- compliant with the current role's permission boundaries.

When in doubt, preserve the existing contract and ask rather than silently
changing it.
