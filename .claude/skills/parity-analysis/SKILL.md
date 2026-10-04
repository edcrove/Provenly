---
name: parity-analysis
description: Compare how established test-management and reporting tools solve a Provenly design question (retries/flaky, suites, partial runs, manual testing, secrets, statuses…) and turn it into a recommendation for Ed. Use when a decision or an Incubator "Pending design" needs market input, or Ed asks how other tools do something.
---

# Parity analysis

1. **Frame the question** in one sentence and list Provenly's constraints that apply (stable `TC-<id>` identity,
   CI-reported `executionStatus` vs derived verdict, decisions already in the Decision Register /
   `docs/implementation-decisions.md`). Do not reopen decided points.
2. **Survey** current docs (web search / fetch, cite URLs) for: TestRail, Xray, Zephyr Scale, Kiwi TCMS, TestLink,
   Testomat.io, ReportPortal; add Allure/Qase when relevant. Per tool: model, terms, defaults, what users complain
   about.
3. **Synthesize** a short table (tool → approach) and the common pattern vs. the outliers.
4. **Recommend** one option for Provenly with the trade-off, plus what stays out of the MVP. Product calls are
   Ed's: present and wait.
5. Record it where it belongs: the Incubator "Pending design" item (parity input section) or the decision's
   Alternatives in the Decision Register (`notion-safe-edit` rules). Give Ed the recommendation in a few lines.
