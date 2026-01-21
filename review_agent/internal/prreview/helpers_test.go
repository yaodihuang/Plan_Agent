package prreview

import (
	"strings"
	"testing"
)

func TestBuildIssueFinderPromptContainsInstructions(t *testing.T) {
	task := "https://github.com/org/repo/pull/42"
	got := buildIssueFinderPrompt(task, "/workspace/change_analysis.md")

	required := []string{
		"Task: " + task,
		universalStudyLine,
		"SOP (use only when the matching pattern appears in code)",
		"Proxy flags are untrusted:",
		"Short-circuit checklist:",
		"Strategy inputs are contracts:",
		"Non-local state time consistency:",
		"Minimal counterexample:",
		"Reference (read-only): Change Analysis at:",
		"/workspace/change_analysis.md",
		"FINAL RESPONSE:",
		"critical P0/P1/P2 issue report",
		"\"No P0/P1 issues found\"",
	}

	for _, req := range required {
		if !strings.Contains(got, req) {
			t.Errorf("prompt missing required text: %q", req)
		}
	}
}

func TestBuildHasRealIssuePromptContainsContractAndSentinel(t *testing.T) {
	prompt := buildHasRealIssuePrompt("No P0/P1 issues found")
	required := []string{
		"strict triage",
		"Contract:",
		"No P0/P1 issues found",
		"Reply ONLY with JSON",
		"has_issue",
		"Review report:",
	}
	for _, needle := range required {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("hasRealIssue prompt missing %q: %q", needle, prompt)
		}
	}
}

func TestBuildReviewerPromptContainsRoleDirectives(t *testing.T) {
	issueText := "some issue"
	prompt := buildLogicAnalystPrompt(issueText)
	requiredPhrases := []string{
		"opponent's Issue List",
		"adversarial / rebuttal-style review",
		"Evidence-first",
		"RESPONSE FORMAT:",
		"# VERDICT",
		"Severity:",
		"The Issue List:",
		issueText,
	}
	for _, phrase := range requiredPhrases {
		if !strings.Contains(prompt, phrase) {
			t.Errorf("reviewer prompt missing directive: %q", phrase)
		}
	}
}

func TestBuildTesterPromptContainsRoleDirectives(t *testing.T) {
	prompt := buildTesterPrompt("some task", "some issue", "/workspace/change_analysis.md")
	requiredPhrases := []string{
		"TESTER",
		universalStudyLine,
		"Simulate a QA engineer",
		"MUST actually run code",
		"Do NOT run `cargo test`",
		"Use ONLY extremely small, targeted test batches",
		p0p1VerdictGateBlock,
		"cargo check --all-targets",
		"cargo clippy --all-targets",
		"Do NOT fabricate",
		"VERDICT",
		"include the key command or code snippet",
		"Change Analysis at:",
	}
	for _, phrase := range requiredPhrases {
		if !strings.Contains(prompt, phrase) {
			t.Errorf("tester prompt missing directive: %q", phrase)
		}
	}
}

func TestBuildExchangePromptIncludesSelfPeerAndReviewerGuidance(t *testing.T) {
	prompt := buildExchangePrompt("reviewer", "task", "issue", "/workspace/change_analysis.md", "my old verdict", "peer said hello")
	required := []string{
		"my old verdict",
		"peer said hello",
		"YOUR PREVIOUS OPINION",
		"PEER'S OPINION",
		p0p1VerdictGateBlock,
		"logic analysis",
		"Do NOT claim you ran tests",
	}
	for _, needle := range required {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("reviewer exchange prompt missing %q: %q", needle, prompt)
		}
	}
}

func TestBuildExchangePromptProvidesTesterGuidance(t *testing.T) {
	prompt := buildExchangePrompt("tester", "task", "issue", "/workspace/change_analysis.md", "my reproduction log", "peer logic view")
	required := []string{
		"my reproduction log",
		"peer logic view",
		"YOUR PREVIOUS OPINION",
		"PEER'S OPINION",
		p0p1VerdictGateBlock,
		"run code",
		"real execution evidence",
	}
	for _, needle := range required {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("tester exchange prompt missing %q: %q", needle, prompt)
		}
	}
}

func TestBuildAlignmentPromptContainsInputs(t *testing.T) {
	alpha := Transcript{Text: "A says # VERDICT: CONFIRMED"}
	beta := Transcript{Text: "B says # VERDICT: CONFIRMED"}
	issue := "ISSUE: sample"
	prompt := buildAlignmentPrompt(issue, alpha, beta)
	required := []string{
		issue,
		alpha.Text,
		beta.Text,
		"Reply ONLY JSON",
		"agree=true",
	}
	for _, needle := range required {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("alignment prompt missing %q: %q", needle, prompt)
		}
	}
}

func TestBuildScoutPromptWritesToPath(t *testing.T) {
	prompt := buildScoutPrompt("task", "/workspace/change_analysis.md")
	required := []string{
		"Role: SCOUT",
		universalStudyLine,
		"Write the analysis to:",
		"/workspace/change_analysis.md",
		"# CHANGE ANALYSIS",
		"High-Risk Areas (ranked)",
		"Before -> After",
		"git merge-base HEAD BASE_BRANCH",
		"git diff --name-status MERGE_BASE_SHA",
		"cargo check --all-targets",
		"cargo clippy --all-targets",
	}
	for _, needle := range required {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("scout prompt missing %q: %q", needle, prompt)
		}
	}
}

func TestExtractTranscriptVerdict(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		want     string
		wantFind bool
	}{
		{
			name:     "confirmed marker",
			input:    "# VERDICT: CONFIRMED\n\n## Reasoning\nok",
			want:     "confirmed",
			wantFind: true,
		},
		{
			name:     "rejected marker lower-case",
			input:    "   # verdict: rejected\nDetails...",
			want:     "rejected",
			wantFind: true,
		},
		{
			name:     "bracketed marker",
			input:    "# VERDICT: [CONFIRMED]\nEvidence",
			want:     "confirmed",
			wantFind: true,
		},
		{
			name:     "ignores quoted marker",
			input:    "> # VERDICT: CONFIRMED\n\nNo explicit marker here",
			want:     "",
			wantFind: false,
		},
		{
			name:     "no marker",
			input:    "I think this is a bug but forgot the header",
			want:     "",
			wantFind: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision, ok := extractTranscriptVerdict(tc.input)
			if ok != tc.wantFind {
				t.Fatalf("expected found=%v, got %v (decision=%+v)", tc.wantFind, ok, decision)
			}
			if !ok {
				return
			}
			if decision.Verdict != tc.want {
				t.Fatalf("expected verdict %q, got %q", tc.want, decision.Verdict)
			}
			if decision.Reason == "" {
				t.Fatalf("expected non-empty reason")
			}
		})
	}
}
