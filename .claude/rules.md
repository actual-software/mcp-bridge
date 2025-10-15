# Claude Code Rules for MCP Repository

## Git Hooks (User Setup)

User should run once:
```bash
make install-hooks
```

This installs hooks that automatically enforce:
- `git commit` → runs `make check` (30s)
- `git push` → runs `make validate` (5min)

## My Workflow (With or Without Hooks)

**Before every `git commit`:**
```bash
make check  # Fast validation
```

**Before every `git push`:**
```bash
make validate  # Full validation - MUST pass
```

If hooks are installed, these run automatically. If not, I run them manually.

**Exit 0 = proceed. Exit 1 = fix and re-run.**

## What Each Does

### `make check` (Fast - 30s)
- Build verification
- Format check
- Import organization
- Basic lint

Use frequently during development.

### `make validate` (Full - 5min)
- Everything in `make check`
- Full linting audit
- All tests
- Coverage analysis
- Documentation checks
- Complexity checks
- Binary verification

Mirrors complete CI pipeline. **MUST pass before pushing.**

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
