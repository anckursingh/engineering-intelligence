---
name: ei-tdd
description: Execute the strict productionised TDD loop for Engineering Intelligence: failing test first, minimum implementation, full gates, one coherent commit, checklists updated.
---

# EI TDD Loop

Use for any feature work in this repo. Order:

1. **Read the checklists** — `.Codex/agents/IMPLEMENTATION-PLAN.md` (commit sequence) and
   `.Codex/agents/TESTING.md` (gates + matrices). Work the next unchecked item; never skip ahead.
2. **Specify** — write the smallest failing test for the behavior.
3. **Red** — `go test ./<pkg> -run <Test> -count=1`; verify it fails for the expected reason.
4. **Green** — minimum implementation. No scaffolding, no refactor while red.
5. **Refactor** — only after green.
6. **Gate** — `./scripts/verify.ps1` (+ `-Race` where CGO exists);
   `go test ./test/acceptance/... ./test/e2e/...` when relevant.
7. **Commit** — one coherent capability; message ends with the Co-Authored-By line;
   update the checklists in the same commit.

Never: implement before red, rewrite existing acceptance tests to fit code, leak
AIKOQL/transport types past `internal/knowledge`, or break the AC regression list
(TESTING.md §31).
