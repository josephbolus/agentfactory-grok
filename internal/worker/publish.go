package worker

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/josephbolus/agentfactory-grok/internal/protocol"
)

type updateValidationError struct {
	code      string
	message   string
	retriable bool
	err       error
}

func (err *updateValidationError) Error() string {
	if err.err != nil {
		return err.message + ": " + err.err.Error()
	}
	return err.message
}

type readyDeliveryEvidence struct {
	HeadBranch string
	HeadSHA    string
}

type checkpointEvidence struct {
	SHA       string
	Published bool
}

func (manager *Manager) validateNeedsInputCheckpoint(
	ctx context.Context,
	claim protocol.Claim,
	repository Repository,
	value worktree,
) (checkpointEvidence, *updateValidationError) {
	release, err := manager.repositoryLocks.acquire(ctx, repositoryCoordinationKey(repository))
	if err != nil {
		return checkpointEvidence{}, &updateValidationError{
			code: "checkpoint_validation_unavailable", message: "checkpoint validation is temporarily unavailable",
			retriable: true, err: err,
		}
	}
	defer release()
	if err := validateRegisteredOrigin(ctx, manager.options.GitExecutable, repository); err != nil {
		return checkpointEvidence{}, &updateValidationError{
			code: "checkpoint_validation_unavailable", message: "repository origin validation failed",
			retriable: true, err: err,
		}
	}
	stdout, stderr, err := runGitCommand(
		ctx, manager.options.GitExecutable, value.Path, 256<<10,
		"status", "--porcelain=v1", "-z", "--untracked-files=all",
	)
	if err != nil {
		return checkpointEvidence{}, &updateValidationError{
			code: "checkpoint_validation_unavailable", message: "the Work worktree could not be inspected",
			retriable: true, err: commandFailure("inspect checkpoint worktree", stdout, stderr, err),
		}
	}
	if len(stdout) != 0 {
		return checkpointEvidence{}, &updateValidationError{
			code:    "checkpoint_worktree_dirty",
			message: "needs-input requires a clean worktree; commit or remove every changed and untracked file",
		}
	}
	stdout, stderr, err = runGitCommand(
		ctx, manager.options.GitExecutable, value.Path, 64<<10,
		"rev-parse", "--verify", "HEAD^{commit}",
	)
	if err != nil || !commitPattern.MatchString(strings.TrimSpace(string(stdout))) {
		return checkpointEvidence{}, &updateValidationError{
			code: "checkpoint_validation_unavailable", message: "local HEAD could not be resolved",
			retriable: true, err: commandFailure("resolve checkpoint HEAD", stdout, stderr, err),
		}
	}
	localSHA := strings.TrimSpace(string(stdout))
	remoteSHA, found, err := remotePublishCommitOptional(
		ctx, manager.options.GitExecutable, repository, claim.Session.Target.PublishBranch,
	)
	if err != nil {
		return checkpointEvidence{}, &updateValidationError{
			code: "checkpoint_validation_unavailable", message: "the Work publish branch could not be checked",
			retriable: true, err: err,
		}
	}
	if found {
		if remoteSHA != localSHA {
			return checkpointEvidence{}, &updateValidationError{
				code:    "checkpoint_head_mismatch",
				message: "local HEAD must match the fetched immutable Factory publish branch before needs-input",
			}
		}
		return checkpointEvidence{SHA: localSHA, Published: true}, nil
	}
	if localSHA != value.BaseCommit {
		return checkpointEvidence{}, &updateValidationError{
			code:    "checkpoint_publish_required",
			message: "changed Work must be committed and pushed to the immutable Factory publish branch before needs-input",
		}
	}
	return checkpointEvidence{SHA: localSHA}, nil
}

type gitHubPullRequest struct {
	HTMLURL string `json:"html_url"`
	Head    struct {
		Ref  string `json:"ref"`
		SHA  string `json:"sha"`
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
}

func (manager *Manager) validateReadyDelivery(
	ctx context.Context,
	claim protocol.Claim,
	repository Repository,
	value worktree,
	pullRequestURL string,
) (readyDeliveryEvidence, *updateValidationError) {
	owner, name, number, err := parseGitHubPullRequestURL(pullRequestURL)
	if err != nil {
		return readyDeliveryEvidence{}, &updateValidationError{code: "invalid_pull_request", message: err.Error()}
	}
	expectedRepository := strings.TrimPrefix(strings.ToLower(claim.Repository.RemoteIdentity), "github.com/")
	if !strings.EqualFold(owner+"/"+name, expectedRepository) {
		return readyDeliveryEvidence{}, &updateValidationError{
			code: "delivery_repository_mismatch", message: "the pull request belongs to a different repository",
		}
	}
	stdout, stderr, commandErr := runCommand(ctx, manager.options.GitHubExecutable, value.Path, 256<<10,
		"api", "--method", "GET", "repos/"+owner+"/"+name+"/pulls/"+strconv.FormatInt(number, 10))
	if commandErr != nil {
		return readyDeliveryEvidence{}, &updateValidationError{
			code: "github_validation_unavailable", message: "GitHub pull request validation is temporarily unavailable",
			retriable: true, err: commandFailure("read pull request", stdout, stderr, commandErr),
		}
	}
	var pullRequest gitHubPullRequest
	if err := json.Unmarshal(stdout, &pullRequest); err != nil || pullRequest.Head.Ref == "" ||
		!commitPattern.MatchString(pullRequest.Head.SHA) || pullRequest.Head.Repo.FullName == "" {
		return readyDeliveryEvidence{}, &updateValidationError{
			code: "github_validation_unavailable", message: "GitHub returned incomplete pull request evidence", retriable: true,
		}
	}
	if !strings.EqualFold(pullRequest.Head.Repo.FullName, expectedRepository) {
		return readyDeliveryEvidence{}, &updateValidationError{
			code: "delivery_repository_mismatch", message: "the pull request head belongs to a different repository",
		}
	}
	if pullRequest.Head.Ref != claim.Session.Target.PublishBranch {
		return readyDeliveryEvidence{}, &updateValidationError{
			code: "delivery_branch_mismatch", message: "the pull request head branch does not match the Work publish branch",
		}
	}

	release, err := manager.repositoryLocks.acquire(ctx, repositoryCoordinationKey(repository))
	if err != nil {
		return readyDeliveryEvidence{}, &updateValidationError{
			code: "delivery_validation_unavailable", message: "repository delivery validation is temporarily unavailable", retriable: true, err: err,
		}
	}
	defer release()
	if err := validateRegisteredOrigin(ctx, manager.options.GitExecutable, repository); err != nil {
		return readyDeliveryEvidence{}, &updateValidationError{
			code: "delivery_validation_unavailable", message: "repository origin validation failed", retriable: true, err: err,
		}
	}
	remoteSHA, err := remotePublishCommit(ctx, manager.options.GitExecutable, repository, claim.Session.Target.PublishBranch)
	if err != nil {
		return readyDeliveryEvidence{}, &updateValidationError{
			code: "publish_ref_unavailable", message: "the Work publish branch could not be fetched", retriable: true, err: err,
		}
	}
	stdout, stderr, err = runGitCommand(ctx, manager.options.GitExecutable, value.Path, 64<<10, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return readyDeliveryEvidence{}, &updateValidationError{
			code: "delivery_validation_unavailable", message: "local HEAD could not be resolved", retriable: true,
			err: commandFailure("resolve local HEAD", stdout, stderr, err),
		}
	}
	localSHA := strings.TrimSpace(string(stdout))
	if localSHA != remoteSHA || localSHA != pullRequest.Head.SHA {
		return readyDeliveryEvidence{}, &updateValidationError{
			code: "delivery_head_mismatch", message: "local HEAD, the fetched publish ref, and the pull request head SHA must match",
		}
	}
	return readyDeliveryEvidence{HeadBranch: pullRequest.Head.Ref, HeadSHA: pullRequest.Head.SHA}, nil
}

func parseGitHubPullRequestURL(value string) (string, string, int64, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "github.com") ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", 0, errors.New("ready requires an HTTPS github.com pull request URL")
	}
	parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	if len(parts) != 4 || parts[0] == "" || parts[1] == "" || parts[2] != "pull" {
		return "", "", 0, errors.New("ready requires a URL shaped like https://github.com/OWNER/REPOSITORY/pull/NUMBER")
	}
	owner, ownerErr := url.PathUnescape(parts[0])
	name, nameErr := url.PathUnescape(parts[1])
	number, numberErr := strconv.ParseInt(parts[3], 10, 64)
	if ownerErr != nil || nameErr != nil || numberErr != nil || number < 1 || strings.ContainsAny(owner+name, "/\\") {
		return "", "", 0, errors.New("pull request URL contains invalid repository or number fields")
	}
	return owner, name, number, nil
}

func remotePublishCommit(
	ctx context.Context,
	gitExecutable string,
	repository Repository,
	branch string,
) (string, error) {
	commit, found, err := remotePublishCommitOptional(ctx, gitExecutable, repository, branch)
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("publish branch does not exist")
	}
	return commit, nil
}

func remotePublishCommitOptional(
	ctx context.Context,
	gitExecutable string,
	repository Repository,
	branch string,
) (string, bool, error) {
	if err := validateBaseBranch(ctx, gitExecutable, repository, branch); err != nil {
		return "", false, err
	}
	stdout, stderr, err := runGitCommand(ctx, gitExecutable, repository.Path, 64<<10,
		"ls-remote", "--refs", "origin", "refs/heads/"+branch)
	if err != nil {
		return "", false, commandFailure("resolve publish branch", stdout, stderr, err)
	}
	fields := strings.Fields(string(stdout))
	if len(fields) == 0 {
		return "", false, nil
	}
	if len(fields) != 2 || fields[1] != "refs/heads/"+branch || !commitPattern.MatchString(fields[0]) {
		return "", false, errors.New("publish branch returned malformed Git evidence")
	}
	commit := fields[0]
	stdout, stderr, err = runGitCommand(ctx, gitExecutable, repository.Path, 256<<10,
		"fetch", "--no-tags", "--no-write-fetch-head", "--refmap=", "origin", "refs/heads/"+branch)
	if err != nil {
		return "", false, commandFailure("fetch publish branch", stdout, stderr, err)
	}
	stdout, stderr, err = runGitCommand(ctx, gitExecutable, repository.Path, 64<<10,
		"ls-remote", "--refs", "origin", "refs/heads/"+branch)
	if err != nil {
		return "", false, commandFailure("recheck publish branch", stdout, stderr, err)
	}
	current := strings.Fields(string(stdout))
	if len(current) != 2 || current[1] != "refs/heads/"+branch || current[0] != commit {
		return "", false, errors.New("publish branch moved during validation")
	}
	stdout, stderr, err = runGitCommand(ctx, gitExecutable, repository.Path, 64<<10,
		"rev-parse", "--verify", commit+"^{commit}")
	if err != nil || strings.TrimSpace(string(stdout)) != commit {
		if err == nil {
			err = errors.New("fetched publish branch did not contain its advertised commit")
		}
		return "", false, commandFailure("verify fetched publish branch", stdout, stderr, err)
	}
	return commit, true, nil
}
