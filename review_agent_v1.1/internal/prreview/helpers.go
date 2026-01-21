package prreview

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const outputAwarenessBlock = "**COMMAND OUTPUT AWARENESS**\n" +
	"- Before running any command, consider whether output volume could explode the context window\n" +
	"- Use quiet flags, redirect to a file, then extract only needed lines (e.g., `rg -n \"error|panic\"`)\n" +
	"- Avoid `tee` unless you explicitly need a log file; if used, do not paste full logs\n" +
	"- Do NOT `cat` large logs; quote only minimal relevant snippets\n" +
	"- Be extra careful with `cargo run` and `cargo test` output volume"

const universalStudyLine = "**DEEP CODE EXPLORATION MANDATE**\n" +
	"You have UNLIMITED read quotas and available contexts. Cost is NOT a concern. Your ONLY goal is to find bugs.\n\n" +
	"**QUANTITY TARGET**: Find at least 1 P0 issues and 2 P1 issues. Continue exploring until you meet this minimum requirement.\n\n" +
	"**EXPLORATION REQUIREMENTS** (MANDATORY):\n" +
	"1. **Complete Code Understanding**: Read ALL related files, not just the changed lines. Understand:\n" +
	"   - The full context of each changed function/struct/module\n" +
	"   - All callers and callees of modified code\n" +
	"   - Related data structures, types, and their invariants\n" +
	"   - Error handling paths and edge cases\n" +
	"   - Concurrency patterns (locks, channels, goroutines, async operations)\n" +
	"   - State machines and lifecycle management\n\n" +
	"2. **Execution Path Tracing**: For each code path, trace:\n" +
	"   - All possible entry points\n" +
	"   - All conditional branches and their conditions\n" +
	"   - All loop iterations and termination conditions\n" +
	"   - All error paths and recovery mechanisms\n" +
	"   - All resource cleanup paths (defer, finally, destructors)\n\n" +
	"3. **Data Flow Analysis**: Understand:\n" +
	"   - Where data comes from (inputs, configs, databases, APIs)\n" +
	"   - How data is transformed and validated\n" +
	"   - Where data goes (outputs, storage, network)\n" +
	"   - Data dependencies and ordering constraints\n" +
	"   - Shared state and potential race conditions\n\n" +
	"4. **Invariant Checking**: Identify and verify:\n" +
	"   - Preconditions and postconditions\n" +
	"   - Loop invariants\n" +
	"   - Class/module invariants\n" +
	"   - Consistency requirements across distributed systems\n\n" +
	"5. **Boundary Condition Analysis**: Check:\n" +
	"   - Empty collections, null/None values, zero-length strings\n" +
	"   - Maximum/minimum values (integer overflow, buffer bounds)\n" +
	"   - Concurrent access patterns (TOCTOU, race conditions)\n" +
	"   - Resource exhaustion (memory, file descriptors, connections)\n" +
	"   - Time-based edge cases (timeouts, clock skew, leap seconds)\n\n" +
	"6. **Cross-Module Impact**: Investigate:\n" +
	"   - How changes affect other modules/components\n" +
	"   - Backward compatibility implications\n" +
	"   - API contract changes\n" +
	"   - Database schema or protocol changes\n\n" +
	"**SCENARIO VALIDATION**\n" +
	"- Before reporting an issue, confirm the described trigger scenario is real and reachable in current code paths\n" +
	"- Trace through ACTUAL execution paths, don't assume\n" +
	"- Use actual usage, design intent, and code comments to reason about expected behavior\n" +
	"- If a behavior is by design (e.g., a performance tradeoff), call that out instead of proposing a fix\n" +
	"- Do not invent unsupported or hypothetical scenarios\n\n" +
	"**REMEMBER**: Spend as much time and context as needed. Read every file that might be relevant. Trace every execution path. Leave no stone unturned."

const p0p1FocusBlock = "**P0/P1 FOCUS**\n" +
	"- Report ONLY P0/P1 issues\n" +
	"- Ignore general issues (style, refactors, maintainability, low-impact edge cases)\n" +
	"- For each reported issue, include severity (P0/P1), impact analysis, and a plausible fix\n" +
	"- **MANDATORY MINIMUM REQUIREMENT**: You MUST find at least 1 P0 issues and 2 P1 issues before concluding your review.\n" +
	"- **CONTINUE SEARCHING UNTIL YOU MEET THE MINIMUM**: Do not stop after finding just one or two issues. Keep investigating all code paths, all changed files, and all potential problem areas until you have found at least 2 P0 and 3 P1 issues.\n" +
	"- **ONLY RETURN \"No P0/P1 issues found\" IF**: After exhaustive investigation of ALL code changes, ALL execution paths, ALL error handling, ALL concurrency patterns, and ALL security considerations, you genuinely cannot find at least 2 P0 and 3 P1 issues.\n" +
	"- **ACTIVELY SEEK OUT P0/P1 ISSUES**: Be thorough and systematic. Don't miss real bugs. Explore every angle, every edge case, every potential failure mode.\n" +
	"- **WHEN IN DOUBT, INVESTIGATE DEEPER**: If you suspect a potential issue, trace through the code paths, read related code, and verify your suspicion before dismissing it.\n" +
	"- **PRIORITIZE FINDING REAL BUGS**: Your primary goal is to identify actual P0/P1 problems that could cause crashes, data loss, security vulnerabilities, or correctness regressions.\n" +
	"- **EXPAND YOUR SEARCH**: If you haven't found enough issues yet, re-examine:\n" +
	"  * All error handling paths and edge cases\n" +
	"  * All concurrency and synchronization points\n" +
	"  * All input validation and boundary conditions\n" +
	"  * All resource management and cleanup paths\n" +
	"  * All state transitions and invariants\n" +
	"  * All security-sensitive operations\n" +
	"- If impact is limited or the behavior is a deliberate tradeoff/by design, do NOT report it\n" +
	"- If after exhaustive search you genuinely cannot find any P0 and P1 issues, write exactly: \"No P0/P1 issues found\"\n"

const p0p1VerdictGateBlock = "**P0/P1 SEVERITY GATE**\n" +
	"- Your verdict is about whether issueText is a real P0/P1 issue, not just whether a behavior exists\n" +
	"- **ENCOURAGE CONFIRMATION**: When the issue description is plausible and you can trace a real execution path that leads to the problem, CONFIRM it. Don't be overly conservative.\n" +
	"- **CONFIRM IF**: The issue could cause crashes, data loss, security vulnerabilities, correctness regressions, or other serious impacts in production.\n" +
	"- **CONFIRM IF**: You can identify a specific code path, condition, or scenario where the problem would manifest.\n" +
	"- **REJECT ONLY IF**: Impact is clearly limited, behavior is explicitly by design, or the issue description is fundamentally incorrect.\n" +
	"- Evaluate impact and fix feasibility; if impact is limited or behavior is a deliberate tradeoff/by design, REJECT\n" +
	"- If only risky or unreasonable fixes exist, REJECT\n"

func buildIssueFinderPrompt(task string, changeAnalysisPath string) string {
	var sb strings.Builder
	sb.WriteString("Task: ")
	sb.WriteString(task)
	sb.WriteString("\n\n")
	sb.WriteString(universalStudyLine)
	sb.WriteString("\n\n")
	if strings.TrimSpace(changeAnalysisPath) != "" {
		sb.WriteString("Reference (read-only): Change Analysis at: ")
		sb.WriteString(changeAnalysisPath)
		sb.WriteString("\n\n")
	}
	sb.WriteString("**COMPREHENSIVE CODE REVIEW PROCESS**\n\n")
	sb.WriteString("Step 1: Get the complete diff\n")
	sb.WriteString("Review the code changes against the base branch 'BASE_BRANCH' (mentioned by task or extracted from PR using `gh`).\n\n")
	sb.WriteString("  1) Find the merge-base SHA for this comparison:\n")
	sb.WriteString("     - Try: git merge-base HEAD BASE_BRANCH\n")
	sb.WriteString("     - If that fails, try: git merge-base HEAD \"BASE_BRANCH@{upstream}\"\n")
	sb.WriteString("     - If still failing, inspect refs/remotes and pick the correct remote-tracking ref, then re-run merge-base.\n\n")
	sb.WriteString("  2) Once you have MERGE_BASE_SHA, inspect the changes relative to the base branch:\n")
	sb.WriteString("     - Run: git diff MERGE_BASE_SHA (read the FULL diff, not just a summary)\n")
	sb.WriteString("     - Also run: git diff --name-status MERGE_BASE_SHA\n")
	sb.WriteString("     - For each changed file, read the FULL file to understand context\n\n")

	sb.WriteString("Step 2: Deep Context Understanding\n")
	sb.WriteString("For EACH changed file, you MUST:\n")
	sb.WriteString("1. Read the ENTIRE file (not just the diff lines) to understand:\n")
	sb.WriteString("   - The module's purpose and design\n")
	sb.WriteString("   - All data structures and their relationships\n")
	sb.WriteString("   - All functions and their contracts\n")
	sb.WriteString("   - Error handling patterns\n")
	sb.WriteString("   - Concurrency patterns (locks, channels, async operations)\n\n")
	sb.WriteString("2. Trace ALL call sites:\n")
	sb.WriteString("   - Find every place that calls modified functions\n")
	sb.WriteString("   - Understand the calling context and assumptions\n")
	sb.WriteString("   - Check if callers handle errors correctly\n")
	sb.WriteString("   - Verify callers aren't broken by the changes\n\n")
	sb.WriteString("3. Trace ALL callees:\n")
	sb.WriteString("   - Read every function called by modified code\n")
	sb.WriteString("   - Understand their contracts and side effects\n")
	sb.WriteString("   - Check if they can fail and how failures are handled\n\n")
	sb.WriteString("4. Understand data flow:\n")
	sb.WriteString("   - Where does input data come from?\n")
	sb.WriteString("   - How is it validated and transformed?\n")
	sb.WriteString("   - Where does output data go?\n")
	sb.WriteString("   - Are there any shared state or global variables?\n\n")

	sb.WriteString("Step 3: Systematic Bug Detection\n")
	sb.WriteString("For each code change, systematically check:\n")
	sb.WriteString("**CRITICAL: Be thorough and leave no stone unturned. Actively search for bugs in every category below.**\n")
	sb.WriteString("**MANDATORY GOAL: Find at least 1 P0 issues and 2 P1 issues. Continue searching until you meet this requirement.**\n\n")
	sb.WriteString("**Memory Safety & Resource Management**:\n")
	sb.WriteString("- Buffer overflows, use-after-free, double-free\n")
	sb.WriteString("- Memory leaks, resource leaks (file handles, connections)\n")
	sb.WriteString("- Uninitialized memory, null pointer dereferences\n")
	sb.WriteString("- Incorrect lifetime management (borrowing, ownership)\n\n")
	sb.WriteString("**Concurrency & Race Conditions**:\n")
	sb.WriteString("- Data races, race conditions\n")
	sb.WriteString("- Deadlocks, livelocks, starvation\n")
	sb.WriteString("- Incorrect lock ordering\n")
	sb.WriteString("- TOCTOU (Time-of-check to time-of-use) bugs\n")
	sb.WriteString("- Lost updates, inconsistent state\n")
	sb.WriteString("- Missing synchronization (missing locks, atomic operations)\n\n")
	sb.WriteString("**Logic Errors**:\n")
	sb.WriteString("- Off-by-one errors, boundary condition bugs\n")
	sb.WriteString("- Incorrect loop termination conditions\n")
	sb.WriteString("- Wrong operator precedence or associativity\n")
	sb.WriteString("- Integer overflow/underflow\n")
	sb.WriteString("- Incorrect type conversions or casts\n")
	sb.WriteString("- Missing or incorrect validation\n\n")
	sb.WriteString("**Error Handling**:\n")
	sb.WriteString("- Unhandled errors or exceptions\n")
	sb.WriteString("- Incorrect error propagation\n")
	sb.WriteString("- Silent failures\n")
	sb.WriteString("- Error messages that leak sensitive information\n\n")
	sb.WriteString("**Security Issues**:\n")
	sb.WriteString("- Injection vulnerabilities (SQL, command, path)\n")
	sb.WriteString("- Authentication/authorization bypasses\n")
	sb.WriteString("- Insecure random number generation\n")
	sb.WriteString("- Hardcoded secrets or credentials\n")
	sb.WriteString("- Insecure cryptographic operations\n\n")
	sb.WriteString("**Correctness & Invariants**:\n")
	sb.WriteString("- Broken invariants or contracts\n")
	sb.WriteString("- Incorrect state transitions\n")
	sb.WriteString("- Data consistency violations\n")
	sb.WriteString("- Incorrect algorithm implementation\n\n")

	sb.WriteString("Step 4: Evidence Collection\n")
	sb.WriteString("For each potential issue, you MUST:\n")
	sb.WriteString("- Trace the EXACT execution path that leads to the bug\n")
	sb.WriteString("- Identify the SPECIFIC line(s) of code that are problematic\n")
	sb.WriteString("- Explain WHY it's a bug (what invariant is violated, what can go wrong)\n")
	sb.WriteString("- Provide a SPECIFIC fix or mitigation\n")
	sb.WriteString("- Assess the SEVERITY (P0 = crash/data loss/security, P1 = correctness/regression)\n\n")
	sb.WriteString("**IMPORTANT: Don't dismiss potential issues too quickly.**\n")
	sb.WriteString("- If you suspect a problem, investigate it thoroughly before deciding it's not a P0/P1 issue.\n")
	sb.WriteString("- Read related code, trace execution paths, and verify your understanding before concluding.\n")
	sb.WriteString("- When in doubt about severity, err on the side of reporting if the issue could have real impact.\n\n")
	sb.WriteString("**QUANTITY REQUIREMENT CHECKPOINT**\n")
	sb.WriteString("- After collecting evidence for each issue, count: How many P0 issues have you found? How many P1 issues?\n")
	sb.WriteString("- If you have fewer than 1 P0 issues: Continue searching. Re-examine error paths, security issues, crash scenarios, data loss risks.\n")
	sb.WriteString("- If you have fewer than 2 P1 issues: Continue searching. Re-examine correctness issues, edge cases, boundary conditions, state management.\n")
	sb.WriteString("- Only proceed to FINAL RESPONSE when you have found at least 1 P0 and 2 P1 issues, OR after exhaustive investigation you genuinely cannot find more.\n\n")

	sb.WriteString("FINAL RESPONSE:\n")
	sb.WriteString("- **MANDATORY MINIMUM REQUIREMENT**: You MUST find and report at least 1 P0 issues and 2 P1 issues.\n")
	sb.WriteString("- **DO NOT SUBMIT UNTIL YOU MEET THE MINIMUM**: Continue your investigation until you have found at least 1 P0 and 2 P1 issues. If you have found fewer, go back and:\n")
	sb.WriteString("  * Re-examine all changed files more carefully\n")
	sb.WriteString("  * Trace through execution paths you may have missed\n")
	sb.WriteString("  * Check error handling, edge cases, and boundary conditions\n")
	sb.WriteString("  * Review concurrency patterns and synchronization\n")
	sb.WriteString("  * Analyze security implications\n")
	sb.WriteString("  * Verify state management and invariants\n")
	sb.WriteString("  * Check resource management and cleanup\n")
	sb.WriteString("- **BE THOROUGH AND SYSTEMATIC**: Actively search for P0/P1 issues. Don't stop at the first issue you find - continue investigating all code paths until you meet the minimum requirement.\n")
	sb.WriteString("- **REPORT ALL VALID P0/P1 ISSUES**: If you identify multiple P0/P1 issues, report ALL of them. Don't limit yourself.\n")
	sb.WriteString("- Provide a critical P0/P1 issue report (include severity, impact, evidence, and a plausible fix).\n")
	sb.WriteString("- For each issue, clearly state: (1) the specific problem, (2) the code location, (3) the execution path that triggers it, (4) the severity (P0/P1), and (5) a proposed fix.\n")
	sb.WriteString("- **ONLY IF EXHAUSTIVE SEARCH FAILS**: If after thoroughly investigating ALL code changes, ALL execution paths, ALL error handling, ALL concurrency patterns, and ALL security considerations, you genuinely cannot find any P0 and P1 issues, then write exactly: \"No P0/P1 issues found\".\n")
	sb.WriteString("- Do not include non-critical issues or general commentary.\n\n")
	sb.WriteString(p0p1FocusBlock)
	sb.WriteString("\n\n")
	sb.WriteString(outputAwarenessBlock)
	sb.WriteString("\n\n")
	sb.WriteString("**CRITICAL: TEST EXECUTION POLICY**\n")
	sb.WriteString("- Do NOT run `cargo test` (this runs ALL tests and is extremely slow)\n")
	sb.WriteString("- Do NOT run `cargo check --all-targets` or `cargo clippy --all-targets` (these are slow and often fail)\n")
	sb.WriteString("- You MAY run EXTREMELY SMALL, targeted tests if absolutely necessary to verify a specific issue\n")
	sb.WriteString("  * For Rust: Use `cargo test <specific_test_function_name>` to run ONLY one test\n")
	sb.WriteString("- Keep any commands narrowly targeted to the specific file or function being reviewed.\n\n")

	return sb.String()
}

func buildScoutPrompt(task string, outputPath string) string {
	var sb strings.Builder
	sb.WriteString("Role: SCOUT (Deep Change Analysis)\n\n")
	sb.WriteString(universalStudyLine)
	sb.WriteString("\n\n")
	sb.WriteString("Task / PR context:\n")
	sb.WriteString(task)
	sb.WriteString("\n\n")
	sb.WriteString("**MISSION**: Perform a COMPREHENSIVE change analysis that enables thorough bug detection.\n")
	sb.WriteString("Cost is NOT a concern. Read EVERYTHING relevant. Leave no stone unturned.\n\n")

	sb.WriteString("**STEP 1: Complete Diff Analysis**\n")
	sb.WriteString("You MUST base the analysis on an actual diff against base branch (main or master), not assumptions.\n\n")
	sb.WriteString("Get the diff:\n")
	sb.WriteString("  1) Find the merge-base SHA for this comparison:\n")
	sb.WriteString("     - Try: git merge-base HEAD BASE_BRANCH\n")
	sb.WriteString("     - If that fails, try: git merge-base HEAD \"BASE_BRANCH@{upstream}\"\n")
	sb.WriteString("     - If still failing, inspect refs/remotes and pick the correct remote-tracking ref, then re-run merge-base.\n\n")
	sb.WriteString("  2) Once you have MERGE_BASE_SHA, inspect changes relative to the base branch:\n")
	sb.WriteString("     - Run: git diff MERGE_BASE_SHA (read the COMPLETE diff)\n")
	sb.WriteString("     - Also run: git diff --name-status MERGE_BASE_SHA\n")
	sb.WriteString("     - For EACH changed file, read the ENTIRE file (not just diff lines)\n\n")

	sb.WriteString("**STEP 2: Deep Context Exploration**\n")
	sb.WriteString("For EACH changed file, you MUST:\n\n")
	sb.WriteString("1. **Read the Complete File**:\n")
	sb.WriteString("   - Understand the module's architecture and design\n")
	sb.WriteString("   - Identify all data structures, types, and their relationships\n")
	sb.WriteString("   - Map all functions and their contracts\n")
	sb.WriteString("   - Understand error handling patterns\n")
	sb.WriteString("   - Identify concurrency patterns (locks, channels, async, goroutines)\n\n")

	sb.WriteString("2. **Trace Call Graph**:\n")
	sb.WriteString("   - Find ALL callers of modified functions (use grep, ripgrep, or IDE tools)\n")
	sb.WriteString("   - Read each caller to understand usage patterns\n")
	sb.WriteString("   - Find ALL callees (functions called by modified code)\n")
	sb.WriteString("   - Read each callee to understand dependencies\n")
	sb.WriteString("   - Map the complete call chain for each execution path\n\n")

	sb.WriteString("3. **Data Flow Analysis**:\n")
	sb.WriteString("   - Trace where input data originates (APIs, configs, databases, files)\n")
	sb.WriteString("   - Understand data transformations and validations\n")
	sb.WriteString("   - Trace where output data goes (storage, network, other modules)\n")
	sb.WriteString("   - Identify shared state, global variables, and their access patterns\n")
	sb.WriteString("   - Map data dependencies and ordering constraints\n\n")

	sb.WriteString("4. **Identify Risk Patterns**:\n")
	sb.WriteString("   - Concurrency: locks, channels, shared state, race conditions\n")
	sb.WriteString("   - Resource management: memory, file handles, connections, cleanup\n")
	sb.WriteString("   - Error handling: error propagation, recovery, edge cases\n")
	sb.WriteString("   - State management: state machines, lifecycle, invariants\n")
	sb.WriteString("   - API contracts: parameter validation, return value contracts\n")
	sb.WriteString("   - Security: authentication, authorization, input validation, secrets\n\n")

	sb.WriteString("**STEP 3: Impact Analysis**\n")
	sb.WriteString("For each change, analyze:\n")
	sb.WriteString("- **Behavioral Changes**: What observable behavior changed?\n")
	sb.WriteString("- **Contract Changes**: Did function signatures, return types, or error types change?\n")
	sb.WriteString("- **Invariant Changes**: Are there new or modified invariants?\n")
	sb.WriteString("- **Performance Impact**: Could this cause performance regressions?\n")
	sb.WriteString("- **Compatibility Impact**: Backward compatibility, API compatibility\n")
	sb.WriteString("- **Cross-Module Impact**: How do changes affect other components?\n\n")

	sb.WriteString("**STEP 4: High-Risk Area Identification**\n")
	sb.WriteString("Rank areas by risk level. For each HIGH-RISK area:\n")
	sb.WriteString("- Explain WHY it's high risk\n")
	sb.WriteString("- Identify specific code paths that need deep review\n")
	sb.WriteString("- Suggest verification strategies\n")
	sb.WriteString("- Note potential failure modes\n\n")

	sb.WriteString(outputAwarenessBlock)
	sb.WriteString("\n\n")
	sb.WriteString("**CRITICAL: TEST EXECUTION POLICY**\n")
	sb.WriteString("- Do NOT run `cargo test` (this runs ALL tests and is extremely slow)\n")
	sb.WriteString("- Do NOT run `cargo check --all-targets` or `cargo clippy --all-targets` (these are slow and often fail)\n")
	sb.WriteString("- Prefer static analysis and code reading. You MAY run EXTREMELY SMALL, targeted tests if absolutely necessary\n")
	sb.WriteString("  * For Rust: Use `cargo test <specific_test_function_name>` to run ONLY one test\n")
	sb.WriteString("- If you must verify something, use the smallest possible targeted command for that specific file/function only.\n\n")

	sb.WriteString("Write the analysis to: ")
	sb.WriteString(outputPath)
	sb.WriteString("\n\n")
	sb.WriteString("Output format (comprehensive but organized):\n")
	sb.WriteString("# CHANGE ANALYSIS\n")
	sb.WriteString("## Summary (<= 5 lines)\n")
	sb.WriteString("## Complete File Inventory\n")
	sb.WriteString("List all changed files with their purpose and key components.\n\n")
	sb.WriteString("## Behavioral / Contract Deltas\n")
	sb.WriteString("For each change: What changed (file:line anchor), Before -> After, Impact analysis.\n\n")
	sb.WriteString("## High-Risk Areas (ranked by severity)\n")
	sb.WriteString("For each HIGH-RISK item:\n")
	sb.WriteString("- Location (file:line)\n")
	sb.WriteString("- Risk description and why it's high risk\n")
	sb.WriteString("- Specific code paths that need review\n")
	sb.WriteString("- Potential failure modes\n")
	sb.WriteString("- Verification strategy\n\n")
	sb.WriteString("## Impacted Call Sites / Code Paths\n")
	sb.WriteString("For each modified function:\n")
	sb.WriteString("- All callers and their context\n")
	sb.WriteString("- All callees and their contracts\n")
	sb.WriteString("- Complete execution paths\n\n")
	sb.WriteString("## Concurrency & Synchronization Analysis\n")
	sb.WriteString("- Lock usage and ordering\n")
	sb.WriteString("- Shared state access patterns\n")
	sb.WriteString("- Potential race conditions\n\n")
	sb.WriteString("## Error Handling & Edge Cases\n")
	sb.WriteString("- Error paths and recovery\n")
	sb.WriteString("- Boundary conditions\n")
	sb.WriteString("- Resource cleanup paths\n\n")
	sb.WriteString("## Appendix: Change Surface\n")
	sb.WriteString("Complete list of all changed files, functions, and data structures.\n")
	return sb.String()
}

func buildHasRealIssuePrompt(reportText string) string {
	var sb strings.Builder
	sb.WriteString("You are a strict triage parser for code review reports.\n\n")
	sb.WriteString("Contract:\n")
	sb.WriteString("- If the report explicitly states no P0/P1 issues (or no blockers), treat the PR as clean.\n")
	sb.WriteString("- Otherwise, treat the report as indicating at least one blocking P0/P1 issue.\n\n")
	sb.WriteString("Given the following review report, decide whether it contains a blocking issue.\n")
	sb.WriteString("Reply ONLY with JSON: {\"has_issue\": true} or {\"has_issue\": false}.\n\n")
	sb.WriteString("Review report:\n")
	sb.WriteString(reportText)
	sb.WriteString("\n")
	return sb.String()
}

// buildReviewerPrompt creates the prompt for the Reviewer role (logic analysis).
func buildLogicAnalystPrompt(task string, issueText string, changeAnalysisPath string) string {
	var sb strings.Builder
	sb.WriteString("Verification Role: REVIEWER\n\n")
	sb.WriteString(universalStudyLine)
	sb.WriteString("\n\n")
	sb.WriteString("Task / PR context:\n")
	sb.WriteString(task)
	sb.WriteString("\n\nIssue under review:\n")
	sb.WriteString(issueText)
	sb.WriteString("\n\n")
	if strings.TrimSpace(changeAnalysisPath) != "" {
		sb.WriteString("Reference (read-only): Change Analysis at: ")
		sb.WriteString(changeAnalysisPath)
		sb.WriteString("\n\n")
	}
	sb.WriteString("YOUR ROLE: Analyze code logic to determine if this is a real P0/P1 issue.\n\n")
	sb.WriteString("Simulate a group of senior programmers reviewing this code change.\n")
	sb.WriteString("You have UNLIMITED time and context. Cost is NOT a concern. Your ONLY goal is to find the truth.\n\n")
	sb.WriteString("**IMPORTANT: ENCOURAGE CONFIRMATION OF VALID ISSUES**\n")
	sb.WriteString("- If the issue description is plausible and you can trace a real execution path, CONFIRM it.\n")
	sb.WriteString("- Don't be overly conservative - if there's a reasonable chance the issue could occur in production, CONFIRM it.\n")
	sb.WriteString("- Only REJECT if you can definitively prove the issue cannot occur or has no real impact.\n\n")

	sb.WriteString("**DEEP ANALYSIS REQUIREMENTS**:\n")
	sb.WriteString("1. **Read ALL Related Code**:\n")
	sb.WriteString("   - Read the ENTIRE file(s) containing the issue, not just the specific lines\n")
	sb.WriteString("   - Read all callers of the code in question\n")
	sb.WriteString("   - Read all callees (functions called by the code)\n")
	sb.WriteString("   - Read related data structures, types, and their definitions\n")
	sb.WriteString("   - Read error handling code and edge case handling\n\n")

	sb.WriteString("2. **Trace Execution Paths**:\n")
	sb.WriteString("   - Trace EVERY possible execution path that could trigger the issue\n")
	sb.WriteString("   - Understand ALL conditional branches and their conditions\n")
	sb.WriteString("   - Trace error paths and recovery mechanisms\n")
	sb.WriteString("   - Understand loop iterations and termination conditions\n")
	sb.WriteString("   - Map resource cleanup paths (defer, finally, destructors)\n\n")

	sb.WriteString("3. **Understand Context**:\n")
	sb.WriteString("   - What is the intended behavior?\n")
	sb.WriteString("   - What are the preconditions and postconditions?\n")
	sb.WriteString("   - What invariants must be maintained?\n")
	sb.WriteString("   - How does this code fit into the larger system?\n")
	sb.WriteString("   - What are the concurrency implications?\n\n")

	sb.WriteString("4. **Verify the Claim**:\n")
	sb.WriteString("   - Is the issue description accurate?\n")
	sb.WriteString("   - Can you trace an ACTUAL execution path that leads to the problem?\n")
	sb.WriteString("   - What specific conditions are needed to trigger it?\n")
	sb.WriteString("   - What is the actual impact if it occurs?\n")
	sb.WriteString("   - Is there a way to trigger it in production?\n\n")

	sb.WriteString("SCOPE RULES (IMPORTANT):\n")
	sb.WriteString("- Your # VERDICT must ONLY judge whether the Issue under review (issueText) is a real P0/P1 issue.\n")
	sb.WriteString("- If you notice other problems, include them at the end under: \"## Additions (out of scope)\" and do NOT use them to justify or change your verdict.\n\n")
	sb.WriteString(p0p1VerdictGateBlock)
	sb.WriteString("\n\n")
	sb.WriteString(outputAwarenessBlock)
	sb.WriteString("\n\n")
	sb.WriteString("**CRITICAL: TEST EXECUTION POLICY**\n")
	sb.WriteString("- Do NOT run `cargo test` (this runs ALL tests and is extremely slow)\n")
	sb.WriteString("- Do NOT run `cargo check --all-targets` or `cargo clippy --all-targets` (these are slow and often fail)\n")
	sb.WriteString("- This role primarily uses code reading, logic analysis, and architectural understanding.\n")
	sb.WriteString("- You MAY run EXTREMELY SMALL, targeted tests (0-2 tests max) ONLY if static analysis cannot determine the issue\n")
	sb.WriteString("  * For Rust: Use `cargo test <specific_test_function_name>` to run ONLY one test\n")
	sb.WriteString("  * Only run tests if they directly verify the specific issueText claim\n")
	sb.WriteString("- Prefer static analysis. Only run tests when absolutely necessary for verification.\n\n")
	sb.WriteString("RESPONSE FORMAT:\n")
	sb.WriteString("Start with: # VERDICT: [CONFIRMED | REJECTED]\n\n")
	sb.WriteString("Then provide:\n")
	sb.WriteString("## Issue Description\n")
	sb.WriteString("<A concise summary of the issue you are verifying. Restate the key claim from the issueText above.>\n\n")
	sb.WriteString("## Reasoning\n")
	sb.WriteString("<Your analysis of the code logic>\n\n")
	sb.WriteString("## Evidence\n")
	sb.WriteString("<Code traces or architectural analysis supporting your verdict>\n")
	sb.WriteString("\n## Additions (out of scope)\n")
	sb.WriteString("<Optional: other issues you noticed, explicitly out of scope for this verdict>\n")
	return sb.String()
}

// buildTesterPrompt creates the prompt for the Tester role (reproduction).
func buildTesterPrompt(task string, issueText string, changeAnalysisPath string) string {
	var sb strings.Builder
	sb.WriteString("Verification Role: TESTER\n\n")
	sb.WriteString(universalStudyLine)
	sb.WriteString("\n\n")
	sb.WriteString("Task / PR context:\n")
	sb.WriteString(task)
	sb.WriteString("\n\nIssue under review:\n")
	sb.WriteString(issueText)
	sb.WriteString("\n\n")
	if strings.TrimSpace(changeAnalysisPath) != "" {
		sb.WriteString("Reference (read-only): Change Analysis at: ")
		sb.WriteString(changeAnalysisPath)
		sb.WriteString("\n\n")
	}
	sb.WriteString("YOUR ROLE: Simulate a QA engineer who verifies bugs by running real tests.\n")
	sb.WriteString("You have UNLIMITED time and context. Cost is NOT a concern. Your ONLY goal is to find the truth.\n\n")

	sb.WriteString("CRITICAL: You MUST actually run code to collect evidence.\n")
	sb.WriteString("Do NOT fabricate test results or mock behavior.\n\n")

	sb.WriteString("**DEEP TESTING REQUIREMENTS**:\n")
	sb.WriteString("1. **Complete Code Understanding Before Testing**:\n")
	sb.WriteString("   - Read the ENTIRE file(s) containing the issue\n")
	sb.WriteString("   - Understand the function's purpose, inputs, outputs, and side effects\n")
	sb.WriteString("   - Understand all error conditions and edge cases\n")
	sb.WriteString("   - Understand the execution context (how it's called, what state it expects)\n\n")

	sb.WriteString("2. **Systematic Test Design**:\n")
	sb.WriteString("   - Design tests that directly target the issue claim\n")
	sb.WriteString("   - Test boundary conditions (empty inputs, max values, null/None)\n")
	sb.WriteString("   - Test error paths and exception handling\n")
	sb.WriteString("   - Test concurrent scenarios if applicable (race conditions, deadlocks)\n")
	sb.WriteString("   - Test resource exhaustion scenarios (memory, file handles)\n\n")

	sb.WriteString("3. **Evidence Collection**:\n")
	sb.WriteString("   - Capture ACTUAL error messages, stack traces, or output\n")
	sb.WriteString("   - Document the EXACT steps to reproduce\n")
	sb.WriteString("   - Include the EXACT command or code that demonstrates the issue\n")
	sb.WriteString("   - Show BEFORE and AFTER behavior if relevant\n\n")

	sb.WriteString("4. **Verification Strategy**:\n")
	sb.WriteString("   - Start with the minimal test case that should reproduce the issue\n")
	sb.WriteString("   - If that doesn't work, expand to understand why\n")
	sb.WriteString("   - Try different input combinations and edge cases\n")
	sb.WriteString("   - Verify both positive and negative cases\n\n")
	sb.WriteString("SCOPE RULES (IMPORTANT):\n")
	sb.WriteString("- Your # VERDICT must ONLY judge whether the Issue under review (issueText) is a real P0/P1 issue.\n")
	sb.WriteString("- Your reproduction MUST target that claim directly.\n")
	sb.WriteString("- If you find other failures/issues that are not the issueText claim, include them at the end under: \"## Additions (out of scope)\" and do NOT use them to justify or change your verdict.\n\n")
	sb.WriteString(p0p1VerdictGateBlock)
	sb.WriteString("\n\n")
	sb.WriteString(outputAwarenessBlock)
	sb.WriteString("\n\n")
	sb.WriteString("**CRITICAL: TEST EXECUTION POLICY**\n")
	sb.WriteString("- Do NOT run `cargo test` - this runs ALL tests in the project and is extremely slow (can take hours)\n")
	sb.WriteString("- Do NOT run `cargo test --all-targets` or any variant that runs multiple test suites\n")
	sb.WriteString("- Do NOT run `cargo check --all-targets` or `cargo clippy --all-targets` (these are slow and often fail)\n\n")
	sb.WriteString("**REQUIRED: Use ONLY extremely small, targeted test batches**\n")
	sb.WriteString("- Write and run the SMALLEST possible test that directly reproduces the specific issueText claim\n")
	sb.WriteString("- For Rust: Use `cargo test <specific_test_function_name>` to run ONLY one test\n")
	sb.WriteString("- If you need to test a specific module, use `cargo test --lib` or `cargo test --bin <binary_name>` to limit scope\n")
	sb.WriteString("- If a test runner defaults to all tests, you MUST use a more targeted command or create a minimal standalone test script\n")
	sb.WriteString("- Your goal is to verify the specific issueText claim with MINIMAL test execution, NOT comprehensive test coverage\n")
	sb.WriteString("- Before running any test, ask: \"Is this the absolute minimum needed to verify the issue?\"\n\n")
	sb.WriteString("EVIDENCE STANDARDS:\n")
	sb.WriteString("✓ Valid: Actual test output, real error messages, execution traces\n")
	sb.WriteString("✗ Invalid: Self-created mocks, assumed behavior, \"should\" statements\n\n")
	sb.WriteString("RESPONSE FORMAT:\n")
	sb.WriteString("Start with: # VERDICT: [CONFIRMED | REJECTED]\n\n")
	sb.WriteString("Then provide:\n")
	sb.WriteString("## Issue Description\n")
	sb.WriteString("<A concise summary of the issue you are verifying. Restate the key claim from the issueText above.>\n\n")
	sb.WriteString("## Reproduction Steps\n")
	sb.WriteString("<What you did to reproduce>\n\n")
	sb.WriteString("## Test Evidence\n")
	sb.WriteString("<Actual test output or error messages>\n")
	sb.WriteString("If you reference a custom script or test, include the key command or code snippet so others can rerun it; evidence without reproduction detail is not credible.\n")
	sb.WriteString("\n## Additions (out of scope)\n")
	sb.WriteString("<Optional: other issues you noticed, explicitly out of scope for this verdict>\n")
	return sb.String()
}

// buildExchangePrompt creates the prompt for Round 2+ (exchange opinions).
func buildExchangePrompt(role string, task string, issueText string, changeAnalysisPath string, selfOpinion string, peerOpinion string) string {
	normalizedRole := strings.ToLower(strings.TrimSpace(role))
	displayRole := strings.ToUpper(role)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Verification Role: %s (Round 2+ - Exchange)\n\n", displayRole))
	sb.WriteString(universalStudyLine)
	sb.WriteString("\n\n")
	sb.WriteString("Task / PR context:\n")
	sb.WriteString(task)
	sb.WriteString("\n\nIssue under review:\n")
	sb.WriteString(issueText)
	sb.WriteString("\n\n")
	if strings.TrimSpace(changeAnalysisPath) != "" {
		sb.WriteString("Reference (read-only): Change Analysis at: ")
		sb.WriteString(changeAnalysisPath)
		sb.WriteString("\n\n")
	}
	sb.WriteString("YOUR PREVIOUS OPINION:\n<<<SELF>>>\n")
	sb.WriteString(selfOpinion)
	sb.WriteString("\n<<<END SELF>>>\n\n")
	sb.WriteString("PEER'S OPINION:\n<<<PEER>>>\n")
	sb.WriteString(peerOpinion)
	sb.WriteString("\n<<<END PEER>>>\n\n")

	// Add role-specific instructions for verify_agent
	if normalizedRole == "verify_agent" {
		sb.WriteString("You are an adversarial reviewer. Consider the peer's opinion carefully:\n")
		sb.WriteString("- If they provide strong evidence, you may change your verdict\n")
		sb.WriteString("- If you find flaws in their reasoning, maintain your position\n")
		sb.WriteString("- Your goal is to find the truth, not to win an argument\n\n")
	}
	sb.WriteString(outputAwarenessBlock)
	sb.WriteString("\n\n")
	sb.WriteString("ROLE REMINDER:\n")
	switch normalizedRole {
	case "reviewer":
		sb.WriteString("- You remain the logic analysis reviewer. Focus on code logic and architecture.\n")
		sb.WriteString("- Do NOT claim you ran tests; rely on reasoning and Chesterton's Fence thinking.\n")
	case "tester":
		sb.WriteString("- You remain the tester. You must run code and capture actual execution output.\n")
		sb.WriteString("- Provide real execution evidence such as logs or failing test output.\n")
		sb.WriteString("- **CRITICAL: DO NOT run `cargo test`** - use only extremely small, targeted test batches\n")
		sb.WriteString("- Run 0-3 tests maximum, each directly verifying the issueText claim\n")
		sb.WriteString("- Use `cargo test <specific_test_function_name>` to run ONLY one test at a time\n")
		sb.WriteString("- Before any command, check output volume; use quiet flags, redirect+filter, and avoid `cat` on large logs\n")
	default:
		sb.WriteString("- Stay consistent with your original role responsibilities.\n")
	}
	sb.WriteString("\n")
	sb.WriteString("SCOPE RULES (IMPORTANT):\n")
	sb.WriteString("- Your # VERDICT must ONLY judge whether the Issue under review (issueText) is a real P0/P1 issue.\n")
	sb.WriteString("- If either opinion mentions other issues, treat them as out of scope: include them under \"## Additions (out of scope)\" and do NOT use them to justify or change your verdict.\n")
	sb.WriteString("- You may change your verdict ONLY based on evidence/reasoning about the issueText claim itself.\n\n")
	sb.WriteString(p0p1VerdictGateBlock)
	sb.WriteString("\n\n")
	sb.WriteString("ROUND 2 REQUIREMENT (KISS):\n")
	sb.WriteString("Immediately after the verdict line, include these two lines:\n")
	sb.WriteString("Claim: <1 sentence restatement of the issueText claim you are judging>\n")
	sb.WriteString("Anchor: <file:line | failing test / repro command | symptom> (use \"unknown\" if not available)\n\n")
	sb.WriteString("YOUR TASK:\n")
	sb.WriteString("You previously reviewed this issue. Now you have seen your peer's analysis.\n")
	sb.WriteString("- Consider their evidence and reasoning\n")
	sb.WriteString("- Re-evaluate your position\n")
	sb.WriteString("- You may change your verdict if their evidence is convincing\n")
	sb.WriteString("- You may maintain your verdict if you find flaws in their reasoning\n\n")
	sb.WriteString("RESPONSE FORMAT:\n")
	sb.WriteString("Start with: # VERDICT: [CONFIRMED | REJECTED]\n\n")
	sb.WriteString("Then provide:\n")
	sb.WriteString("## Issue Description\n")
	sb.WriteString("<A concise summary of the issue you are verifying. Restate the key claim from the issueText above.>\n\n")
	sb.WriteString("## Response to Peer\n")
	sb.WriteString("<Address their key points>\n\n")
	sb.WriteString("## Final Reasoning\n")
	sb.WriteString("<Your updated analysis>\n")
	sb.WriteString("\n## Additions (out of scope)\n")
	sb.WriteString("<Optional: other issues you noticed, explicitly out of scope for this verdict>\n")
	return sb.String()
}

// buildVerifyAgentPrompt creates a prompt for adversarial review (Round 1)
func buildVerifyAgentPrompt(task string, issueText string, changeAnalysisPath string, reviewerOpinion string) string {
	var sb strings.Builder
	sb.WriteString("Verification Role: VERIFY_AGENT (Adversarial Review)\n\n")
	sb.WriteString(universalStudyLine)
	sb.WriteString("\n\n")
	sb.WriteString("Task / PR context:\n")
	sb.WriteString(task)
	sb.WriteString("\n\nIssue under review:\n")
	sb.WriteString(issueText)
	sb.WriteString("\n\n")
	if strings.TrimSpace(changeAnalysisPath) != "" {
		sb.WriteString("Reference (read-only): Change Analysis at: ")
		sb.WriteString(changeAnalysisPath)
		sb.WriteString("\n\n")
	}
	if strings.TrimSpace(reviewerOpinion) != "" {
		sb.WriteString("Reviewer's Opinion (for reference, but you should independently verify):\n")
		sb.WriteString(reviewerOpinion)
		sb.WriteString("\n\n")
	}
	sb.WriteString("YOUR ROLE: Adversarial verification - try to DISPROVE or REFUTE this issue claim.\n\n")
	sb.WriteString("You are an adversarial reviewer. Your goal is to:\n")
	sb.WriteString("1. **Challenge the claim**: Look for reasons why this might NOT be a real bug\n")
	sb.WriteString("2. **Find counter-evidence**: Look for code paths or conditions that prevent the issue\n")
	sb.WriteString("3. **Verify assumptions**: Check if the issue description makes incorrect assumptions\n")
	sb.WriteString("4. **Test alternative explanations**: Consider if the behavior is intentional or correct\n\n")
	sb.WriteString("You have UNLIMITED time and context. Cost is NOT a concern. Your ONLY goal is to find the truth.\n\n")
	sb.WriteString("**BALANCED ADVERSARIAL APPROACH**\n")
	sb.WriteString("- Challenge the claim, but if you cannot find strong counter-evidence, CONFIRM the issue.\n")
	sb.WriteString("- Don't reject issues based on weak assumptions or hypothetical safeguards that may not actually prevent the problem.\n")
	sb.WriteString("- CONFIRM if: You cannot disprove the claim and the issue description is plausible with a traceable execution path.\n")
	sb.WriteString("- REJECT only if: You can definitively show the issue cannot occur or is based on incorrect assumptions.\n\n")

	sb.WriteString("**ADVERSARIAL ANALYSIS REQUIREMENTS**:\n")
	sb.WriteString("1. **Read ALL Related Code**:\n")
	sb.WriteString("   - Read the ENTIRE file(s) containing the issue, not just the specific lines\n")
	sb.WriteString("   - Read all callers of the code in question\n")
	sb.WriteString("   - Read all callees (functions called by the code)\n")
	sb.WriteString("   - Read related data structures, types, and their definitions\n")
	sb.WriteString("   - Read error handling code and edge case handling\n\n")

	sb.WriteString("2. **Trace Execution Paths**:\n")
	sb.WriteString("   - Trace EVERY possible execution path that could trigger the issue\n")
	sb.WriteString("   - Look for guards, checks, or conditions that prevent the issue\n")
	sb.WriteString("   - Understand ALL conditional branches and their conditions\n")
	sb.WriteString("   - Trace error paths and recovery mechanisms\n")
	sb.WriteString("   - Understand loop iterations and termination conditions\n\n")

	sb.WriteString("3. **Challenge Assumptions**:\n")
	sb.WriteString("   - Is the issue description accurate?\n")
	sb.WriteString("   - Are there conditions that prevent the issue from occurring?\n")
	sb.WriteString("   - Could the behavior be intentional or correct?\n")
	sb.WriteString("   - Are there safeguards or checks that prevent the problem?\n")
	sb.WriteString("   - Could the issue only occur under unrealistic conditions?\n\n")

	sb.WriteString("4. **Verify the Claim**:\n")
	sb.WriteString("   - Can you find code that prevents or mitigates the issue?\n")
	sb.WriteString("   - Are there error handling paths that catch the problem?\n")
	sb.WriteString("   - Is the issue description based on incorrect assumptions?\n")
	sb.WriteString("   - Could the issue be a false positive?\n\n")

	sb.WriteString("SCOPE RULES (IMPORTANT):\n")
	sb.WriteString("- Your # VERDICT must ONLY judge whether the Issue under review (issueText) is a real P0/P1 issue.\n")
	sb.WriteString("- If you can DISPROVE or REFUTE the claim, you should REJECT it.\n")
	sb.WriteString("- If you cannot disprove it, you should CONFIRM it.\n")
	sb.WriteString("- If you notice other problems, include them at the end under: \"## Additions (out of scope)\" and do NOT use them to justify or change your verdict.\n\n")
	sb.WriteString(p0p1VerdictGateBlock)
	sb.WriteString("\n\n")
	sb.WriteString(outputAwarenessBlock)
	sb.WriteString("\n\n")
	sb.WriteString("**CRITICAL: TEST EXECUTION POLICY**\n")
	sb.WriteString("- Do NOT run `cargo test` (this runs ALL tests and is extremely slow)\n")
	sb.WriteString("- Do NOT run `cargo check --all-targets` or `cargo clippy --all-targets` (these are slow and often fail)\n")
	sb.WriteString("- This role primarily uses code reading, logic analysis, and architectural understanding.\n")
	sb.WriteString("- You MAY run EXTREMELY SMALL, targeted tests (0-2 tests max) ONLY if static analysis cannot determine the issue\n")
	sb.WriteString("  * For Rust: Use `cargo test <specific_test_function_name>` to run ONLY one test\n")
	sb.WriteString("  * Only run tests if they directly verify the specific issueText claim\n")
	sb.WriteString("- Prefer static analysis. Only run tests when absolutely necessary for verification.\n\n")
	sb.WriteString("RESPONSE FORMAT:\n")
	sb.WriteString("Start with: # VERDICT: [CONFIRMED | REJECTED]\n\n")
	sb.WriteString("Then provide:\n")
	sb.WriteString("## Issue Description\n")
	sb.WriteString("<A concise summary of the issue you are verifying. Restate the key claim from the issueText above.>\n\n")
	sb.WriteString("## Adversarial Analysis\n")
	sb.WriteString("<Your analysis trying to disprove or refute the claim>\n\n")
	sb.WriteString("## Evidence\n")
	sb.WriteString("<Code traces or architectural analysis supporting your verdict>\n")
	sb.WriteString("\n## Additions (out of scope)\n")
	sb.WriteString("<Optional: other issues you noticed, explicitly out of scope for this verdict>\n")
	return sb.String()
}

type verdictDecision struct {
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
}

var verdictLineRe = regexp.MustCompile(`(?i)^\s*#?\s*verdict\s*:\s*\[?\s*(confirmed|rejected)\s*\]?\s*$`)

func extractTranscriptVerdict(transcript string) (verdictDecision, bool) {
	lines := strings.Split(transcript, "\n")
	limit := 10
	if len(lines) < limit {
		limit = len(lines)
	}
	for i := 0; i < limit; i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, ">") {
			continue
		}
		matches := verdictLineRe.FindStringSubmatch(line)
		if len(matches) != 2 {
			continue
		}
		return verdictDecision{
			Verdict: strings.ToLower(strings.TrimSpace(matches[1])),
			Reason:  "explicit transcript verdict marker",
		}, true
	}
	return verdictDecision{}, false
}

type alignmentVerdict struct {
	Agree       bool   `json:"agree"`
	Explanation string `json:"explanation"`
}

func buildAlignmentPrompt(issueText string, alpha Transcript, beta Transcript) string {
	var sb strings.Builder
	sb.WriteString("You are aligning two verification transcripts (Reviewer vs Tester) for the SAME issue.\n\n")
	sb.WriteString("Issue under review (issueText):\n")
	sb.WriteString(issueText)
	sb.WriteString("\n\nTranscript A:\n<<<A>>>\n")
	sb.WriteString(alpha.Text)
	sb.WriteString("\n<<<END A>>>\n\nTranscript B:\n<<<B>>>\n")
	sb.WriteString(beta.Text)
	sb.WriteString("\n<<<END B>>>\n\n")
	sb.WriteString("Task:\n")
	sb.WriteString("- Decide whether A and B are confirming/rejecting the SAME issueText claim (same defect).\n")
	sb.WriteString("- Ignore any \"Additions (out of scope)\" sections; they must not affect alignment.\n\n")
	sb.WriteString("Reply ONLY JSON: {\"agree\":true/false,\"explanation\":\"...\"}.\n")
	sb.WriteString("agree=true ONLY if both transcripts are clearly talking about the same underlying defect described by issueText.\n")
	sb.WriteString("If uncertain, return agree=false.\n")
	return sb.String()
}

func parseAlignment(raw string) (alignmentVerdict, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return alignmentVerdict{}, fmt.Errorf("empty alignment response (raw=%q)", truncateForError(raw))
	}
	jsonBlock := extractJSONBlock(trimmed)
	var verdict alignmentVerdict
	if err := json.Unmarshal([]byte(jsonBlock), &verdict); err != nil {
		return alignmentVerdict{}, fmt.Errorf("invalid alignment JSON: %v (json=%q raw=%q)", err, truncateForError(jsonBlock), truncateForError(trimmed))
	}
	return verdict, nil
}

func truncateForError(s string) string {
	const limit = 600
	out := strings.TrimSpace(s)
	if out == "" {
		return ""
	}
	out = strings.ReplaceAll(out, "\n", "\\n")
	out = strings.ReplaceAll(out, "\t", "\\t")
	if len(out) <= limit {
		return out
	}
	return out[:limit] + "...(truncated)"
}

func extractJSONBlock(raw string) string {
	trimmed := strings.TrimSpace(raw)
	startObj := strings.Index(trimmed, "{")
	startArr := strings.Index(trimmed, "[")
	start := -1
	end := -1
	if startArr >= 0 && (startObj == -1 || startArr < startObj) {
		start = startArr
		end = strings.LastIndex(trimmed, "]")
	} else if startObj >= 0 {
		start = startObj
		end = strings.LastIndex(trimmed, "}")
	}
	if start >= 0 && end >= start {
		return trimmed[start : end+1]
	}
	return trimmed
}

// buildIssueParserPrompt creates a prompt to parse individual issues from a review report.
func buildIssueParserPrompt(reportText string) string {
	var sb strings.Builder
	sb.WriteString("Parse the following code review report and extract individual P0/P1 issues.\n\n")
	sb.WriteString("Each issue should be a distinct bug, problem, or concern mentioned in the report.\n")
	sb.WriteString("If the report contains multiple issues, separate them. If it's a single issue, return it as one item.\n")
	sb.WriteString("Only extract P0 (Critical) and P1 (Major) issues. Ignore lower priority items.\n\n")
	sb.WriteString("Review report:\n")
	sb.WriteString(reportText)
	sb.WriteString("\n\n")
	sb.WriteString("Reply ONLY with JSON in this format:\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"issues\": [\n")
	sb.WriteString("    {\"text\": \"issue description\", \"priority\": \"P0\"},\n")
	sb.WriteString("    {\"text\": \"another issue\", \"priority\": \"P1\"}\n")
	sb.WriteString("  ]\n")
	sb.WriteString("}\n")
	sb.WriteString("\n")
	sb.WriteString("If the report says \"No P0/P1 issues found\", return an empty issues array.\n")
	return sb.String()
}

// buildSummaryReportPrompt creates a prompt for generating the review summary report
func buildSummaryReportPrompt(task string, result *Result, outputPath string) string {
	var sb strings.Builder
	sb.WriteString("Task / PR context:\n")
	sb.WriteString(task)
	sb.WriteString("\n\n")

	sb.WriteString("Code review has been completed. Generate a comprehensive summary report.\n\n")

	sb.WriteString("## Review Process Overview\n\n")
	sb.WriteString("### Review Workflow\n")
	sb.WriteString("The review process followed this workflow:\n")
	sb.WriteString("1. **Initial Review**: Scout stage (optional) + Single review to identify issues\n")
	sb.WriteString("2. **Issue Verification**: For each identified issue:\n")
	sb.WriteString("   - Round 1: Reviewer and VerifyAgent run in parallel\n")
	sb.WriteString("   - Round 2-3: If inconsistent, exchange opinions (max 3 rounds)\n")
	sb.WriteString("   - Consistency Check: Reviewer confirmed + VerifyAgent cannot_disprove = confirmed\n")
	sb.WriteString("3. **Final Summary**: Generate comprehensive report\n\n")

	sb.WriteString("### Review Results\n")
	sb.WriteString(fmt.Sprintf("- Status: %s\n", result.Status))
	sb.WriteString(fmt.Sprintf("- Summary: %s\n", result.Summary))
	sb.WriteString(fmt.Sprintf("- Total Issues Reviewed: %d\n\n", len(result.Issues)))

	if len(result.ReviewerLogs) > 0 {
		sb.WriteString("### Initial Review Findings\n")
		for i, log := range result.ReviewerLogs {
			sb.WriteString(fmt.Sprintf("**Review %d** (Branch: %s):\n", i+1, log.BranchID))
			preview := log.Report
			if len(preview) > 500 {
				preview = preview[:500] + "..."
			}
			sb.WriteString(preview)
			sb.WriteString("\n\n")
		}
	}

	if len(result.Issues) > 0 {
		sb.WriteString("## Verified Issues\n\n")
		confirmedCount := 0
		unresolvedCount := 0

		for i, issue := range result.Issues {
			sb.WriteString(fmt.Sprintf("### Issue %d\n", i+1))
			sb.WriteString(fmt.Sprintf("**Status**: %s\n", issue.Status))
			sb.WriteString(fmt.Sprintf("**Issue Text**: %s\n\n", issue.IssueText))

			if issue.Status == commentConfirmed {
				confirmedCount++
			} else if issue.Status == commentUnresolved {
				unresolvedCount++
			}

			// Review process details
			sb.WriteString("**Review Process**:\n")
			sb.WriteString(fmt.Sprintf("- Exchange Rounds: %d\n", issue.ExchangeRounds))
			if issue.ReviewerRound1BranchID != "" {
				sb.WriteString(fmt.Sprintf("- Reviewer Round 1 Branch: %s\n", issue.ReviewerRound1BranchID))
			}
			if issue.VerifyAgentRound1BranchID != "" {
				sb.WriteString(fmt.Sprintf("- VerifyAgent Round 1 Branch: %s\n", issue.VerifyAgentRound1BranchID))
			}
			if issue.ReviewerRound2BranchID != "" {
				sb.WriteString(fmt.Sprintf("- Reviewer Round 2 Branch: %s\n", issue.ReviewerRound2BranchID))
			}
			if issue.VerifyAgentRound2BranchID != "" {
				sb.WriteString(fmt.Sprintf("- VerifyAgent Round 2 Branch: %s\n", issue.VerifyAgentRound2BranchID))
			}
			if issue.ReviewerRound3BranchID != "" {
				sb.WriteString(fmt.Sprintf("- Reviewer Round 3 Branch: %s\n", issue.ReviewerRound3BranchID))
			}
			if issue.VerifyAgentRound3BranchID != "" {
				sb.WriteString(fmt.Sprintf("- VerifyAgent Round 3 Branch: %s\n", issue.VerifyAgentRound3BranchID))
			}
			sb.WriteString("\n")

			// Reviewer analysis
			if issue.Alpha.Text != "" {
				sb.WriteString("**Reviewer Analysis**:\n")
				reviewerText := issue.Alpha.Text
				if len(reviewerText) > 300 {
					reviewerText = reviewerText[:300] + "..."
				}
				sb.WriteString(reviewerText)
				sb.WriteString("\n\n")
			}

			// VerifyAgent result
			if issue.Beta.Text != "" {
				sb.WriteString("**VerifyAgent Result**:\n")
				if issue.Beta.Verdict != "" {
					sb.WriteString(fmt.Sprintf("- Verdict: %s\n", issue.Beta.Verdict))
				}
				if issue.Beta.VerdictReason != "" {
					sb.WriteString(fmt.Sprintf("- Reason: %s\n", issue.Beta.VerdictReason))
				}
				verifyText := issue.Beta.Text
				if len(verifyText) > 500 {
					verifyText = verifyText[:500] + "..."
				}
				sb.WriteString(fmt.Sprintf("- Analysis: %s\n", verifyText))
				sb.WriteString("\n")
			}

			if issue.VerdictExplanation != "" {
				sb.WriteString(fmt.Sprintf("**Final Verdict**: %s\n\n", issue.VerdictExplanation))
			}

			sb.WriteString("---\n\n")
		}

		sb.WriteString(fmt.Sprintf("**Issue Summary**: %d confirmed, %d unresolved\n\n", confirmedCount, unresolvedCount))
	}

	// Add statistics
	if result.ReviewStatistics != nil {
		sb.WriteString("Review Process Statistics:\n")
		sb.WriteString(fmt.Sprintf("- Total Steps: %d\n", result.ReviewStatistics.TotalSteps))
		sb.WriteString(fmt.Sprintf("- Total Duration: %s\n", result.ReviewStatistics.TotalDuration))

		if len(result.ReviewStatistics.AbnormalSteps) > 0 {
			sb.WriteString(fmt.Sprintf("- Abnormal Steps: %d\n", len(result.ReviewStatistics.AbnormalSteps)))
			for _, abnormal := range result.ReviewStatistics.AbnormalSteps {
				sb.WriteString(fmt.Sprintf("  - %s: %s\n", abnormal.StepName, abnormal.Description))
			}
		}

		if len(result.ReviewStatistics.StepTimings) > 0 {
			sb.WriteString("\nStep Timings:\n")
			for _, timing := range result.ReviewStatistics.StepTimings {
				sb.WriteString(fmt.Sprintf("- %s: %s\n", timing.StepName, timing.Duration))
			}
		}

		if len(result.ReviewStatistics.IssueStatistics) > 0 {
			sb.WriteString("\nIssue Statistics:\n")
			for issueText, stat := range result.ReviewStatistics.IssueStatistics {
				preview := issueText
				if len(preview) > 100 {
					preview = preview[:100] + "..."
				}
				sb.WriteString(fmt.Sprintf("- %s: %d steps, %d reviewer rounds, %d tester rounds", preview, stat.Steps, stat.ReviewerRounds, stat.TesterRounds))
				if stat.VerifyAgentRounds > 0 {
					sb.WriteString(fmt.Sprintf(", %d verify_agent rounds", stat.VerifyAgentRounds))
				}
				sb.WriteString("\n")
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Write the summary report to: ")
	sb.WriteString(outputPath)
	sb.WriteString("\n\n")
	sb.WriteString("Report Format:\n")
	sb.WriteString("# CODE REVIEW SUMMARY\n\n")
	sb.WriteString("## Review Overview\n")
	sb.WriteString("- Task description\n")
	sb.WriteString("- Overall status and summary\n")
	sb.WriteString("- Total issues found and verified\n\n")
	sb.WriteString("## Issues Found\n")
	sb.WriteString("For each issue:\n")
	sb.WriteString("- Issue description\n")
	sb.WriteString("- Verification status (confirmed/unresolved)\n")
	sb.WriteString("- Reviewer and Tester analysis summary\n")
	sb.WriteString("- Verify Agent result (if available)\n")
	sb.WriteString("- Final verdict and explanation\n\n")
	sb.WriteString("## Review Process Statistics\n")
	sb.WriteString("- Total number of steps executed\n")
	sb.WriteString("- Total duration\n")
	sb.WriteString("- Step timing breakdown\n")
	sb.WriteString("- Abnormal steps (errors or unusual behavior)\n")
	sb.WriteString("- Per-issue statistics (rounds, steps, duration)\n\n")
	sb.WriteString("## Conclusion\n")
	sb.WriteString("- Summary of findings\n")
	sb.WriteString("- Recommendations\n")

	return sb.String()
}
