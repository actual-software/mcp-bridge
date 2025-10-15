# Git Hooks

Enforces validation automatically in normal git workflow.

## Installation

```bash
make install-hooks
```

## What Gets Installed

### pre-commit hook
- Runs `make check` (30s)
- Catches basic issues before commit
- Blocks commit if validation fails

### pre-push hook
- Runs `make validate` (5min)
- Runs full CI checks before push
- Blocks push if validation fails

## Workflow After Installation

```bash
git commit  # Automatically runs make check
git push    # Automatically runs make validate
```

No need to remember - it's enforced.

## Bypass (Not Recommended)

```bash
git commit --no-verify  # Skip pre-commit
git push --no-verify    # Skip pre-push
```
