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

const universalStudyLine = "Read as much as you can, you have unlimited read quotas and available contexts. When you are not sure about something, you must study the code until you figure out.\n\n" +
	"**SCENARIO VALIDATION**\n" +
	"- Before reporting an issue, confirm the described trigger scenario is real and reachable in current code paths\n" +
	"- We encourage deep exploration of relevant execution paths and scenarios\n" +
	"- Use actual usage, design intent, and code comments to reason about expected behavior and performance tradeoffs\n" +
	"- If a behavior is by design (e.g., a performance tradeoff), call that out instead of proposing a fix\n" +
	"- Do not invent unsupported or hypothetical scenarios"

const p0p1FocusBlock = "**P0/P1 FOCUS**\n" +
	"- Report ONLY P0/P1 issues\n" +
	"- Ignore general issues (style, refactors, maintainability, low-impact edge cases)\n" +
	"- For each reported issue, include severity (P0/P1), impact analysis, and a plausible fix\n" +
	"- If impact is limited or the behavior is a deliberate tradeoff/by design, do NOT report it\n" +
	"- If no P0/P1 issues exist, write exactly: \"No P0/P1 issues found\"\n"

const p0p1VerdictGateBlock = "**P0/P1 SEVERITY GATE**\n" +
	"- Your verdict is about whether issueText is a real P0/P1 issue, not just whether a behavior exists\n" +
	"- Evaluate impact and fix feasibility; if impact is limited or behavior is a deliberate tradeoff/by design, REJECT\n" +
	"- If only risky or unreasonable fixes exist, REJECT\n"

const bugFinderSOPBlock = "**SOP (use only when the matching pattern appears in code)**\n" +
	"- Proxy flags are untrusted: when logic uses intent/maybe/temporary flags instead of final truth, ask \"can it be true early and later overturned?\" If yes, don't short-circuit critical logic on it.\n" +
	"- Short-circuit checklist: when you see return/continue/skip, list intended outputs/state; identify what becomes missing/default/stale; check downstream handling (missing => worst-case/full processing?).\n" +
	"- Strategy inputs are contracts: when selecting strategies/heuristics, treat candidate sets/cost inputs/pruning as externally visible; guards that coarsen/empty them are regression risk.\n" +
	"- Non-local state time consistency: when reading/writing ctx/session/global, trace the read/write order; suspect irreversible decisions based on pre-write assumptions.\n" +
	"- Minimal counterexample: for each guard, try a case where the guard triggers but later falls back / becomes irrelevant; if possible, treat it as a behavior-change point.\n"

func buildIssueFinderPrompt(task string, changeAnalysisPath string) string {
	var sb strings.Builder
	sb.WriteString("Task: ")
	sb.WriteString(task)
	sb.WriteString("\n\n")
	sb.WriteString(universalStudyLine)
	sb.WriteString("\n\n")
	sb.WriteString(bugFinderSOPBlock)
	sb.WriteString("\n\n")
	if strings.TrimSpace(changeAnalysisPath) != "" {
		sb.WriteString("Reference (read-only): Change Analysis at: ")
		sb.WriteString(changeAnalysisPath)
		sb.WriteString("\n\n")
	}
	sb.WriteString("FINAL RESPONSE:\n")
	sb.WriteString("- Provide a critical P0/P1/P2 issue report (include severity, impact, evidence, and a plausible fix).\n")
	sb.WriteString("- If no P0/P1/P2 issues exist, write exactly: \"No P0/P1 issues found\".\n\n")
	return sb.String()
}

func buildScoutPrompt(task string, outputPath string) string {
	var sb strings.Builder
	sb.WriteString("Role: SCOUT\n\n")
	sb.WriteString(universalStudyLine)
	sb.WriteString("\n\n")
	sb.WriteString("Task / PR context:\n")
	sb.WriteString(task)
	sb.WriteString("\n\n")
	sb.WriteString("Requirement: Write a Change Analysis that helps to improve subsequent review + testing.\n")
	sb.WriteString("Goal: high-signal summary + impact/risk analysis (NOT a line-by-line commentary).\n")
	sb.WriteString("You MUST base the analysis on an actual diff against base branch (main or master), not assumptions.\n\n")
	sb.WriteString("Get the diff:\n")
	sb.WriteString("  1) Find the merge-base SHA for this comparison:\n")
	sb.WriteString("     - Try: git merge-base HEAD BASE_BRANCH\n")
	sb.WriteString("     - If that fails, try: git merge-base HEAD \"BASE_BRANCH@{upstream}\"\n")
	sb.WriteString("     - If still failing, inspect refs/remotes and pick the correct remote-tracking ref, then re-run merge-base.\n\n")
	sb.WriteString("  2) Once you have MERGE_BASE_SHA, inspect changes relative to the base branch:\n")
	sb.WriteString("     - Run: git diff MERGE_BASE_SHA\n")
	sb.WriteString("     - Also run: git diff --name-status MERGE_BASE_SHA\n\n")
	sb.WriteString("Analysis guidance:\n")
	sb.WriteString("- Focus on behavior, invariants, error semantics, edge cases, concurrency, compatibility.\n")
	sb.WriteString("- If defaults/contracts/config/env/flags changed, treat it as high risk; and find likely call sites.\n")
	sb.WriteString("- After reviewing the full diff, label KEY vs secondary points; deep dive ALL KEY items and keep secondary brief.\n")
	sb.WriteString("- Include file:line or symbol anchors for key points.\n\n")
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
	sb.WriteString("Output format (concise but complete):\n")
	sb.WriteString("# CHANGE ANALYSIS\n")
	sb.WriteString("## Summary (<= 5 lines)\n")
	sb.WriteString("## Behavioral / Contract Deltas\n")
	sb.WriteString("## High-Risk Areas (ranked)\n")
	sb.WriteString("For each item, include: What changed (anchor), Before -> After, Who/what is impacted, How to verify.\n")
	sb.WriteString("Mark each KEY item and provide deeper analysis there; keep non-KEY items brief.\n")
	sb.WriteString("## Impacted Call Sites / Code Paths\n")
	sb.WriteString("## Appendix: Change Surface\n")
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

func BuildLogicAnalystPrompt(issueText string) string {
	return buildLogicAnalystPrompt(issueText)
}

// buildReviewerPrompt creates the prompt for the Reviewer role (logic analysis).
func buildLogicAnalystPrompt(issueText string) string {
	var sb strings.Builder
	sb.WriteString("Verification Role: REVIEWER\n\n")
	sb.WriteString("You will review an opponent's Issue List. Your default stance is: each issue may be a misread, a misunderstanding, or an edge case--unless the code evidence forces you to accept it.\n\n")
	sb.WriteString("Reference severity definitions (guidance, not a hard rule):\n")
	sb.WriteString("- P0 (Critical/Blocker): Reachable under default production configuration, and causes production unavailability; severe data loss/corruption; a security vulnerability; or a primary workflow is completely blocked with no practical workaround. Must be fixed immediately.\n")
	sb.WriteString("- P1 (High): Reachable in realistic production scenarios (default or commonly enabled configs), and significantly impairs core/major functionality or violates user-facing contracts relied upon (including user-visible correctness errors), or causes a severe performance regression that impacts use; a workaround may exist but is costly/risky/high-friction. Must be fixed before release.\n")
	sb.WriteString("- Lightweight evidence bar (guidance): A P0/P1 claim must be backed by clear code-causal evidence and an explicit blast-radius assessment; if it’s borderline between P1 and P2, default to P1 unless the impact is clearly narrow or edge-case only.\n\n")
	sb.WriteString("Goal: For each issue, run an adversarial / rebuttal-style review. Try hard to find weaknesses that would prevent it from being legitimately classified as P0/P1. If you cannot find such a weakness, be honest and acknowledge it as a real P0/P1 issue.\n\n")
	sb.WriteString("How to work (principles, not rigid steps):\n")
	sb.WriteString("- Evidence-first: Conclusions must come from code and build/runtime-path facts, not experience or speculation.\n")
	sb.WriteString("- Reachability matters: Confirm whether the reported behavior is reachable in default/production paths, or only under gated features, tests, unusual configs, or non-standard environments.\n")
	sb.WriteString("- Impact must be concrete: State what it actually causes (crash, data corruption, correctness break, resource leak, supply-chain/repro risk, etc.), and its scope/probability.\n")
	sb.WriteString("- Prioritize counter-evidence: Actively look for counterexamples—unreachable branches, existing guards, existing tests/coverage, runtime fallbacks, isolation boundaries, or cases where it only affects developer workflows.\n")
	sb.WriteString("- Fixes have costs: If a fix is proposed, discuss its side effects, compatibility risk, and complexity. Avoid “fixing” something in a way that creates a bigger problem.\n\n")
	sb.WriteString("注意\n\n")
	sb.WriteString("1. 解释代码，而不是猜测\n")
	sb.WriteString("2. 质疑假设\n")
	sb.WriteString("3. 面对理解空白\n")
	sb.WriteString("4. 将分析代码和 issue 看作科学实验。不要只是猜测，\n\n")
	sb.WriteString("Output requirements:\n")
	sb.WriteString("For each issue, give a clear verdict: P0 / P1 / P2 / Not an issue, and include the most critical supporting evidence (file path + key symbols/logic). Provide a one-sentence justification for why it does or does not deserve P0/P1 in real scenarios.\n\n")
	sb.WriteString("Optional strengthening (still not rigid): Any P0/P1 claim should be backed by a minimal trigger condition or a clear, code-grounded reasoning chain.\n")
	sb.WriteString("RESPONSE FORMAT:\n")
	sb.WriteString("Start with: # VERDICT: [CONFIRMED | REJECTED]\n")
	sb.WriteString("Then: Severity: [P0 | P1 | P2 | Not an issue]\n\n")
	sb.WriteString("Then provide:\n")
	sb.WriteString("## Reasoning\n")
	sb.WriteString("<Your analysis of the code logic>\n\n")
	sb.WriteString("## Evidence\n")
	sb.WriteString("<Code traces or architectural analysis supporting your verdict>\n")
	sb.WriteString("\n## Additions (out of scope)\n")
	sb.WriteString("<Optional: other issues you noticed, explicitly out of scope for this verdict>\n")
	sb.WriteString("\n\n---\n\nThe Issue List:\n\n")
	sb.WriteString(issueText)
	sb.WriteString("\n")
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
	sb.WriteString("YOUR ROLE: Simulate a QA engineer who verifies bugs by running real tests.\n\n")
	sb.WriteString("CRITICAL: You MUST actually run code to collect evidence.\n")
	sb.WriteString("Do NOT fabricate test results or mock behavior.\n\n")
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
	sb.WriteString("## Reproduction Steps\n")
	sb.WriteString("<What you did to reproduce>\n\n")
	sb.WriteString("## Test Evidence\n")
	sb.WriteString("<Actual test output or error messages>\n")
	sb.WriteString("If you reference a custom script or test, include the key command or code snippet so others can rerun it; evidence without reproduction detail is not credible.\n")
	sb.WriteString("\n## Additions (out of scope)\n")
	sb.WriteString("<Optional: other issues you noticed, explicitly out of scope for this verdict>\n")
	return sb.String()
}

// buildExchangePrompt creates the prompt for Round 2 (exchange opinions).
func buildExchangePrompt(role string, task string, issueText string, changeAnalysisPath string, selfOpinion string, peerOpinion string) string {
	normalizedRole := strings.ToLower(strings.TrimSpace(role))
	displayRole := strings.ToUpper(role)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Verification Role: %s (Round 2 - Exchange)\n\n", displayRole))
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
	sb.WriteString("## Response to Peer\n")
	sb.WriteString("<Address their key points>\n\n")
	sb.WriteString("## Final Reasoning\n")
	sb.WriteString("<Your updated analysis>\n")
	sb.WriteString("\n## Additions (out of scope)\n")
	sb.WriteString("<Optional: other issues you noticed, explicitly out of scope for this verdict>\n")
	return sb.String()
}

type verdictDecision struct {
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
}

var verdictLineRe = regexp.MustCompile(`(?i)^\s*#?\s*verdict\s*:\s*\[?\s*(confirmed|rejected)\s*\]?\s*$`)

type verdictExtractionResponse struct {
	Verdict    string `json:"verdict"`
	Evidence   string `json:"evidence,omitempty"`
	Confidence any    `json:"confidence,omitempty"`
}

func buildVerdictExtractionPrompt(transcript Transcript) string {
	var sb strings.Builder
	sb.WriteString("You are extracting a final verdict from a verification transcript.\n")
	sb.WriteString("Do NOT re-evaluate the underlying issue; ONLY extract what verdict the transcript's author intended.\n")
	sb.WriteString("The verdict line may be wrapped in markdown (bullets, backticks, headings).\n\n")
	sb.WriteString("Return ONLY JSON with this schema:\n")
	sb.WriteString("{\"verdict\":\"confirmed|rejected|unknown\",\"evidence\":\"<copy the line(s) that support your decision>\"}\n")
	sb.WriteString("Use verdict=unknown ONLY if you cannot confidently determine the intended final verdict.\n")
	sb.WriteString("If multiple verdicts appear, prefer the final one.\n\n")
	sb.WriteString("Transcript metadata:\n")
	sb.WriteString(fmt.Sprintf("- agent: %s\n", strings.TrimSpace(transcript.Agent)))
	sb.WriteString(fmt.Sprintf("- round: %d\n\n", transcript.Round))
	sb.WriteString("Transcript:\n<<<TRANSCRIPT>>>\n")
	sb.WriteString(transcript.Text)
	sb.WriteString("\n<<<END TRANSCRIPT>>>\n")
	return sb.String()
}

func parseVerdictExtractionResponse(raw string) (verdictDecision, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return verdictDecision{}, fmt.Errorf("empty verdict response (raw=%q)", truncateForError(raw))
	}
	jsonBlock := extractJSONBlock(trimmed)
	var out verdictExtractionResponse
	if err := json.Unmarshal([]byte(jsonBlock), &out); err != nil {
		return verdictDecision{}, fmt.Errorf("invalid verdict JSON: %v (json=%q raw=%q)", err, truncateForError(jsonBlock), truncateForError(trimmed))
	}
	verdict := strings.ToLower(strings.TrimSpace(out.Verdict))
	switch verdict {
	case "confirmed", "rejected", "unknown":
		reason := "llm transcript verdict"
		if ev := strings.TrimSpace(out.Evidence); ev != "" {
			reason = fmt.Sprintf("llm transcript verdict (evidence: %s)", truncateForError(ev))
		}
		return verdictDecision{Verdict: verdict, Reason: reason}, nil
	default:
		return verdictDecision{}, fmt.Errorf("invalid verdict value %q (raw=%q)", out.Verdict, truncateForError(trimmed))
	}
}

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
