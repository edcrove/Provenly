---
name: close-milestone
description: Close a Provenly phase (POC, MVP…) once Ed approves — PR merged by Ed, main as default branch, CI green on main, Notion phase marked done, Trello reorganized for the next phase, final report. Use when every card of the phase is in Hecho or Ed says to close the phase.
---

# Close a milestone

Preconditions (verify live, report what is missing and stop):
- Every card of the phase is in **Hecho** (approved by Ed) or explicitly moved out of the phase by a decision.
- The phase PR is green and mergeable. **Only Ed merges** to `main`; Claude never merges or pushes to `main`.

Steps after Ed merges:
1. GitHub: confirm the merge, that `main` is the default branch (Ed changes it if not — ask), CI green on the
   merge commit, branch protection intact. Unsubscribe from the PR and delete pending check-in triggers.
2. Repo: tag only if Ed asks. Next work starts from `main` on a fresh session branch.
3. Notion (`notion-safe-edit`): mark the phase done on the Consolidated Workspace page with date, PR link and
   scope delivered; move deferred items to the Incubator; Decision Register entries implemented → `Implemented`.
4. Trello: archive nothing without Ed; order **Por hacer** for the next phase (labels by phase), comment on cards
   that carry over.
5. Report to Ed in a few lines (UYT): what shipped, what moved to the next phase, first card proposed.
