# Validation Workflow

## Setup (One-Time)

Install git hooks to enforce validation automatically:

```bash
make install-hooks
```

This installs:
- **pre-commit hook** → runs `make check` (30s) before every commit
- **pre-push hook** → runs `make validate` (5min) before every push

**After installation, validation runs automatically in your normal workflow:**
```bash
git commit  # Hook runs make check automatically
git push    # Hook runs make validate automatically
```

No need to remember - it's enforced.

## Two-Tier Validation

### Quick Check (~30 seconds)
```bash
make check
```

Runs:
- Build verification
- Format check
- Import organization
- Basic lint

**Use:** Frequently during development for fast feedback

### Full Validation (~5 minutes)
```bash
make validate
```

Runs everything CI runs:
- Quick check (above)
- Full lint audit
- All tests
- Coverage analysis
- Documentation checks
- Complexity analysis
- Binary verification

**Use:** Before pushing code

## Workflow

### With Hooks Installed (Recommended)

```bash
# 1. Develop
# ... make changes ...

# 2. Commit (hook runs make check automatically)
git commit -m "your message"

# 3. Push (hook runs make validate automatically)
git push
```

Hooks enforce validation - commits/pushes blocked if checks fail.

### Without Hooks

```bash
# 1. Develop
# ... make changes ...

# 2. Check before commit
make check

# 3. Validate before push
make validate

# 4. Only push if validate passes
git push
```

## What Validation Catches

- Code doesn't compile
- Formatting issues (gofmt)
- Unorganized imports
- Lint violations
- Test failures
- Low coverage
- Missing documentation
- High complexity

## Exit Codes

- `0` = All checks passed, safe to push
- `1` = One or more checks failed, fix before pushing

## Tips

- Run `make check` after each change for quick feedback
- Run `make validate` before important pushes
- If validate fails, read output carefully - it tells you exactly what's wrong
- Fix issues and re-run until green
