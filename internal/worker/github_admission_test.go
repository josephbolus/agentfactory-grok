package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/josephbolus/agentfactory-grok/internal/protocol"
)

func TestVerifyGitHubAdmissionRefusesStaleWork(t *testing.T) {
	claim := protocol.Claim{
		Session: protocol.ClaimedSession{
			Target: protocol.WorkTarget{
				SourceKind:      "github_issue",
				SourceReference: "https://github.com/acme/api/issues/4",
			},
		},
	}
	manager := &Manager{
		issueAdmission: func(context.Context, protocol.Claim) (protocol.GitHubIssueAdmission, error) {
			return protocol.GitHubIssueAdmission{
				Labels: []protocol.GitHubLabel{{Name: protocol.NeedsAgentLabel}},
				ProjectItems: []protocol.GitHubProjectItem{{
					Status: protocol.GitHubProjectStatus{Name: protocol.ProjectStatusReview},
				}},
			}, nil
		},
	}
	if err := manager.verifyGitHubAdmission(context.Background(), claim); err == nil {
		t.Fatal("stale Project status was admitted")
	}
	manager.issueAdmission = func(context.Context, protocol.Claim) (protocol.GitHubIssueAdmission, error) {
		return protocol.GitHubIssueAdmission{
			Labels: []protocol.GitHubLabel{{Name: "enhancement"}},
			ProjectItems: []protocol.GitHubProjectItem{{
				Status: protocol.GitHubProjectStatus{Name: protocol.ProjectStatusReady},
			}},
		}, nil
	}
	if err := manager.verifyGitHubAdmission(context.Background(), claim); err == nil {
		t.Fatal("missing needs-agent was admitted")
	}
	manager.issueAdmission = func(context.Context, protocol.Claim) (protocol.GitHubIssueAdmission, error) {
		return protocol.GitHubIssueAdmission{}, errors.New("gh failed")
	}
	if err := manager.verifyGitHubAdmission(context.Background(), claim); err == nil {
		t.Fatal("github read failure was ignored")
	}
	manager.issueAdmission = func(context.Context, protocol.Claim) (protocol.GitHubIssueAdmission, error) {
		return protocol.GitHubIssueAdmission{
			Labels: []protocol.GitHubLabel{{Name: protocol.NeedsAgentLabel}},
			ProjectItems: []protocol.GitHubProjectItem{{
				Status: protocol.GitHubProjectStatus{Name: protocol.ProjectStatusReady},
			}},
		}, nil
	}
	if err := manager.verifyGitHubAdmission(context.Background(), claim); err != nil {
		t.Fatal(err)
	}
	taskClaim := protocol.Claim{Session: protocol.ClaimedSession{Target: protocol.WorkTarget{SourceKind: "repository"}}}
	if err := manager.verifyGitHubAdmission(context.Background(), taskClaim); err != nil {
		t.Fatal(err)
	}
}

func TestAdvertisedRuntimesIncludePiCodexAndClaudeCode(t *testing.T) {
	got := protocol.SupportedRuntimes()
	want := map[string]bool{
		protocol.RuntimePi: true, protocol.RuntimeCodex: true, protocol.RuntimeClaudeCode: true,
	}
	if len(got) != 3 {
		t.Fatalf("supported runtimes = %#v", got)
	}
	for _, runtime := range got {
		if !want[runtime] {
			t.Fatalf("unexpected runtime %q", runtime)
		}
	}
	manager := &Manager{
		config: Config{Name: "local", Runtime: protocol.RuntimeCodex, Runtimes: got, MaxConcurrent: 1},
		health: health{
			State: "healthy",
			Capabilities: []protocol.Capability{
				{Kind: protocol.CapabilityKindRuntime, Name: protocol.RuntimePi, Status: protocol.CapabilityReady},
				{Kind: protocol.CapabilityKindRuntime, Name: protocol.RuntimeCodex, Status: protocol.CapabilityReady},
				{Kind: protocol.CapabilityKindRuntime, Name: protocol.RuntimeClaudeCode, Status: protocol.CapabilityReady},
			},
		},
	}
	registration := manager.registrationLocked()
	seen := map[string]bool{}
	for _, capability := range registration.Capabilities {
		if capability.Kind == protocol.CapabilityKindRuntime {
			seen[capability.Name] = true
		}
	}
	for _, runtime := range got {
		if !seen[runtime] {
			t.Fatalf("registration omitted runtime %q: %#v", runtime, registration.Capabilities)
		}
	}
}
