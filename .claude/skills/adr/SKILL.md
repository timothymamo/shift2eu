---
name: adr
description: Write an Architecture Decision Record for shift2eu under docs/adr. Use when a change introduces or alters an architectural decision - a new layer or port, a dependency or framework choice, a data format, the taxonomy structure, the testing approach, or tooling - or when asked to document a decision.
---

# Write an ADR

1. Next number: `ls docs/adr` and take the highest `NNNN` plus one.
2. Create `docs/adr/NNNN-<kebab-title>.md` with this structure:

   ```markdown
   # NNNN. <Title as a decision, e.g. "Use a provider-neutral taxonomy for mapping">

   - Status: Proposed | Accepted | Superseded by [NNNN](NNNN-....md)
   - Date: YYYY-MM-DD

   ## Context
   The forces at play: requirements, constraints, what hurts today.

   ## Decision
   What we will do, in one or two sentences, then the detail.

   ## Alternatives considered
   Each option and why it lost.

   ## Consequences
   What becomes easier, what becomes harder, what we must now do.
   ```

3. Add it to the list under "Architecture decision records" in `docs/README.md`, the documentation's
   index, as `- [NNNN. <Title>](adr/NNNN-<kebab-title>.md)`; `make check` fails on a page the index
   does not list.
4. Never rewrite an accepted ADR's decision. Supersede it with a new ADR and update the old one's status.
5. Link the ADR from the PRD or PR that motivated it.

Signal only what you started (CLAUDE.md rule 10): stop a run of your own only by the pid and start time you noted; never a process found by name, never a group.
