---
name: code-review
description: Perform thorough code review with focus on security, performance, and maintainability
---

# Code Review Skill

When performing a code review, follow these guidelines:

## 1. Scope Analysis
- Read all modified files and understand the full change
- Identify the intent of the PR/change

## 2. Review Checklist
- **Security**: Check for injection risks, hardcoded secrets, auth bypasses
- **Performance**: Look for N+1 queries, memory leaks, unnecessary allocations
- **Correctness**: Verify edge cases, error handling, race conditions
- **Maintainability**: Assess naming, complexity, test coverage, documentation

## 3. Output Format
For each issue found, report:
- **Severity**: critical / major / minor / suggestion
- **File**: path and line number
- **Issue**: clear description of the problem
- **Suggestion**: concrete fix or improvement
