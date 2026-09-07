package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/josephbolus/agentfactory-grok/internal/protocol"
)

type issueAdmissionFunc func(context.Context, protocol.Claim) (protocol.GitHubIssueAdmission, error)

func (manager *Manager) verifyGitHubAdmission(ctx context.Context, claim protocol.Claim) error {
	if claim.Session.Target.SourceKind != "github_issue" {
		return nil
	}
	check := manager.issueAdmission
	if check == nil {
		check = liveIssueAdmission(manager.options.GitHubExecutable)
	}
	state, err := check(ctx, claim)
	if err != nil {
		return err
	}
	if !state.Admissible() {
		return errors.New("stale GitHub admission: needs-agent and Project Ready are required")
	}
	return nil
}

func liveIssueAdmission(githubExecutable string) issueAdmissionFunc {
	if githubExecutable == "" {
		githubExecutable = "gh"
	}
	return func(ctx context.Context, claim protocol.Claim) (protocol.GitHubIssueAdmission, error) {
		reference := strings.TrimSpace(claim.Session.Target.SourceReference)
		if reference == "" {
			return protocol.GitHubIssueAdmission{}, errors.New("github issue reference is required")
		}
		command := exec.CommandContext(ctx, githubExecutable, "issue", "view", reference, "--json", "labels,projectItems")
		output, err := command.Output()
		if err != nil {
			return protocol.GitHubIssueAdmission{}, fmt.Errorf("read live GitHub issue: %v", err)
		}
		var state protocol.GitHubIssueAdmission
		if err := json.Unmarshal(output, &state); err != nil {
			return protocol.GitHubIssueAdmission{}, fmt.Errorf("decode live GitHub issue: %v", err)
		}
		return state, nil
	}
}
