# Domain Docs

How the engineering skills should consume this repo's domain documentation
when exploring the codebase. Layout: single-context.

## Before exploring, read these

- **`CONTEXT.md`** at the repo root (glossary of domain terms).
- **`docs/adr/`**: ADRs that touch the area you're about to work in.
- The numbered specs `00`-`07` and `06-decisions.md` remain the authoritative
  contracts (`AGENTS.md` section 2). `CONTEXT.md` and `docs/adr/` are
  supporting material and never override them.

If `CONTEXT.md` or `docs/adr/` don't exist, **proceed silently**. The
`/domain-modeling` skill (via `/grill-with-docs`) creates them lazily.

## File structure

```
/
├── CONTEXT.md
├── docs/adr/
├── 00-overview.md ... 07-architecture.md   (protected specs)
├── 06-decisions.md                         (protected decision log)
└── backlog/
```

## Use the glossary's vocabulary

When output names a domain concept (ticket title, test name, proposal), use the
term as defined in `CONTEXT.md`. Taxonomy values still come only from
`03-taxonomy.md`; never invent enum values (`AGENTS.md` section 7).

## Flag ADR and spec conflicts

If output contradicts an ADR, `06-decisions.md`, or a numbered spec, surface it
explicitly rather than silently overriding:

> _Contradicts 06-decisions.md "<decision>", but worth reopening because..._

Agents cannot edit the protected files. Flag the change to Ali.
