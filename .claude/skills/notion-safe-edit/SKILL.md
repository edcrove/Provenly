---
name: notion-safe-edit
description: Rules for any write to Provenly's Notion workspace (Consolidated Workspace, Decision Register, Incubator) — read before write, batch for the rate limit, never delete or alter an original before its copy is validated, supersede instead of rewriting decisions. Use before creating, moving, updating or migrating Notion pages.
---

# Editing Notion safely

- **Read before write.** Fetch the page (and the data source schema for databases) in this turn; edit the current
  content, never a remembered version. Quote exact property names and select options from the schema.
- **Rate limit.** The plan is limited: batch reads (one fetch per page, `query-data-sources` instead of page by
  page) and writes (create several pages in one call, one update with all changes). Back off and retry on 429;
  never loop single-page writes.
- **Migrations / moves:** copy → validate the copy (fetch it, compare every section, property and link) → only
  then archive or change the original, and only with Ed's OK. Never delete or alter an original first.
- **Decision Register:** one page per decision; changes never rewrite history — set the old entry to
  **Superseded** with "Superseded <date> by …" and create the new one (use the `record-decision` skill).
- **Where things go:** decided → Decision Register (+ repo docs); idea not refined → **Incubator** ("Pending design"
  item with problem, scope limits, parity input); executable work → Trello. No backlog cards in Notion.
- After writing, fetch again to confirm, and tell Ed what changed where in one line.
