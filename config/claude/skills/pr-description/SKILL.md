---
name: pr-description
description: Generate a GitHub/GitLab pull request description from the git diff between the current branch and its upstream base branch. Trigger this skill whenever the user asks to write, generate, or draft a PR description, pull request body, PR summary, or merge request description. Also trigger when the user says things like "write a PR for this branch", "describe my changes", or "make a PR desc". Always use this skill for anything PR-description-related — do not attempt to write PR descriptions without it.
---

# PR Description Skill

Gen GitHub/GitLab PR desc Markdown from diff: current branch vs base.

---

## Step 1 — Discover the base branch

Find upstream base:

```bash
git rev-parse --verify origin/develop
git rev-parse --verify origin/main
```

Use first existing. Ambiguous -> ask user: *"Which branch did you open this from? (e.g. main, develop)"*

---

## Step 2 — Get the diff

Base ref known -> get:

```bash
# Commit log (one line each)
git log <base>..HEAD --oneline

# Full diff (stat + patch)
git diff <base>..HEAD --stat
git diff <base>..HEAD
```

Large diff (>600 lines) -> `--stat` + sample:

```bash
git diff <base>..HEAD --stat
git diff <base>..HEAD | head -400
```

Note truncation in desc.

---

## Step 3 — Write the PR description

Markdown body, **only these two sections** (omit N/A):

```markdown
## Summary

<concise explanation of what changed and why, derived from the diff — 2–5 sentences or a short bullet list>

## Breaking Changes

<only include if there are breaking changes: renamed/removed public APIs, changed interfaces, config schema changes, removed behaviour, required migrations>
- If none: omit this section entirely (do not write "None" or "N/A")
```

### Writing rules

- Specific: ref actual file/fn/module names from diff when clarifies.
- No invented intent -> derive from diff + commit msgs.
- Concise. No filler ("This PR...", "In this change...", "As part of this work...").
- No extra sections (no "How to test", "Checklist") unless requested.
- Output only Markdown block. No preamble/commentary.