package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/josephbolus/agentfactory-grok/internal/protocol"
)

func TestBuildPromptIncludesGrammaticalSafetyInstruction(t *testing.T) {
	claim := protocol.Claim{
		Session: protocol.ClaimedSession{
			TaskName: "Fix the prompt",
			Prompt:   "Keep the change focused.",
		},
		Repository: protocol.Repository{RemoteIdentity: "github.com/josephbolus/agentfactory-grok"},
	}
	value := worktree{Branch: "factory/123456789abc-abcdef123456", BaseBranch: "main"}

	want := "You are running in a Factory managed Git worktree.\n" +
		"Work only on the assigned Session and repository. Preserve unrelated changes and do not touch Factory state or unrelated worktrees. " +
		"Do not switch, create, rename, or delete branches or worktrees. Complete and verify the Session before returning a concise result.\n\n" +
		"Task: Fix the prompt\n" +
		"Repository: github.com/josephbolus/agentfactory-grok\n" +
		"Working branch: factory/123456789abc-abcdef123456\n" +
		"Target base branch: main\n\n" +
		"Keep the change focused."

	if got := buildPrompt(claim, value); got != want {
		t.Fatalf("buildPrompt() = %q, want %q", got, want)
	}
}

func TestStageStartFailureReasonHonorsCancellation(t *testing.T) {
	tests := []struct {
		name, current, code, want string
		err                       error
	}{
		{name: "cancelled by control plane", code: "cancellation_requested", want: "cancelled"},
		{name: "lease lost", code: "lease_not_owner", want: "lease_lost"},
		{name: "other API error", code: "stage_not_pending", want: "failed"},
		{name: "transport error", err: errors.New("connection closed"), want: "failed"},
		{name: "existing timeout wins", current: "timeout", code: "cancellation_requested", want: "timeout"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.err
			if err == nil {
				err = &APIError{Code: test.code}
			}
			if got := stageStartFailureReason(test.current, err); got != test.want {
				t.Fatalf("reason = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSupervisorStopReasonPreservesLeaseLossAndCancellation(t *testing.T) {
	for reason, want := range map[string]string{
		"lease_lost": "lease_lost", "cancelled": "cancelled", "timeout": "timeout",
		"supervisor_error": "failed", "parent_lost": "failed", "outcome_reported": "", "exited": "",
	} {
		if got := attemptStopReasonForSupervisor(reason); got != want {
			t.Errorf("attemptStopReasonForSupervisor(%q) = %q, want %q", reason, got, want)
		}
	}
}

func TestCompletedWorktreeCleanupUsesAuthoritativeAttemptState(t *testing.T) {
	for _, testCase := range []struct {
		name                  string
		completed             bool
		authoritativeState    string
		retainReportedFailure bool
		want                  bool
	}{
		{name: "successful completion", completed: true, authoritativeState: "succeeded", want: true},
		{name: "cancellation wins", completed: true, authoritativeState: "cancelled"},
		{name: "reported failure retention", completed: true, authoritativeState: "succeeded", retainReportedFailure: true},
		{name: "completion rejected", authoritativeState: "succeeded"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got := shouldCleanCompletedWorktree(
				testCase.completed, testCase.authoritativeState, testCase.retainReportedFailure,
			)
			if got != testCase.want {
				t.Fatalf("shouldCleanCompletedWorktree() = %t, want %t", got, testCase.want)
			}
		})
	}
}

func TestFinalStopReasonIsRecheckedAfterPostflight(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handle := &attemptHandle{context: ctx, cancel: cancel, deadline: time.Now().Add(-time.Second)}
	if got := handle.stopReasonAt(time.Now()); got != "timeout" {
		t.Fatalf("expired finalization stop reason = %q", got)
	}
	state, result, errorText := terminalForFinalStop(
		handle.stopReason(), "succeeded", "ready result", "",
	)
	if state != "failed" || result != "" || errorText != "Session timeout reached" {
		t.Fatalf("timeout terminal = %q, %q, %q", state, result, errorText)
	}

	state, result, errorText = terminalForFinalStop("cancelled", "succeeded", "ready result", "")
	if state != "cancelled" || result != "" || errorText != "attempt cancelled" {
		t.Fatalf("cancellation terminal = %q, %q, %q", state, result, errorText)
	}
}
