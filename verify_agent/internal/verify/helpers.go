package verify

import (
	"encoding/json"
	"fmt"
	"strings"
)

const outputAwarenessBlock = "**COMMAND OUTPUT AWARENESS**\n" +
	"- Before running any command, consider whether output volume could explode the context window\n" +
	"- Use quiet flags, redirect to a file, then extract only needed lines (e.g., `rg -n \"error|panic\"`)\n" +
	"- Avoid `tee` unless you explicitly need a log file; if used, do not paste full logs\n" +
	"- Do NOT `cat` large logs; quote only minimal relevant snippets\n" +
	"- Be extra careful with `cargo run` and `cargo test` output volume"

const falsePositiveStudyLine = "Read as much as you can, you have unlimited read quotas and available contexts. When you are not sure about something, you must study the code until you figure out.\n\n" +
	"**CRITICAL ASSUMPTION: THIS BUG CLAIM IS A FALSE POSITIVE**\n" +
	"- The bug description you receive is a FALSE POSITIVE (虚假报警)\n" +
	"- This is NOT a real bug - it is an incorrect bug report that needs to be refuted\n" +
	"- Your MANDATORY goal is to FIND THE PROBLEM with the bug claim and explain WHY it is wrong\n\n" +
	"**VERIFICATION PRINCIPLE**\n" +
	"- Your PRIMARY goal is to PROVE the bug claim is WRONG (false positive)\n" +
	"- **UNLESS YOU ARE ABSOLUTELY CERTAIN** that the bug claim is actually correct (i.e., the assumption is wrong), you MUST find evidence that it's false\n" +
	"- You MUST find the specific problem with the bug claim - where is it incorrect?\n" +
	"- Is it based on incorrect assumptions? Wrong code understanding? Missing context? Impossible execution path?\n" +
	"- **ONLY if you find IRREFUTABLE evidence that the bug claim is actually CORRECT** (meaning the assumption was wrong), should you conclude the bug is real\n" +
	"- If you find ANY evidence that the bug claim is incorrect, STOP immediately and provide a detailed refutation report\n" +
	"- Focus on correctness first, efficiency second\n" +
	"- Your final report MUST clearly state: (1) Why the bug claim is wrong, (2) What the correct behavior actually is, (3) Evidence supporting your refutation"

const realBugStudyLine = "Read as much as you can, you have unlimited read quotas and available contexts. When you are not sure about something, you must study the code until you figure out.\n\n" +
	"**CRITICAL ASSUMPTION: THIS BUG CLAIM IS A REAL BUG**\n" +
	"- The bug description you receive is a REAL BUG that needs to be verified and CONFIRMED\n" +
	"- This IS a real bug - it is a valid bug report that needs to be confirmed\n" +
	"- Your MANDATORY goal is to CONFIRM the bug exists and explain WHY it is a real bug\n\n" +
	"**VERIFICATION PRINCIPLE**\n" +
	"- Your PRIMARY goal is to PROVE the bug claim is CORRECT (real bug)\n" +
	"- **UNLESS YOU ARE ABSOLUTELY CERTAIN** that the bug claim is actually incorrect (i.e., the assumption is wrong), you MUST find evidence that it's real\n" +
	"- You MUST trace through actual code paths, understand the execution flow, and collect evidence that confirms the bug\n" +
	"- **ONLY if you find IRREFUTABLE evidence that the bug claim is actually WRONG** (meaning the assumption was wrong), should you conclude the bug is false\n" +
	"- If the bug exists (which it should, since this is a real bug), confirm it with evidence\n" +
	"- Focus on correctness first, efficiency second\n" +
	"- Your final report MUST clearly state: (1) Why the bug is real, (2) What the actual bug behavior is, (3) Evidence supporting your confirmation"

// buildFormalizationPrompt creates the prompt for Task 1: Bug Claim Formalization Agent
func buildFormalizationPrompt(bugDescription string, codeContext string, isFalsePositive bool) string {
	var sb strings.Builder
	sb.WriteString("Task 1: Bug Claim Formalization Agent\n\n")

	// Use different study lines based on whether this is a false positive
	if isFalsePositive {
		sb.WriteString(falsePositiveStudyLine)
	} else {
		sb.WriteString(realBugStudyLine)
	}
	sb.WriteString("\n\n")
	sb.WriteString("Bug Description:\n")
	sb.WriteString(bugDescription)
	sb.WriteString("\n\n")
	if strings.TrimSpace(codeContext) != "" {
		sb.WriteString("Code Context:\n")
		sb.WriteString(codeContext)
		sb.WriteString("\n\n")
	}
	if isFalsePositive {
		sb.WriteString("YOUR TASK: Formalize the bug claim into a structured assertion, then FIND THE PROBLEM with it.\n\n")
		sb.WriteString("**REMEMBER: This bug claim is a FALSE POSITIVE. Your job is to find why it's wrong.**\n\n")
		sb.WriteString("You need to:\n")
		sb.WriteString("1. Extract the claim structure:\n")
		sb.WriteString("   - **Precondition**: What conditions does the claim say must be true for the bug to occur?\n")
		sb.WriteString("   - **Path**: What execution path does the claim say leads to the bug?\n")
		sb.WriteString("   - **Postcondition**: What incorrect state or behavior does the claim say results from the bug?\n")
		sb.WriteString("2. **CRITICALLY EXAMINE the claim**: Is it based on incorrect assumptions? Wrong understanding of the code? Missing context?\n")
		sb.WriteString("3. **FIND THE PROBLEM**: Why is this claim incorrect? What is the actual behavior?\n\n")
		sb.WriteString("CRITICAL: If the bug description is ambiguous, incomplete, based on incorrect assumptions, or cannot be properly formalized, STOP and report that the bug claim is INVALID with detailed explanation of WHY it's wrong.\n\n")
	} else {
		sb.WriteString("YOUR TASK: Formalize the bug claim into a structured assertion, and CONFIRM it can be properly formalized.\n\n")
		sb.WriteString("**REMEMBER: This bug claim is a REAL BUG. Your job is to formalize it so it can be verified.**\n\n")
		sb.WriteString("You need to extract:\n")
		sb.WriteString("1. **Precondition**: What conditions must be true for the bug to occur?\n")
		sb.WriteString("2. **Path**: What execution path or code flow leads to the bug?\n")
		sb.WriteString("3. **Postcondition**: What incorrect state or behavior results from the bug?\n\n")
		sb.WriteString("CRITICAL: Since this is a REAL BUG, you should be able to formalize it into a clear assertion.\n")
		sb.WriteString("**ONLY if the bug description is genuinely ambiguous, incomplete, or cannot be formalized** (meaning it's not a valid bug report), should you report INVALID.\n")
		sb.WriteString("Otherwise, you MUST extract and formalize the bug claim structure.\n\n")
	}
	sb.WriteString(outputAwarenessBlock)
	sb.WriteString("\n\n")
	sb.WriteString("RESPONSE FORMAT:\n")
	sb.WriteString("First, restate the bug claim:\n")
	sb.WriteString("## Bug Claim\n")
	sb.WriteString("<Restate the bug description clearly>\n\n")
	sb.WriteString("Then provide your judgment:\n")
	sb.WriteString("# STATUS: [VALID | INVALID]\n\n")
	sb.WriteString("If STATUS is INVALID, provide:\n")
	sb.WriteString("## Judgment\n")
	if isFalsePositive {
		sb.WriteString("<Your judgment: The bug claim is invalid because... (explain the specific problem with the claim)>\n\n")
		sb.WriteString("## Reason\n")
		sb.WriteString("<Why the bug claim cannot be formalized - what is wrong with it?>\n\n")
		sb.WriteString("## Refutation Report\n")
		sb.WriteString("<Detailed explanation: What is the actual correct behavior? Why is the bug claim incorrect? What assumptions or understanding in the claim are wrong?>\n\n")
	} else {
		sb.WriteString("<Your judgment: The bug claim is invalid because...>\n\n")
		sb.WriteString("## Reason\n")
		sb.WriteString("<Why the bug claim cannot be formalized>\n\n")
	}
	sb.WriteString("If STATUS is VALID, provide:\n")
	sb.WriteString("## Judgment\n")
	sb.WriteString("<Your judgment: The bug claim is valid and can be formalized>\n\n")
	sb.WriteString("## Formalized Assertion\n")
	sb.WriteString("```json\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"precondition\": \"<conditions that must be true>\",\n")
	sb.WriteString("  \"path\": \"<execution path or code flow>\",\n")
	sb.WriteString("  \"postcondition\": \"<incorrect state or behavior>\"\n")
	sb.WriteString("}\n")
	sb.WriteString("```\n\n")
	sb.WriteString("## Analysis\n")
	sb.WriteString("<Your reasoning about the formalization>\n")
	return sb.String()
}

// buildReachabilityPrompt creates the prompt for Task 2: Reachability Analysis Agent
func buildReachabilityPrompt(formalizedAssertion string, codeContext string, isFalsePositive bool) string {
	var sb strings.Builder
	sb.WriteString("Task 2: Reachability Analysis Agent\n\n")

	// Use different study lines based on whether this is a false positive
	if isFalsePositive {
		sb.WriteString(falsePositiveStudyLine)
	} else {
		sb.WriteString(realBugStudyLine)
	}
	sb.WriteString("\n\n")
	sb.WriteString("Formalized Assertion:\n")
	sb.WriteString(formalizedAssertion)
	sb.WriteString("\n\n")
	if strings.TrimSpace(codeContext) != "" {
		sb.WriteString("Code Context:\n")
		sb.WriteString(codeContext)
		sb.WriteString("\n\n")
	}
	if isFalsePositive {
		sb.WriteString("YOUR TASK: Determine if the precondition and path described in the assertion are reachable with valid inputs, and FIND THE PROBLEM with the claim.\n\n")
		sb.WriteString("**REMEMBER: This bug claim is a FALSE POSITIVE. Your job is to find why the claimed state cannot be reached.**\n\n")
		sb.WriteString("You need to analyze:\n")
		sb.WriteString("1. Can the precondition be satisfied with valid inputs? (If not, WHY not?)\n")
		sb.WriteString("2. Can the execution path be reached from the precondition? (If not, what prevents it?)\n")
		sb.WriteString("3. Are there any guards, checks, or constraints that prevent reaching the bug state? (What are they?)\n")
		sb.WriteString("4. **FIND THE SPECIFIC PROBLEM**: Why is the claim wrong? What is the actual execution flow?\n\n")
		sb.WriteString("CRITICAL: If the state is NOT reachable with valid inputs, STOP and provide a DETAILED REFUTATION REPORT explaining:\n")
		sb.WriteString("- Why the claimed state cannot be reached\n")
		sb.WriteString("- What guards, checks, or constraints prevent it\n")
		sb.WriteString("- What the actual correct execution flow is\n")
		sb.WriteString("- Why the bug claim is based on incorrect assumptions\n\n")
	} else {
		sb.WriteString("YOUR TASK: Determine if the precondition and path described in the assertion are reachable with valid inputs, and CONFIRM the bug state is reachable.\n\n")
		sb.WriteString("**REMEMBER: This bug claim is a REAL BUG. Your job is to confirm that the bug state CAN be reached.**\n\n")
		sb.WriteString("You need to analyze:\n")
		sb.WriteString("1. Can the precondition be satisfied with valid inputs? (If yes, HOW?)\n")
		sb.WriteString("2. Can the execution path be reached from the precondition? (If yes, WHAT is the path?)\n")
		sb.WriteString("3. Are there any guards, checks, or constraints that prevent reaching the bug state? (If not, WHY not?)\n")
		sb.WriteString("4. **CONFIRM REACHABILITY**: Trace through the code and provide evidence that the bug state IS reachable\n\n")
		sb.WriteString("CRITICAL: Since this is a REAL BUG, you should find evidence that the state IS reachable.\n")
		sb.WriteString("**ONLY if you find IRREFUTABLE evidence that the state is NOT reachable** (meaning the assumption was wrong), should you report UNREACHABLE.\n")
		sb.WriteString("Otherwise, you MUST find and document how the bug state can be reached.\n\n")
	}
	sb.WriteString(outputAwarenessBlock)
	sb.WriteString("\n\n")
	sb.WriteString("**CRITICAL: TEST EXECUTION POLICY**\n")
	sb.WriteString("- Do NOT run `cargo test` (this runs ALL tests and is extremely slow)\n")
	sb.WriteString("- Do NOT run `cargo check --all-targets` or `cargo clippy --all-targets` (these are slow and often fail)\n")
	sb.WriteString("- You MAY run EXTREMELY SMALL, targeted tests if absolutely necessary to verify reachability\n")
	sb.WriteString("  * For Rust: Use `cargo test <specific_test_function_name>` to run ONLY one test\n")
	sb.WriteString("- Prefer static analysis and code reading. Only run tests when absolutely necessary.\n\n")
	sb.WriteString("RESPONSE FORMAT:\n")
	sb.WriteString("First, restate the formalized assertion:\n")
	sb.WriteString("## Formalized Assertion\n")
	sb.WriteString("<Restate the precondition, path, and postcondition>\n\n")
	sb.WriteString("Then provide your judgment:\n")
	sb.WriteString("# STATUS: [REACHABLE | UNREACHABLE | INVALID]\n\n")
	sb.WriteString("If STATUS is UNREACHABLE or INVALID, provide:\n")
	sb.WriteString("## Judgment\n")
	if isFalsePositive {
		sb.WriteString("<Your judgment: The bug state is unreachable/invalid because... (explain the specific problem)>\n\n")
		sb.WriteString("## Reason\n")
		sb.WriteString("<Why the state cannot be reached with valid inputs - what prevents it?>\n\n")
		sb.WriteString("## Evidence\n")
		sb.WriteString("<Code references, guards, or constraints that prevent reachability>\n\n")
		sb.WriteString("## Refutation Report\n")
		sb.WriteString("<Detailed explanation: Why is the bug claim wrong? What is the actual correct behavior? What incorrect assumptions does the claim make? Provide specific code evidence.>\n\n")
	} else {
		sb.WriteString("<Your judgment: The bug state is unreachable/invalid because...>\n\n")
		sb.WriteString("## Reason\n")
		sb.WriteString("<Why the state cannot be reached with valid inputs>\n\n")
		sb.WriteString("## Evidence\n")
		sb.WriteString("<Code references, guards, or constraints that prevent reachability>\n\n")
	}
	sb.WriteString("If STATUS is REACHABLE, provide:\n")
	sb.WriteString("## Judgment\n")
	sb.WriteString("<Your judgment: The bug state is reachable with valid inputs>\n\n")
	sb.WriteString("## Reachability Analysis\n")
	sb.WriteString("<How the precondition and path can be reached>\n\n")
	sb.WriteString("## Evidence\n")
	sb.WriteString("<Code references supporting reachability>\n")
	return sb.String()
}

// buildTestGeneratorPrompt creates the prompt for Task 3: Test Generator Agent
func buildTestGeneratorPrompt(formalizedAssertion string, reachabilityAnalysis string, codeContext string, isFalsePositive bool) string {
	var sb strings.Builder
	sb.WriteString("Task 3: Test Generator Agent\n\n")

	// Use different study lines based on whether this is a false positive
	if isFalsePositive {
		sb.WriteString(falsePositiveStudyLine)
	} else {
		sb.WriteString(realBugStudyLine)
	}
	sb.WriteString("\n\n")
	sb.WriteString("Formalized Assertion:\n")
	sb.WriteString(formalizedAssertion)
	sb.WriteString("\n\n")
	sb.WriteString("Reachability Analysis:\n")
	sb.WriteString(reachabilityAnalysis)
	sb.WriteString("\n\n")
	if strings.TrimSpace(codeContext) != "" {
		sb.WriteString("Code Context:\n")
		sb.WriteString(codeContext)
		sb.WriteString("\n\n")
	}
	if isFalsePositive {
		sb.WriteString("YOUR TASK: Generate a minimal test case that can verify or refute the bug claim, and FIND THE PROBLEM with the claim.\n\n")
		sb.WriteString("**REMEMBER: This bug claim is a FALSE POSITIVE. Your job is to demonstrate why it's wrong through testing.**\n\n")
		sb.WriteString("The test should:\n")
		sb.WriteString("1. Set up the precondition (as claimed by the bug report)\n")
		sb.WriteString("2. Execute the path described in the assertion (as claimed by the bug report)\n")
		sb.WriteString("3. Check if the postcondition (bug) actually occurs\n")
		sb.WriteString("4. **ANALYZE THE RESULTS**: What actually happens? Why is the bug claim incorrect?\n\n")
		sb.WriteString("CRITICAL: Since this is a FALSE POSITIVE, the test should demonstrate that the bug does NOT occur.\n")
		sb.WriteString("If the test shows the bug does NOT occur (which it should, since this is a false positive), STOP and provide a DETAILED REFUTATION REPORT explaining:\n")
		sb.WriteString("- What the test actually shows (the correct behavior)\n")
		sb.WriteString("- Why the bug claim is incorrect\n")
		sb.WriteString("- What incorrect assumptions the claim makes\n")
		sb.WriteString("- What the actual correct behavior is\n\n")
	} else {
		sb.WriteString("YOUR TASK: Generate a minimal test case that can verify the bug claim, and CONFIRM the bug exists.\n\n")
		sb.WriteString("**REMEMBER: This bug claim is a REAL BUG. Your job is to confirm that the bug DOES occur through testing.**\n\n")
		sb.WriteString("The test should:\n")
		sb.WriteString("1. Set up the precondition (as described in the assertion)\n")
		sb.WriteString("2. Execute the path described in the assertion\n")
		sb.WriteString("3. Check if the postcondition (bug) occurs\n")
		sb.WriteString("4. **ANALYZE THE RESULTS**: Does the bug actually occur? What evidence confirms it?\n\n")
		sb.WriteString("CRITICAL: Since this is a REAL BUG, the test should demonstrate that the bug DOES occur.\n")
		sb.WriteString("If the test shows the bug DOES occur (which it should, since this is a real bug), provide a DETAILED CONFIRMATION REPORT explaining:\n")
		sb.WriteString("- What the test results show (the bug behavior)\n")
		sb.WriteString("- How the bug manifests\n")
		sb.WriteString("- What evidence confirms the bug is real\n")
		sb.WriteString("- Clear statement: \"The bug claim is CONFIRMED as REAL because...\"\n\n")
		sb.WriteString("**ONLY if you find IRREFUTABLE evidence that the bug does NOT occur** (meaning the assumption was wrong), should you report BUG_REFUTED.\n")
		sb.WriteString("Otherwise, you MUST find and document evidence that confirms the bug is real.\n\n")
	}
	sb.WriteString(outputAwarenessBlock)
	sb.WriteString("\n\n")
	sb.WriteString("**CRITICAL: TEST EXECUTION POLICY**\n")
	sb.WriteString("- Generate the SMALLEST possible test that directly verifies the assertion\n")
	sb.WriteString("- For Rust: Use `cargo test <specific_test_function_name>` to run ONLY one test\n")
	sb.WriteString("- Do NOT run comprehensive test suites\n")
	sb.WriteString("- The test should be minimal and focused on the specific bug claim\n\n")
	sb.WriteString("RESPONSE FORMAT:\n")
	sb.WriteString("First, restate the bug claim and assertion:\n")
	sb.WriteString("## Bug Claim\n")
	sb.WriteString("<Restate the original bug description>\n\n")
	sb.WriteString("## Formalized Assertion\n")
	sb.WriteString("<Restate the precondition, path, and postcondition>\n\n")
	sb.WriteString("Then provide your judgment:\n")
	sb.WriteString("# STATUS: [BUG_CONFIRMED | BUG_REFUTED | TEST_INCONCLUSIVE]\n\n")
	if isFalsePositive {
		sb.WriteString("**NOTE: Since this is a FALSE POSITIVE, STATUS should typically be BUG_REFUTED.**\n\n")
		sb.WriteString("## Judgment\n")
		sb.WriteString("<Your judgment: The bug is [refuted/confirmed/inconclusive] because... (explain the specific problem with the claim)>\n\n")
		sb.WriteString("Then provide:\n")
		sb.WriteString("## Test Case\n")
		sb.WriteString("<The minimal test code that tests the bug claim>\n\n")
		sb.WriteString("## Test Execution\n")
		sb.WriteString("<Actual test output or results - what actually happens when the test runs?>\n\n")
		sb.WriteString("## Analysis\n")
		sb.WriteString("<Detailed analysis: What do the test results show? What is the actual correct behavior? Why does this refute the bug claim?>\n\n")
		sb.WriteString("## Refutation Report\n")
		sb.WriteString("<COMPREHENSIVE explanation of why the bug claim is wrong:\n")
		sb.WriteString("- What the test results actually demonstrate\n")
		sb.WriteString("- What the correct behavior is\n")
		sb.WriteString("- Why the bug claim is incorrect (wrong assumptions, missing context, incorrect understanding, etc.)\n")
		sb.WriteString("- Specific code evidence supporting your refutation\n")
		sb.WriteString("- Clear statement: \"The bug claim is a FALSE POSITIVE because...\"\n>\n")
	} else {
		sb.WriteString("**NOTE: Since this is a REAL BUG, STATUS should typically be BUG_CONFIRMED.**\n\n")
		sb.WriteString("## Judgment\n")
		sb.WriteString("<Your judgment: The bug is [confirmed/refuted/inconclusive] because... (explain the evidence)>\n\n")
		sb.WriteString("Then provide:\n")
		sb.WriteString("## Test Case\n")
		sb.WriteString("<The minimal test code that tests the bug claim>\n\n")
		sb.WriteString("## Test Execution\n")
		sb.WriteString("<Actual test output or results - what actually happens when the test runs?>\n\n")
		sb.WriteString("## Analysis\n")
		sb.WriteString("<Detailed analysis: What do the test results show? Does the bug occur? What evidence confirms or refutes the bug claim?>\n\n")
		sb.WriteString("## Confirmation Report\n")
		sb.WriteString("<If the bug is confirmed, provide COMPREHENSIVE explanation:\n")
		sb.WriteString("- What the test results actually demonstrate\n")
		sb.WriteString("- How the bug manifests\n")
		sb.WriteString("- What evidence confirms the bug is real\n")
		sb.WriteString("- Specific code evidence supporting your confirmation\n")
		sb.WriteString("- Clear statement: \"The bug claim is CONFIRMED as REAL because...\"\n>\n")
	}
	return sb.String()
}

// FormalizedAssertion represents the structured bug claim
type FormalizedAssertion struct {
	Precondition  string `json:"precondition"`
	Path          string `json:"path"`
	Postcondition string `json:"postcondition"`
}

// parseFormalizedAssertion extracts the JSON assertion from the response
func parseFormalizedAssertion(response string) (FormalizedAssertion, error) {
	var assertion FormalizedAssertion

	// Try to extract JSON block
	jsonBlock := extractJSONBlock(response)
	if jsonBlock == "" {
		return assertion, fmt.Errorf("no JSON block found in response")
	}

	if err := json.Unmarshal([]byte(jsonBlock), &assertion); err != nil {
		return assertion, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return assertion, nil
}

func extractJSONBlock(raw string) string {
	trimmed := strings.TrimSpace(raw)

	// First, try to find and extract JSON from markdown code blocks (```json ... ```)
	jsonBlockStart := strings.Index(trimmed, "```json")
	if jsonBlockStart >= 0 {
		// Find the start of actual JSON (after ```json and optional newline)
		jsonStart := jsonBlockStart + 7 // len("```json")
		// Skip whitespace and newlines
		for jsonStart < len(trimmed) && (trimmed[jsonStart] == ' ' || trimmed[jsonStart] == '\n' || trimmed[jsonStart] == '\r') {
			jsonStart++
		}
		// Find the closing ```
		jsonBlockEnd := strings.Index(trimmed[jsonStart:], "```")
		if jsonBlockEnd >= 0 {
			jsonEnd := jsonStart + jsonBlockEnd
			// Trim trailing whitespace
			jsonContent := strings.TrimSpace(trimmed[jsonStart:jsonEnd])
			if jsonContent != "" {
				return jsonContent
			}
		}
	}

	// Also try generic code block (``` ... ```)
	codeBlockStart := strings.Index(trimmed, "```")
	if codeBlockStart >= 0 {
		// Find the start of actual content (after ``` and optional language identifier)
		contentStart := codeBlockStart + 3
		// Skip language identifier and whitespace
		for contentStart < len(trimmed) && trimmed[contentStart] != '\n' {
			contentStart++
		}
		contentStart++ // Skip the newline
		// Find the closing ```
		codeBlockEnd := strings.Index(trimmed[contentStart:], "```")
		if codeBlockEnd >= 0 {
			contentEnd := contentStart + codeBlockEnd
			jsonContent := strings.TrimSpace(trimmed[contentStart:contentEnd])
			// Check if it looks like JSON (starts with { or [)
			if strings.HasPrefix(jsonContent, "{") || strings.HasPrefix(jsonContent, "[") {
				return jsonContent
			}
		}
	}

	// Fallback: find JSON object or array directly
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

// extractStatus extracts the status from a response
func extractStatus(response string, validStatuses []string) string {
	lines := strings.Split(response, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") && strings.Contains(line, "STATUS:") {
			for _, status := range validStatuses {
				if strings.Contains(strings.ToUpper(line), strings.ToUpper(status)) {
					return status
				}
			}
		}
	}
	return ""
}
