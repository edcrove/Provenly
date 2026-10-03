---
name: record-decision
description: Record a decision Ed takes (product, domain, architecture, process) in every place that must reflect it — Notion Decision Register, repo docs and Trello cards — and supersede old decisions without deleting them. Use whenever Ed answers a pending decision or changes an earlier one.
---

# Record a decision

1. **Notion Decision Register** (data source in *08 — Planning & Control*): create one page per decision with
   Decision (short title), Category, Status (`Accepted`, `Deferred`, `Implemented`, `Rejected`), Date, Rationale
   (start with "Decided by Ed"), Recommendation (what was decided, concretely), Alternatives.
   - If it replaces an earlier decision: set the old one to **Superseded** and add "Superseded <date> by …" to its
     Rationale. Never delete or rewrite the old decision.
2. **Repo**:
   - behavior/implementation choice → `docs/implementation-decisions.md` (append a **Status:** line to the entry).
   - plan/scope → `docs/mvp-plan.md` (decisions table, phases, out-of-scope list).
   - commit and push.
3. **Notion consolidated page / Incubator**: update the summary lines that quote the decision; deferred ideas go to
   the Incubator as a "Pending design" item (problem, scope boundaries, parity input), never as Trello cards.
4. **Trello**: comment on every card the decision unblocks or changes; rename/relabel cards that leave a phase
   (do not archive or delete without Ed).
5. Tell Ed in one or two lines what was recorded where.
