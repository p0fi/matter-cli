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

## Stacked pull requests

Use a stack when a change genuinely depends on another PR that hasn't merged yet — e.g. a bug fix that builds on an in-flight feature branch, or a large feature deliberately split into reviewable layers. Independent work still gets its own branch off `main` and its own standalone PR; don't stack things that don't depend on each other.

This repo uses **GitHub's native stacked pull requests** (public preview), managed through the `gh stack` CLI extension (`github/gh-stack`) — not a manually chosen `--base` branch. A PR whose base happens to equal another PR's head branch is *not* the same thing: GitHub only renders the stack map, auto-rebases upper layers, and retargets a PR's base automatically when the merge below it lands if the PR was created through `gh stack`. Always use the extension; never approximate a stack with `git checkout -b` + `gh pr create --base <branch>`.

Install once per machine: `gh extension install github/gh-stack`.

**Starting a new stack:**

```bash
gh stack init                    # first branch of the stack, targets main
git add . && git commit -m "..."
gh stack add my-next-layer       # new branch on top of the current one
git add . && git commit -m "..."
gh stack push                    # push all branches to origin
gh stack submit                  # create/update the linked PRs on GitHub
```

`gh stack add -Am "message" branch-name` stages, commits, and creates the next layer in one step.

**Adding a layer to an existing stack** (e.g. implementing an issue that's a prerequisite for, or builds on, a PR already open in the stack):

```bash
gh stack checkout <PR#>          # discovers and tracks the stack locally if not already tracked
gh stack add my-new-layer        # branches off the current top of the stack
# ... commit work ...
gh stack push
gh stack submit
```

`gh stack checkout` accepts a stack number, PR number/URL, or branch name, and will pull an untracked-locally stack down from GitHub if needed — run it before assuming a stack has to be recreated from scratch.

Other useful commands: `gh stack view` (show the current stack and its PR links), `gh stack sync` (pull remote changes into the local stack), `gh stack rebase` (rebase the whole stack after the trunk moves), `gh stack merge` (merge the stack in order).