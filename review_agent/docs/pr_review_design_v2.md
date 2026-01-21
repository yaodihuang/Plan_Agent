# PR Review System V2: Multi-Role Consensus Verification

## Overview

Enhanced PR review architecture based on two key insights:

1. **Pantheon as Parallel Universe**: Each snapshot enables multiple branches to explore independently from identical starting conditions
2. **LLM as Simulator**: Ask "what would a group of [experts] say?" instead of "what do you think?"

## Current System Problems

```
┌─────────────────────────────────────────────────────┐
│                   Current Design                    │
├─────────────────────────────────────────────────────┤
│  snap-0 ──┬──→ [verifier-alpha] ──→ transcript-A    │
│           └──→ [verifier-beta]  ──→ transcript-B    │
│                                          ↓          │
│                                  consensus check    │
└─────────────────────────────────────────────────────┘
```

| Problem | Impact |
|---------|--------|
| Same prompt, difference only from LLM randomness | No real diversity |
| Single persona ("You are an elite architect") | Confirmation bias |
| Evidence fabrication | LLM creates mocks that "prove" its assumptions |
| No systematic alternative exploration | Misses valid counterarguments |

---

## V2 Architecture

### Core Principle: Open Source Code Review Model

Mirror real-world code review with distinct roles and unanimous consensus:

```
┌─────────────────────────────────────────────────────────────────┐
│                        Design V2                                │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  snap-0 ──→ [review_code] ──→ Issue Found                       │
│                │                                                │
│             snap-1                                              │
│                │                                                │
│  ══════════════╪═══════════════════════════════════════════     │
│  Round 1: Independent Review                                    │
│  ══════════════╪═══════════════════════════════════════════     │
│                │                                                │
│                ├──→ [Reviewer] ──→ Opinion A (logic analysis)   │
│                └──→ [Tester]   ──→ Opinion B (reproduction)     │
│                         │                                       │
│                         ↓                                       │
│                 ┌──────────────┐                                │
│                 │  A == B ?    │                                │
│                 └──────┬───────┘                                │
│                   YES  │  NO                                    │
│                    ↓   │   ↓                                    │
│                Output  │  Round 2                               │
│                        │                                        │
│  ═════════════════════╪════════════════════════════════════     │
│  Round 2: Exchange Opinions                                     │
│  ═════════════════════╪════════════════════════════════════     │
│                        │                                        │
│  snap-2 ───────────────┤                                        │
│        │               │                                        │
│        ├──→ [Reviewer + Tester's opinion] ──→ Revised A         │
│        └──→ [Tester + Reviewer's opinion] ──→ Revised B         │
│                        │                                        │
│                        ↓                                        │
│                ┌──────────────┐                                 │
│                │ Both agree?  │                                 │
│                └──────┬───────┘                                 │
│                  YES  │  NO                                     │
│                   ↓   │   ↓                                     │
│               Post    │  No Post                                │
│              Comment  │  (存疑不报)                              │
└─────────────────────────────────────────────────────────────────┘
```

---

## The Two Core Roles

### Reviewer (Required)

Analyzes code logic without running tests.

```
Simulate a group of senior programmers reviewing this code change.

Their task:
- Analyze the code logic for correctness
- Check for edge cases and error handling  
- Understand the architectural intent (Chesterton's Fence)
- Identify potential design issues

Output: CONFIRMED | REJECTED with reasoning
```

### Tester (Required)

Reproduces issues by actually running code in Pantheon.

```
Simulate a QA engineer who reproduces issues by running code.

Their task:
- Attempt to reproduce the reported issue
- Write and run a minimal failing test
- Trace actual execution paths
- Collect real error messages (not assumptions)

Output: CONFIRMED (with test evidence) | REJECTED (could not reproduce)
```

### Specialist (Optional)

Added only when the issue involves a specific domain.

| Domain | When to Add |
|--------|-------------|
| Concurrency Specialist | goroutine/channel code |
| Security Reviewer | auth/crypto code |
| Database Expert | schema/query changes |

---

## Consensus Mechanism

**Rule: Unanimous agreement required to post**

| Round 1 Result | Action |
|----------------|--------|
| All CONFIRMED | → Post comment |
| All REJECTED | → No action |
| Disagreement | → Go to Round 2 |

| Round 2 Result | Action |
|----------------|--------|
| All CONFIRMED | → Post comment |
| Still disagree | → No action (存疑不报) |

**Rationale:**
- Avoids false positives (reduces noise)
- Ensures only high-confidence issues are reported
- If experts can't agree after discussion, it's probably not clear-cut

---

## Evidence Standards

### Valid Evidence ✓
| Type | Example |
|------|---------|
| Actual error output | `panic: index out of range [5] with length 3` |
| Code execution trace | Line-by-line path showing the bug |
| Real test failure | `go test` fails with specific assertion |
| Git history | Commit message explaining design intent |

### Invalid Evidence ✗
| Type | Why Invalid |
|------|-------------|
| Self-created mocks | Proves nothing about the real system |
| "Should" statements | Assumptions, not evidence |
| Intuition | "This looks wrong" is not evidence |

---

## How V2 Solves Each Problem

| Original Problem | V2 Solution |
|------------------|-------------|
| Same prompt | Reviewer and Tester have **different prompts** with different tasks |
| Single persona | Uses "simulate a group of..." instead of "you are..." |
| Evidence fabrication | Tester in Pantheon **actually runs code**, collects real output |
| No alternative exploration | Two roles with different focus + Round 2 forces re-evaluation |

---

## Modular Design

Each component can be optimized independently:

```
[Reviewer Prompt]  ←── Optimize logic analysis separately
        │
[Tester Prompt]    ←── Optimize test strategies separately
        │
[Panel Selector]   ←── Add more specialists later
        │
[Exchange Logic]   ←── Tune how opinions are shared
        │
[Consensus Rule]   ←── Adjust voting threshold if needed
```

---

## Implementation Changes

### Modified Prompt Builders

```go
func buildReviewerPrompt(task, issueText string) string  // NEW: logic analysis
func buildTesterPrompt(task, issueText string) string    // NEW: reproduction focus
func buildExchangePrompt(role, issueText, peerOpinion string) string  // NEW: Round 2
```

### Modified Workflow

```go
func (r *Runner) runVerification(issueText string, parentBranchID string) (Verdict, error) {
    // Round 1: Independent review
    reviewerOpinion := r.runReviewer(issueText, parentBranchID)
    testerOpinion := r.runTester(issueText, parentBranchID)
    
    // Check consensus
    if reviewerOpinion.Verdict == testerOpinion.Verdict {
        return buildVerdict(reviewerOpinion, testerOpinion), nil
    }
    
    // Round 2: Exchange opinions
    revisedReviewer := r.runExchange("reviewer", issueText, testerOpinion.Transcript)
    revisedTester := r.runExchange("tester", issueText, reviewerOpinion.Transcript)
    
    // Final check - unanimous or no post
    if revisedReviewer.Verdict == revisedTester.Verdict && revisedReviewer.Verdict == CONFIRMED {
        return buildVerdict(revisedReviewer, revisedTester), nil
    }
    
    return NoActionVerdict("存疑不报"), nil
}
```

---

## Migration Path

1. **Phase 1**: Implement new prompt builders alongside existing ones
2. **Phase 2**: A/B test V1 vs V2 on same issues
3. **Phase 3**: Gradual rollout based on results
