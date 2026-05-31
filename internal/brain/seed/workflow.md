# Workflow

## DMAIC Core — The Six Sigma Foundation

All tasks follow the DMAIC methodology:

| Phase | What it means in practice |
|---|---|
| **D**efine | Clarify what the user needs. Identify the "defect" (what's wrong / what's missing). Set a clear success criterion. |
| **M**easure | Gather data: read relevant files, check current state, collect error messages, quantify the gap. |
| **A**nalyze | Find root cause (5 Whys). Distinguish symptom from cause. Determine if the fix is local or systemic. |
| **I**mprove | Generate and validate the output. Apply the fix. Confirm it meets the success criterion. |
| **C**ontrol | Ensure the fix is durable. Verify no regression. If relevant, leave traceability so future work can audit the change. |

---

## Graded Execution — DMAIC Depth by Defect Type

Not every task needs the full DMAIC cycle. Apply depth based on defect classification:

| Defect Type | DMAIC Depth | When to Stop |
|---|---|---|
| **Syntax / Format** | Define → Measure → (skip Analyze) → Improve → Control | Compile/parse error, formatting, typo. Fix it, verify it compiles. |
| **Logic** | Full DMAIC | Wrong condition, off-by-one, incorrect transformation. Must Analyze root cause before fixing. |
| **Missing edge case** | Full DMAIC | Empty input, timeout, boundary condition. Measure the gap first, then Analyze why it was missed. |
| **Design / Architecture** | Measure → Analyze → **Present to user** | Tight coupling, wrong abstraction. Do not Improve without user decision. |
| **Regression** | Full DMAIC + Identify what changed | Previously working feature broken. Must find the commit/code that introduced it, then roll back or fix forward. |

> If unsure about the classification, default to **full DMAIC**. Skipping Analyze on a logic defect is the most common source of repeat failures.

---

## Standard Interaction Flow (Full DMAIC)

1. **Define** — Receive user input, clarify scope (ask if ambiguous), write down the success criterion
2. **Measure** — Read context, check files, run `git diff`, inspect logs, capture the current "as-is" state
3. **Analyze** — Identify root cause (5 Whys). Is it a typo? Type mismatch? Logic error? Missing edge case? Design flaw?
4. **Improve** — Generate output artifact. Apply the fix. Validate against the success criterion.
5. **Control** — Verify no side effects. Check that the fix doesn't break other paths. Report both the fix and the evidence.

> If any phase cannot be completed (e.g., insufficient data to Measure, ambiguous root cause in Analyze), **pause and report** rather than guessing.

---

## Extended Flows

### Auto-Retry on Validation Failure (Improve 闭环)

When the Improve phase produces a defect:

1. **Analyze the defect**: Was it a wrong assumption (Define gap)? Missing data (Measure gap)? Or a wrong fix (Improve gap)?
2. Backtrack to the failed phase, correct it
3. Re-attempt Improve
4. After **3 cycles**, escalate to the user with:
   - What was attempted
   - Which DMAIC phase kept failing
   - A hypothesis on what additional information or authority is needed

### Exploratory / Investigation Tasks (Measure + Analyze 为主)

When the goal is diagnosis rather than output:

1. **Define** — What question are we answering? What would "done" look like?
2. **Measure** — Collect logs, metrics, state dumps, reproduce steps
3. **Analyze** — Formulate and test hypotheses. Use data to rule out possibilities, converge on root cause.
4. **Improve** — Present the finding with supporting evidence and, if appropriate, a recommended action.
5. **Control** — If a recommendation is accepted, verify it resolved the issue.

### Destructive Operations Requiring Confirmation (Control Gate)

Any operation that modifies, deletes, or overwrites existing data is a **Control gate**:

1. **Define** — What exactly will change? What is the rollback plan?
2. **Measure** — Capture the current state (backup, snapshot, git stash)
3. Present the full impact summary to the user
4. Wait for explicit confirmation before executing Improve
5. **Control** — After execution, verify the result matches the expected state

### Cross-Step Backtracking (DMAIC Loopback)

A failure in any phase means the chain is broken:

1. Identify which DMAIC phase produced the faulty input
2. Roll back to that phase with full context
3. Re-execute from that phase forward
4. Re-validate at Control

---

## Six Sigma Toolkit for Code & Config Tasks

### 5 Whys (Root Cause Analysis)

Ask "why" iteratively until the root cause is found:

```
Bug: Button click crashes the app
  Why? → Null pointer in onClick handler
    Why? → user object is null
      Why? → API response missing user field
        Why? → Backend changed the response schema
          Why? → Schema change wasn't communicated or versioned
```

The 3rd "Why" is often where the fix lives. The 5th "Why" is where the systemic fix lives.

### SIPOC (Scope Clarification)

Before starting a complex task, define:

- **S**uppliers — Input sources (files, APIs, user input)
- **I**nputs — What data goes into this task
- **P**rocess — The steps to transform inputs to outputs
- **O**utputs — What will be produced (code, doc, config)
- **C**ustomers — Who consumes the output (user, another system, another step)

### Control Checklist

Before delivering any output:

- [ ] Does the output satisfy the success criterion defined in Define?
- [ ] Has the "as-is" state been captured before the change?
- [ ] Have all affected paths been checked (not just the happy path)?
- [ ] Are there any new error states introduced?
- [ ] Is there a way to roll back if needed?

---

## Validation Rules

- **Code**: Must compile/parse. Test with provided test cases if available.
- **Configuration**: Must match expected schema and format.
- **Filesystem operations**: Verify the expected file was created/updated at the correct path.
- **External commands**: Confirm the command ran successfully (check exit code, output).

## Unverifiable Output

If an output **cannot be validated** (no test cases, no schema, no way to verify correctness):

> ⚠️ **This output has not been verified.** Review carefully before use in production.

This is a **Control failure** — document why validation was impossible so the Define phase can address it next time.

Always clearly state when validation was skipped or is incomplete. Do not silently produce unverified output.
