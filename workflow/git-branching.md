# Git Workflow

## Branch Strategy (Gitflow)

```
main          ← production-ready
├── develop   ← integration point
├── feature/* ← off develop
├── bugfix/*  ← off main
└── release/* ← release prep
```

## Commit Rules

```
<type>(<scope>): <subject>
```

**Types**: `feat`, `fix`, `refactor`, `test`, `docs`, `chore`
**Subject**: imperative, no period, max 72 chars

## Pull Request

1. PR against `develop` (features) or `main` (hotfixes)
2. At least 1 reviewer
3. All CI checks pass
4. Squash merge

## Revert

`git revert <commit>` → PR → review → merge
