# Git Workflow

Agents must treat commits as part of the development process, not an afterthought. When starting a new feature or command, agents should create a new branch to develop the feature or command. When the feature or command is complete, agents should merge the branch into the main branch using a pull request.

## When to commit

Agents should **suggest committing** (or commit directly if the user has granted permission) at these points:

* **After a feature is confirmed working** — tests pass, the user is satisfied with the result.
* **After fixing a bug** — once the fix is verified with a test and confirmed by the user.
* **Before starting a risky refactor** — so there is a clean rollback point.
* **After a meaningful intermediate milestone** — e.g., "parser done and tested, index not started yet."
* **After updating docs, config, or CI** — these are self-contained changes worth capturing.

When in doubt, commit more often rather than less. Small, well-described commits are cheap and easy to review or revert.

## Commit message format

Use short, imperative-mood subject lines. A body is optional but encouraged for non-trivial changes.

```
<type>: <concise summary>

Optional longer explanation of what changed and why.
```

Types (lowercase):

* `feat` — new feature or command
* `fix` — bug fix
* `refactor` — restructuring without behavior change
* `test` — adding or updating tests only
* `docs` — documentation, README, AGENTS.md
* `chore` — CI, Makefile, tooling, dependency updates
* `style` — formatting, linting fixes (no logic change)

## What to commit

* **Do** commit source code, tests, config, documentation, CI, and task definitions.
* **Do not** commit build artifacts (`bin/`), coverage files (`coverage.out`, `coverage.html`), or OS junk (`.DS_Store`). These are already in `.gitignore`.

## Pre-commit checks

Before committing, agents must verify:

1. `mise run lint` passes (or at minimum `go vet ./...` and `gofmt -l .` reports no files).
2. `mise run test` passes.
3. No unrelated changes are staged — keep commits focused.

If a commit includes a new feature, the commit should include the tests for that feature.