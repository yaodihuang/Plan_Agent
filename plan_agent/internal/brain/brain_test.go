package brain

import "testing"

func TestCompleteReturnsErrorForInvalidURL(t *testing.T) {
	brain := NewLLMBrain("key", "http://[::1", "dep", "2024-12-01-preview", 1)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()
	_, err := brain.Complete([]ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}
