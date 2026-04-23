---
name: review-feedback
description: >
  Handle PR review feedback end-to-end: fetch review comments from GitHub,
  evaluate their validity and how to fix them, interactively ask the user
  which ones to address, implement the fixes, reply to each resolved comment
  on GitHub, and mark threads as resolved. Use this skill whenever the user
  wants to address PR review comments, respond to reviewer feedback, work
  through code review suggestions, handle review requests, or reply to PR
  discussions — even if they just say "let's go through the PR comments" or
  "what did the reviewer say?" or "can you fix the review feedback on PR 42?".
---

# PR Review Feedback Skill

This skill guides you through a complete PR review feedback cycle: fetch → evaluate → select → fix → reply → resolve.

## Step 1: Identify the PR

If the user hasn't specified a PR number, find the right one:

```bash
# Show open PRs on the current repo
gh pr list

# Or check if there's a current branch PR
gh pr view
```

Ask the user to confirm which PR to work on if it's ambiguous.

## Step 2: Fetch All Review Comments

Start with the human-readable summary — it's fast and gives you everything you need to evaluate the comments:

```bash
PR=<number>

# Get all comments in readable form (inline + review body)
gh pr view $PR --comments
```

This shows inline comments, review body text, and the PR description in one shot. Use this as your primary source for evaluating comments.

You'll need comment IDs later to post replies. Fetch those only once you know which comments you're addressing (Step 4), to avoid fetching data you don't need:

```bash
# Get inline comment IDs for the ones you'll reply to
gh api repos/:owner/:repo/pulls/$PR/comments \
  --jq '[.[] | {id, body, path, line, user: .user.login, reply_to: .in_reply_to_id}]'

# Get review IDs if you need to reply to review-body comments
gh api repos/:owner/:repo/pulls/$PR/reviews \
  --jq '[.[] | select(.body != "") | {id, body, user: .user.login}]'
```

## Step 3: Evaluate Each Comment

For each comment, assess:

1. **Is it actionable?** — Not a question, not already resolved, not a nitpick you've already addressed.
2. **Is it valid?** — Does the reviewer have a point? Consider the codebase conventions, the PR goals, and whether the code actually has the issue they describe.
3. **How to fix it** — Briefly note the file and what change is needed. If it's a style preference rather than a correctness issue, note that too.

Present your analysis as a numbered list, one item per comment thread. Include the comment author, file/line if applicable, a short quote of the comment, your assessment (Valid / Nitpick / Already addressed / Question), and a one-line fix description.

Example format:
```
[1] @alice on pkg/foo/bar.go:42 — "This function leaks the connection on error"
    Assessment: VALID — add defer conn.Close() before the error check
    
[2] @alice on pkg/foo/bar.go:78 — "nit: rename `x` to `count`"  
    Assessment: NITPICK — reasonable but cosmetic
    
[3] @bob (review body) — "Have you considered caching here?"
    Assessment: QUESTION — no code change needed, just reply
```

## Step 4: Ask What to Address

Present the numbered list and ask the user which items to tackle:

> "I found N comment threads. Which ones should I address? (Enter numbers like `1 3 4`, or `all`, or `skip` to exit)"

Wait for the user's response before proceeding. Respect their choices — don't silently fix things they didn't select.

## Step 5: Implement the Fixes

For each selected item:

1. Read the relevant file(s) before making any changes.
2. Make the code change using Edit (prefer targeted edits over full rewrites).
3. For this project: run `mise run build` after changes to confirm it compiles, and `mise run lint` to check style.
4. Note any test files that should be updated — flag them to the user if you're unsure.

Keep fixes focused. Don't refactor surrounding code unless the reviewer explicitly asked for it.

## Step 6: Reply to Each Resolved Comment

For each comment you addressed, post a reply on GitHub. Append `--jq '.html_url'` to suppress the full JSON response — you only need the URL as confirmation:

```bash
# Reply to an inline comment (reply thread)
gh api repos/:owner/:repo/pulls/$PR/comments \
  -f body="Fixed in this commit - <brief description of what changed>." \
  -F in_reply_to_id=<comment_id> \
  --jq '.html_url'

# Reply to a review-level comment (creates a new PR comment)
gh api repos/:owner/:repo/issues/$PR/comments \
  -f body="@<reviewer> addressed this - <brief description>." \
  --jq '.html_url'
```

Keep replies short and factual: what changed, not why the reviewer was wrong.

For comments you're NOT addressing (user chose to skip), leave them alone — don't mark them resolved or leave a dismissive reply.

## Step 7: Resolve Threads (Inline Comments Only)

GitHub allows resolving inline review threads via GraphQL. First get the thread IDs — use `--jq` to extract only what you need (ID, resolved state, path, first comment body) and avoid dumping the full nested response into context:

```bash
gh api graphql -f query='
query($owner:String!, $repo:String!, $pr:Int!) {
  repository(owner:$owner, name:$repo) {
    pullRequest(number:$pr) {
      reviewThreads(first:50) {
        nodes { id, isResolved, comments(first:1) { nodes { body, path } } }
      }
    }
  }
}' -F owner=:owner -F repo=:repo -F pr=$PR \
  --jq '.data.repository.pullRequest.reviewThreads.nodes | map({id, isResolved, path: .comments.nodes[0].path, body: .comments.nodes[0].body})'
```

Then resolve each addressed thread. Use `--jq` to suppress the full response:

```bash
gh api graphql -f query='
mutation($threadId:ID!) {
  resolveReviewThread(input:{threadId:$threadId}) {
    thread { isResolved }
  }
}' -F threadId="<thread_node_id>" \
  --jq '.data.resolveReviewThread.thread.isResolved'
```

Only resolve threads you've actually fixed. Leave open questions or skipped items unresolved.

## Step 8: Summarize

After addressing everything, give the user a brief summary:
- How many comments were addressed vs skipped
- Files changed
- Whether build/lint passed
- Remind them to commit and push if they haven't already (`git add <files> && git commit`)

---

## Tips for Common Situations

**Comment asks for a test** — Add the test, then mention the test file in your reply.

**Comment is vague** — If you can infer a reasonable fix, make it and explain in the reply what you interpreted. If genuinely unclear, ask the user before touching the code.

**Multiple comments on the same file** — Batch them into a single read/edit cycle rather than opening the file repeatedly.

**Comment is already fixed** — Reply "Already addressed in the current diff" and resolve the thread. No code change needed.

**Comment is about something outside scope** — Flag it to the user: "This seems outside the PR scope — want to open a separate issue?" Don't make the change without checking.
