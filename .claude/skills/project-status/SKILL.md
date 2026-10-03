---
name: project-status
description: Report where Provenly stands right now — Trello cards per list, PR/CI state, default branch, pending decisions in Notion and open Incubator designs — checked live, never from memory. Use when Ed asks "¿qué falta?", "¿cómo seguimos?", "¿en qué estamos?" or for any status question.
---

# Project status

Ed's rule: **verify before reporting state.** Every line of the answer must come from a source read in this turn.

## Read (in parallel)
- **Trello** board **Provenly**: cards in Por hacer, En progreso, Validation, Hecho (names, labels, last comment).
  Note cards waiting on Ed (asked "¿Paso X a Hecho?" with no answer).
- **GitHub** `edcrove/Provenly`: open PRs (state, mergeable, latest check runs on head), default branch, latest CI
  run on the working branch. `git fetch` + `git status` for unpushed work.
- **Notion**: Decision Register entries with Status `Proposed`/`Deferred` (pending Ed); the consolidated page's MVP
  plan section; Incubator "Pending design" items. Batch reads (rate limit).
- Scheduled check-ins: `list_triggers` (what is armed and when).

## Answer
- Times in **UYT (UTC−3)**. Spanish (Uruguay) or English, whichever Ed used.
- Answer first, short: what is waiting on Ed (decisions, approvals, merges), what Claude does next, what is blocked.
  Group only if it helps; no recap of finished work unless asked.
- If a source cannot be read, say which and do not guess its state.
