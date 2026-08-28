# Git Workflow — CommerceOS

> **Why this document exists:** your current workflow is `git add .` →
> `git commit -m ""` → `git push -u origin main`, every time, straight to
> `main`. It works, in the sense that the code ends up on GitHub. It also
> means every commit is an opaque, unreviewable dump, there is no way to
> undo one change without undoing everything next to it, and nothing
> stops a secret or a junk file from going in. Your own history already
> shows the cost: commit `646a5f9` ("Most phases are implemented") touched
> **1,134 files in one shot**; `201ca4d` ("Phase 1 completed") touched
> **1,055**. Neither commit message tells you — or a judge reading your
> repo — what actually changed or why. This document is the fix: a
> workflow that is still fast for a solo project on a deadline, but
> produces a history someone else (a judge, a teammate, future-you) can
> actually read.
>
> This is written for exactly your situation: one person, one repo,
> `main` as the only branch that matters, a hard deadline. It is
> deliberately **not** full GitFlow (release branches, hotfix branches,
> develop branches) — that ceremony solves problems a team of dozens has,
> not a buildathon solo project. Everything below is the subset of
> "industry standard" that actually pays for itself at this scale.

---

## 0. Fix these two things first (five minutes, once)

Before touching workflow habits, close two gaps already sitting in this
repo:

1. **Untrack the OS junk files.** `.DS_Store` and `backend/.DS_Store` are
   currently tracked in git (`git ls-files | grep DS_Store` shows both).
   They're macOS Finder metadata — noise in every diff, and a magnet for
   merge conflicts that mean nothing. Fix:
   ```bash
   echo ".DS_Store" >> .gitignore
   echo "**/.DS_Store" >> .gitignore
   git rm --cached .DS_Store backend/.DS_Store
   git commit -m "chore: stop tracking .DS_Store files"
   ```
2. **Rotate the exposed credential.** If you haven't already: revoke the
   GitHub personal access token currently embedded in `git remote -v`'s
   output, re-point the remote to the plain HTTPS URL (no credentials in
   it), and let a credential helper or SSH key handle auth instead. A
   token embedded in a remote URL is stored in plaintext in
   `.git/config` — anything that reads that file, or anyone you paste
   `git remote -v` output to, gets it.

Everything else in this doc is about not creating the next version of
either problem.

---

## 1. The core loop

For any change bigger than a typo, the loop is: **branch → commit(s) →
push → merge to main → delete the branch.** Not because a solo repo
needs code review from someone else, but because a branch is a free,
disposable workspace — if the change goes sideways, you delete the
branch and `main` was never touched. Committing straight to `main` means
every experiment is permanent the moment you type `git commit`.

```bash
# 1. Start from an up-to-date main
git checkout main
git pull

# 2. Branch for the thing you're about to do
git checkout -b feat/cart-item-removal

# 3. Work. Commit in small, logical pieces (see §3) as you go —
#    not one giant commit at the end.

# 4. Before pushing, run the project's own checks (see §4)
cd backend && go build ./... && go vet ./... && go test ./...
cd ../frontend && npm run lint && npm run build

# 5. Push the branch (not main)
git push -u origin feat/cart-item-removal

# 6. Open a PR on GitHub, even solo — see §6 for why — then merge it
# 7. Clean up
git checkout main
git pull
git branch -d feat/cart-item-removal
```

### Naming branches

Prefix by intent, then a short kebab-case description:

| Prefix | For |
|---|---|
| `feat/` | a new capability (`feat/conversational-checkout`) |
| `fix/` | a bug fix (`fix/cart-total-rounding`) |
| `chore/` | tooling, deps, config, cleanup (`chore/ci-postgres-service`) |
| `docs/` | documentation only (`docs/demo-script`) |
| `refactor/` | restructuring with no behavior change |
| `test/` | adding/fixing tests only |

The prefix isn't ceremony — it's what makes `git branch -a` and your PR
list scannable six weeks from now instead of a wall of `fix-stuff` and
`update2`.

---

## 2. Never commit straight to `main` (with one narrow exception)

The branch-per-change habit above is what actually protects `main`. The
one exception: a genuinely trivial, self-contained fix — a typo in a
comment, a one-line `.gitignore` addition — can go straight to `main`
with its own honest commit. If you're unsure whether something qualifies
as "trivial," it doesn't; branch it.

If you want GitHub to enforce this instead of relying on habit: repo
Settings → Branches → add a branch protection rule for `main` requiring
a pull request before merging. Free on a public repo, and it means a
tired 2am commit can't accidentally land directly on `main`.

---

## 3. Commits: small, atomic, honestly described

### Atomic means one logical change per commit

Not "one file" — one *change*. Adding a new endpoint and its route
registration and its one caller is one atomic commit even though it
touches three files. Adding that endpoint *and* unrelated cart-UI polish
*and* a dependency bump is three commits, even if you did the work in
one sitting.

Why this matters in practice: `git bisect` (finding which commit
introduced a bug) and `git revert` (undoing one change) both only work
cleanly when commits are atomic. A 1,134-file commit is neither
bisectable nor revertable — you either keep all of it or lose all of it.

Use `git add -p` (patch mode) instead of `git add .` when you've made
several unrelated changes in one working session — it lets you stage
(and commit) one logical chunk at a time instead of everything at once:

```bash
git add -p          # walk through each hunk, y/n/s to split further
git commit -m "..."
git add -p           # stage the next logical chunk
git commit -m "..."
```

### Commit messages: Conventional Commits

Format: `<type>(<scope>): <what changed>, imperative mood, under ~70 chars`

```
feat(agents): add conversational checkout entry point
fix(catalog): correct paise amount in repository test fixture
chore(gitignore): stop tracking .DS_Store
docs(files): add git workflow guide
refactor(growth): extract EV heuristic into its own function
test(policy): add Level 3 hard-gate coverage
```

Types that matter here: `feat`, `fix`, `chore`, `docs`, `refactor`,
`test`, `perf`, `ci`. Scope (in parentheses) is optional but helpful in
a multi-package repo like this one — `agents`, `growth`, `policy`,
`frontend`, `db` all read clearly.

For anything non-obvious, add a body after a blank line explaining
**why**, not what (the diff already shows what):

```
fix(catalog): correct paise amount in repository test fixture

The paise-normalization migration changed airpods-pro-2's stored price
from 24900 to 2490000, but this test was never updated to match, so
`go test ./...` has been silently failing since that migration landed.
```

That second commit is a real one from this session — compare it to
`"Most phases are implemented"` as a diff-review experience for anyone
(including you, in a month) trying to understand what happened and why.

### What never goes in a commit message

`"fix"`, `"update"`, `"wip"`, `""` (empty), or a message describing a
different change than the diff actually contains. If you can't describe
a commit in one honest sentence, it's probably not atomic yet — split
it.

---

## 4. Before every commit, not just before every push

A commit is cheap; a broken commit that makes it into `main`'s history
is not. Run, at minimum:

```bash
git status              # anything staged that shouldn't be? (.env, a debug file, node_modules)
git diff --staged       # read what you're about to commit, top to bottom
```

Before pushing a branch (not necessarily every single commit on it —
that's what CI is for, see §7):

```bash
cd backend && go build ./... && go vet ./... && go test ./...
cd frontend && npm run lint && npm run build
```

This project's own history is the argument for this step: the two test
failures fixed earlier this session (`TestPostgresRepositoryGetProduct`,
`TestPostgresRepositoryListProducts`) had been silently broken since a
migration and a seed-data change landed, because nothing ran `go test`
against them afterward before committing. `go vet` alone would have
caught the `fakeCatalogRepo` interface-satisfaction bug immediately
instead of it surfacing later.

---

## 5. Secrets: the .gitignore is necessary but not sufficient

`.gitignore` already correctly excludes `.env`, `.env.local`, `.env.*`,
and `node_modules/` in this repo — good, and confirmed none of those are
currently tracked. But `.gitignore` only stops *new* accidental commits;
it does nothing if a secret is already staged or already committed. Two
extra habits:

- **Read `git diff --staged` before every commit** (see §4) — it is the
  actual last line of defense, and it's free.
- **If a secret ever does get committed**, `git rm` in a new commit is
  **not enough** — the secret is still in history and still in
  `origin`. The correct response is: rotate/revoke the secret
  immediately (treat it as burned, same as the exposed token in §0),
  *then* worry about scrubbing history if you want to (`git filter-repo`
  or GitHub's secret-scanning support) — history-rewriting is optional
  cleanup, rotation is not.

`infra/.env.example` and `.env.example` (already in this repo) are the
right pattern: commit the *shape* of the config, never the values.

---

## 6. Pull requests, even solo

Opening a PR from your branch into `main` — even with nobody else to
review it — buys you three things a direct push doesn't:

1. **A diff view before it's permanent.** GitHub's PR diff is a better
   review surface than scrolling `git log -p` after the fact.
2. **CI runs on the PR, not after the damage.** This repo already has
   `.github/workflows/ci.yml` — a PR is where you find out it's red
   *before* `main` is broken, not after.
3. **A written record of *why*.** The PR description is where "why did
   we do this" lives — a commit message explains one commit, a PR
   description explains the whole branch's intent. For a buildathon,
   this doubles as free documentation a judge can skim.

Merge strategy for a solo repo: **squash merge** if the branch's commits
were exploratory ("wip", "try again", "actually fix it"); **regular
merge** (preserving individual commits) if you kept them atomic per §3.
Either is fine — the one to avoid is a branch with 15 "wip" commits
merged as 15 separate commits into `main`'s permanent history.

---

## 7. CI is your safety net, not your first check

`.github/workflows/ci.yml` exists in this repo already — treat a red CI
run on a PR as a hard stop, not a suggestion. It catches what local
habit slips on (forgetting to run tests after a rebase, an environment
difference, a test that only fails against a fresh database). It should
never be the *first* time you learn a change is broken, though — that's
what §4's pre-push checklist is for. CI confirms; it shouldn't discover.

---

## 8. Tags, for milestones that matter

For a buildathon specifically, tag the commit you actually submit —
it's the difference between "the code as graded" and "whatever `main`
happens to be by the time someone checks months later":

```bash
git tag -a v1.0-buildathon-submission -m "Razorpay Buildathon submission"
git push origin v1.0-buildathon-submission
```

Tag any other point you'd want to be able to return to exactly (a working
demo checkpoint before a risky refactor, for instance).

---

## 9. What NOT to do (all currently-live habits worth retiring)

- **`git add .` without looking first.** Use `git status` and
  `git diff --staged` before every commit — see §4.
- **Committing straight to `main`.** Branch, even for a solo project —
  see §2.
- **One commit per session instead of one commit per change.** Splits
  into atomic commits with `git add -p` — see §3.
- **Empty or meaningless commit messages.** Conventional Commits format,
  one honest sentence minimum — see §3.
- **Pushing without running build/lint/test locally.** CI will catch it
  eventually, but a red CI run on `main` (versus a branch) means the
  breakage is live — see §4 and §7.
- **Rewriting history that's already pushed and shared** (`git push
  --force` to `main`). Never do this on a branch anyone else (including
  CI, including a judge who cloned it) might have already pulled. Force-push
  is fine on your own not-yet-shared feature branch; never on `main`.

---

## 10. Quick reference

```bash
# Start a change
git checkout main && git pull
git checkout -b feat/short-description

# Stage thoughtfully
git status
git add -p                     # or `git add <specific-files>`
git diff --staged              # read it before committing

# Commit
git commit -m "feat(scope): imperative, under ~70 chars"

# Before pushing
cd backend && go build ./... && go vet ./... && go test ./...
cd ../frontend && npm run lint && npm run build

# Push the branch, not main
git push -u origin feat/short-description
# → open a PR on GitHub, let CI run, merge

# After merge
git checkout main && git pull
git branch -d feat/short-description

# Undo the last commit but keep the changes staged
git reset --soft HEAD~1

# See what actually changed before committing
git diff                       # unstaged
git diff --staged              # staged

# Recover from almost anything (as long as you haven't gc'd)
git reflog
```

---

*This workflow is intentionally sized for one person and a deadline —
no `develop` branch, no release branches, no mandatory second reviewer.
If this project ever grows a second contributor, the next thing to add
is a `CONTRIBUTING.md` with review requirements; everything else here
already scales.*
