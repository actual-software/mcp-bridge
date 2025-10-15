# Claude Code Rules for MCP Repository

## CRITICAL: First Session Setup

**On first interaction with this repository, I MUST:**

1. Check if git hooks are installed:
   ```bash
   ls -la .git/hooks/pre-commit .git/hooks/pre-push
   ```

2. If hooks don't exist or aren't executable, install them:
   ```bash
   make install-hooks
   ```

This ensures consistent validation for all commits and pushes.

## Git Hooks (Automatic Enforcement)

Once installed, hooks automatically run:
- `git commit` → runs `make check` (30s)
- `git push` → runs `make validate` (5min)

**I MUST wait for hooks to complete fully.** Never use `--no-verify` to bypass them.

## My Workflow

**Before every `git commit`:**
```bash
make check  # Fast validation
```

**Before every `git push`:**
```bash
make validate  # Full validation - MUST pass
```

If hooks are installed, these run automatically and **I must wait for completion**.
If hooks aren't installed, I run these commands manually.

**Exit 0 = proceed. Exit 1 = fix and re-run.**

## What Each Does

### `make check` (Fast - 30s)
- Build verification
- Format check
- Import organization
- Basic lint

Use frequently during development.

### `make validate` (Full validation)
- Everything in `make check`
- Full linting audit
- Unit and integration tests (E2E and slow tests skipped)
- Documentation checks
- Complexity checks
- Binary verification

**Skipped for speed:** E2E tests, performance/slow tests (via `-short` flag), coverage (CI runs these)

**MUST pass before pushing.**

## Key Rules

1. **Never push without `make validate` passing**
2. **Never assume "0 issues" means success** - check build first
3. **Fix all failures** - no shortcuts
4. **Ask for help** if stuck - don't push broken code

## Common Trap

**WRONG:**
```bash
# Break code
make lint  # Shows "0 issues" (code doesn't compile!)
git push   # CI fails
```

**CORRECT:**
```bash
make validate  # Runs build first, catches compile errors
# Fix all issues
git push       # Only if validate passed
```

## Done Criteria

Task is complete when:
- ✅ Feature/fix implemented
- ✅ `make validate` exits 0
- ✅ Code pushed

If validate fails, task is NOT complete.
