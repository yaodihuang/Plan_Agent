package prreview

import "testing"

func TestDetermineVerdictFallsBackToLLMOverrideWhenMarkerIsNotRegexParsable(t *testing.T) {
	called := 0
	r := &Runner{
		verdictOverride: func(transcript Transcript) (verdictDecision, error) {
			called++
			return verdictDecision{Verdict: "confirmed", Reason: "override"}, nil
		},
	}

	// The explicit verdict exists but is wrapped in markdown list + inline code,
	// which the strict regex-based extractor does not parse.
	transcript := Transcript{
		Agent: "reviewer",
		Round: 1,
		Text:  "Wrote the verdict:\n\n- `# VERDICT: CONFIRMED`\n- `Severity: P0`\n",
	}

	decision, err := r.determineVerdict(transcript)
	if err != nil {
		t.Fatalf("determineVerdict error: %v", err)
	}
	if decision.Verdict != "confirmed" {
		t.Fatalf("expected verdict %q, got %q (decision=%+v)", "confirmed", decision.Verdict, decision)
	}
	if called != 1 {
		t.Fatalf("expected verdictOverride to be called once, got %d", called)
	}
}

func TestDetermineVerdictPrefersExplicitMarkerOverLLMOverride(t *testing.T) {
	r := &Runner{
		verdictOverride: func(Transcript) (verdictDecision, error) {
			t.Fatalf("verdictOverride should not be called when explicit marker is present")
			return verdictDecision{}, nil
		},
	}
	transcript := Transcript{
		Agent: "reviewer",
		Round: 1,
		Text:  "# VERDICT: REJECTED\n\nClaim: test\nAnchor: unknown\n",
	}
	decision, err := r.determineVerdict(transcript)
	if err != nil {
		t.Fatalf("determineVerdict error: %v", err)
	}
	if decision.Verdict != "rejected" {
		t.Fatalf("expected verdict %q, got %q (decision=%+v)", "rejected", decision.Verdict, decision)
	}
	if decision.Reason == "" {
		t.Fatalf("expected non-empty reason")
	}
}
