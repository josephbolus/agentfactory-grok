package controlplane

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/josephbolus/agentfactory-grok/internal/protocol"
)

type fakeGitHubIssues struct {
	issues           map[string][]githubIssue
	pullRequests     map[string][]githubPullRequest
	calls            []string
	pullRequestCalls []string
}

func (f *fakeGitHubIssues) ListPullRequests(_ context.Context, repository string) ([]githubPullRequest, error) {
	f.pullRequestCalls = append(f.pullRequestCalls, repository)
	return f.pullRequests[repository], nil
}

func (f *fakeGitHubIssues) ListIssues(_ context.Context, repository string) ([]githubIssue, error) {
	f.calls = append(f.calls, repository)
	return f.issues[repository], nil
}

func readyIssue(number int, title, url string, labels ...string) githubIssue {
	issue := githubIssue{
		Number: number, Title: title, URL: url,
		ProjectItems: []githubProjectItem{{Status: githubProjectStatus{Name: protocol.ProjectStatusReady}}},
	}
	hasAgent := false
	for _, name := range labels {
		issue.Labels = append(issue.Labels, githubIssueLabel{Name: name})
		if strings.EqualFold(name, protocol.NeedsAgentLabel) {
			hasAgent = true
		}
	}
	if !hasAgent {
		issue.Labels = append(issue.Labels, githubIssueLabel{Name: protocol.NeedsAgentLabel})
	}
	return issue
}

func countRuns(t *testing.T, store *Store) int {
	t.Helper()
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM runs`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func mustEnableIntake(t *testing.T, store *Store, remote string) protocol.ManagedRepository {
	t.Helper()
	repository, _, err := store.CreateManagedRepository(context.Background(), protocol.CreateManagedRepositoryRequest{
		RemoteIdentity: remote,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.setManagedRepositoryIssueIntake(context.Background(), repository.ID, true); err != nil {
		t.Fatal(err)
	}
	return repository
}

func TestGitHubIssueIntakeAdmitsEachIssueOnce(t *testing.T) {
	store := newTestStore(t)
	repository, _, err := store.CreateManagedRepository(context.Background(), protocol.CreateManagedRepositoryRequest{
		RemoteIdentity: "github.com/acme/api",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.setManagedRepositoryIssueIntake(context.Background(), repository.ID, true); err != nil {
		t.Fatal(err)
	}
	source := &fakeGitHubIssues{issues: map[string][]githubIssue{
		repository.RemoteIdentity: {readyIssue(42, "Fix case-insensitive search", "https://github.com/acme/api/issues/42")},
	}}
	store.githubIssues = source

	if err := store.PollGitHubIssues(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.PollGitHubIssues(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(source.calls) != 2 {
		t.Fatalf("GitHub issue calls = %d, want 2", len(source.calls))
	}
	var runCount int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM runs`).Scan(&runCount); err != nil {
		t.Fatal(err)
	}
	if runCount != 1 {
		t.Fatalf("runs = %d, want 1", runCount)
	}
	var runID string
	if err := store.db.QueryRow(`SELECT id FROM runs`).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	run, err := store.Run(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Run.OutcomeContract != protocol.OutcomeProcessExit ||
		strings.Contains(run.Sessions[0].ResolvedPrompt, "factory update") {
		t.Fatalf("GitHub intake Run = %#v", run)
	}
	if run.Run.Targets[0].SourceTitle != "Fix case-insensitive search" ||
		run.Sessions[0].Target.SourceTitle != "Fix case-insensitive search" {
		t.Fatalf("GitHub intake title = %#v, %#v", run.Run.Targets[0], run.Sessions[0].Target)
	}
	var sourceReference string
	if err := store.db.QueryRow(`SELECT source_reference FROM sessions`).Scan(&sourceReference); err != nil {
		t.Fatal(err)
	}
	if sourceReference != "https://github.com/acme/api/issues/42" {
		t.Fatalf("source reference = %q", sourceReference)
	}
}

func TestGitHubIssueIntakeRequiresRepositoryOptIn(t *testing.T) {
	store := newTestStore(t)
	repository, _, err := store.CreateManagedRepository(context.Background(), protocol.CreateManagedRepositoryRequest{
		RemoteIdentity: "github.com/acme/api",
	})
	if err != nil {
		t.Fatal(err)
	}
	source := &fakeGitHubIssues{issues: map[string][]githubIssue{
		repository.RemoteIdentity: {readyIssue(42, "", "https://github.com/acme/api/issues/42")},
	}}
	store.githubIssues = source

	if err := store.PollGitHubIssues(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(source.calls) != 0 {
		t.Fatalf("GitHub issue calls = %d, want 0", len(source.calls))
	}
}

func TestGitHubPullRequestWakeRequiresLinkedTerminalWork(t *testing.T) {
	store := newTestStore(t)
	repository, _, err := store.CreateManagedRepository(context.Background(), protocol.CreateManagedRepositoryRequest{
		RemoteIdentity: "github.com/acme/api",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.setManagedRepositoryIssueIntake(context.Background(), repository.ID, true); err != nil {
		t.Fatal(err)
	}
	admission, err := store.admitGitHubIssueBuild(context.Background(), protocol.BuildRequest{
		RequestKey: "issue-42", References: []string{"https://github.com/acme/api/issues/42"},
	}, "Fix case-insensitive search")
	if err != nil {
		t.Fatal(err)
	}
	work := admission.Run.Sessions[0]
	pullRequestURL := "https://github.com/acme/api/pull/7"
	if _, err := store.db.Exec(`
		UPDATE sessions SET state = 'ready', terminal_at = ?, pull_request_url = ? WHERE id = ?
	`, store.now().UnixMilli(), pullRequestURL, work.ID); err != nil {
		t.Fatal(err)
	}
	source := &fakeGitHubIssues{pullRequests: map[string][]githubPullRequest{
		repository.RemoteIdentity: {{URL: pullRequestURL}},
	}}
	store.githubIssues = source

	if err := store.PollGitHubIssues(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.PollGitHubIssues(context.Background()); err != nil {
		t.Fatal(err)
	}
	var replacementCount int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE predecessor_work_id = ?`, work.ID).Scan(&replacementCount); err != nil {
		t.Fatal(err)
	}
	if replacementCount != 1 {
		t.Fatalf("replacement Work = %d, want 1", replacementCount)
	}
	var replacementTitle string
	if err := store.db.QueryRow(`SELECT source_title FROM sessions WHERE predecessor_work_id = ?`, work.ID).Scan(&replacementTitle); err != nil {
		t.Fatal(err)
	}
	if replacementTitle != "Fix case-insensitive search" {
		t.Fatalf("replacement source title = %q", replacementTitle)
	}
	var replacementID string
	if err := store.db.QueryRow(`SELECT id FROM sessions WHERE predecessor_work_id = ?`, work.ID).Scan(&replacementID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`
		UPDATE sessions SET state = 'ready', terminal_at = ?, pull_request_url = ? WHERE id = ?
	`, store.now().UnixMilli(), pullRequestURL, replacementID); err != nil {
		t.Fatal(err)
	}
	source.pullRequests[repository.RemoteIdentity] = nil
	if err := store.PollGitHubIssues(context.Background()); err != nil {
		t.Fatal(err)
	}
	source.pullRequests[repository.RemoteIdentity] = []githubPullRequest{{URL: pullRequestURL}}
	if err := store.PollGitHubIssues(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&replacementCount); err != nil {
		t.Fatal(err)
	}
	if replacementCount != 3 {
		t.Fatalf("sessions after re-labelling PR = %d, want 3", replacementCount)
	}

	source.pullRequests[repository.RemoteIdentity] = []githubPullRequest{{URL: "https://github.com/acme/api/pull/99"}}
	if err := store.PollGitHubIssues(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&replacementCount); err != nil {
		t.Fatal(err)
	}
	if replacementCount != 3 {
		t.Fatalf("sessions after unrelated PR = %d, want 3", replacementCount)
	}
}

func TestGitHubIssueIntakeRequiresNeedsAgentAndProjectReady(t *testing.T) {
	store := newTestStore(t)
	repository := mustEnableIntake(t, store, "github.com/acme/api")
	missingAgent := readyIssue(1, "No agent label", "https://github.com/acme/api/issues/1")
	missingAgent.Labels = []githubIssueLabel{{Name: "enhancement"}}
	todo := readyIssue(2, "Todo column", "https://github.com/acme/api/issues/2")
	todo.ProjectItems = []githubProjectItem{{Status: githubProjectStatus{Name: protocol.ProjectStatusTodo}}}
	untracked := readyIssue(3, "No project", "https://github.com/acme/api/issues/3")
	untracked.ProjectItems = nil
	source := &fakeGitHubIssues{issues: map[string][]githubIssue{
		repository.RemoteIdentity: {missingAgent, todo, untracked},
	}}
	store.githubIssues = source
	if err := store.PollGitHubIssues(context.Background()); err != nil {
		t.Fatal(err)
	}
	if countRuns(t, store) != 0 {
		t.Fatalf("admitted %d runs without needs-agent and Ready", countRuns(t, store))
	}
	source.issues[repository.RemoteIdentity] = []githubIssue{
		readyIssue(4, "Ready work", "https://github.com/acme/api/issues/4"),
	}
	if err := store.PollGitHubIssues(context.Background()); err != nil {
		t.Fatal(err)
	}
	if countRuns(t, store) != 1 {
		t.Fatalf("runs = %d, want 1 after Ready + needs-agent", countRuns(t, store))
	}
}

func TestGitHubIssueIntakeFreezesOneWorkflowAndRearmsOnLeave(t *testing.T) {
	store := newTestStore(t)
	repository := mustEnableIntake(t, store, "github.com/acme/api")
	source := &fakeWorkflowSource{
		commit: "abc123deadbeef",
		files: map[string][]byte{
			".factory/workflows/implement.md": []byte("---\nid: implement\ntitle: Implement\ngithub_issue:\n  labels_all: [factory:ready-to-implement]\n---\nImplement the issue.\n"),
			".factory/workflows/triage.md":    []byte("---\nid: triage\ntitle: Triage\ngithub_issue:\n  labels_all: [factory:ready-for-spec]\n---\nTriage the issue.\n"),
		},
		issues: map[string][]githubIssue{
			repository.RemoteIdentity: {readyIssue(7, "Search bug", "https://github.com/acme/api/issues/7", "factory:ready-to-implement")},
		},
	}
	store.githubIssues = source
	if err := store.PollGitHubIssues(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.PollGitHubIssues(context.Background()); err != nil {
		t.Fatal(err)
	}
	if countRuns(t, store) != 1 {
		t.Fatalf("second poll while labels stay set created %d runs", countRuns(t, store))
	}
	work := mustOnlyWork(t, store)
	assertOneWorkflow(t, work, "implement", ".factory/workflows/implement.md", "abc123deadbeef")
	if _, err := store.db.Exec(`UPDATE sessions SET state = 'succeeded', terminal_at = ? WHERE id = ?`, store.now().UnixMilli(), work.ID); err != nil {
		t.Fatal(err)
	}
	source.issues[repository.RemoteIdentity] = nil
	if err := store.PollGitHubIssues(context.Background()); err != nil {
		t.Fatal(err)
	}
	if countRuns(t, store) != 1 {
		t.Fatalf("leaving the label set created work: %d", countRuns(t, store))
	}
	source.issues[repository.RemoteIdentity] = []githubIssue{
		readyIssue(7, "Search bug", "https://github.com/acme/api/issues/7", "factory:ready-to-implement"),
	}
	if err := store.PollGitHubIssues(context.Background()); err != nil {
		t.Fatal(err)
	}
	if countRuns(t, store) != 2 {
		t.Fatalf("returning after leave-label rearm created %d runs, want 2", countRuns(t, store))
	}
}

func TestGitHubIssueIntakeAmbiguousLabelsAllBlocks(t *testing.T) {
	store := newTestStore(t)
	repository := mustEnableIntake(t, store, "github.com/acme/api")
	source := &fakeWorkflowSource{
		commit: "commit-1",
		files: map[string][]byte{
			".factory/workflows/a.md": []byte("---\nid: a\ntitle: A\ngithub_issue:\n  labels_all: [team:qa]\n---\na\n"),
			".factory/workflows/b.md": []byte("---\nid: b\ntitle: B\ngithub_issue:\n  labels_all: [team:qa]\n---\nb\n"),
		},
		issues: map[string][]githubIssue{
			repository.RemoteIdentity: {readyIssue(8, "QA", "https://github.com/acme/api/issues/8", "team:qa")},
		},
	}
	store.githubIssues = source
	if err := store.PollGitHubIssues(context.Background()); err == nil {
		t.Fatal("ambiguous labels_all did not block")
	}
	if countRuns(t, store) != 0 {
		t.Fatalf("ambiguous match admitted %d runs", countRuns(t, store))
	}
}

func TestGitHubIssueIntakeZeroMatchFallsBackToImplement(t *testing.T) {
	store := newTestStore(t)
	repository := mustEnableIntake(t, store, "github.com/acme/api")
	source := &fakeWorkflowSource{
		commit: "commit-fallback",
		files: map[string][]byte{
			".factory/workflows/implement.md": []byte("---\nid: implement\ntitle: Implement\n---\nFallback implement.\n"),
			".factory/workflows/dba.md":       []byte("---\nid: dba\ntitle: DBA\ngithub_issue:\n  labels_all: [team:dba]\n---\nDBA\n"),
		},
		issues: map[string][]githubIssue{
			repository.RemoteIdentity: {readyIssue(9, "Generic", "https://github.com/acme/api/issues/9")},
		},
	}
	store.githubIssues = source
	if err := store.PollGitHubIssues(context.Background()); err != nil {
		t.Fatal(err)
	}
	work := mustOnlyWork(t, store)
	assertOneWorkflow(t, work, "implement", ".factory/workflows/implement.md", "commit-fallback")
}

func TestTaskAndProcedureWorkStoresOneWorkflow(t *testing.T) {
	store := newTestStore(t)
	repository, _, err := store.CreateManagedRepository(context.Background(), protocol.CreateManagedRepositoryRequest{
		RemoteIdentity: "github.com/acme/api",
	})
	if err != nil {
		t.Fatal(err)
	}
	registerTestWorker(t, store, workerA, 2, protocol.RepositoryRegistration{
		Key: "api", RemoteIdentity: repository.RemoteIdentity,
	})
	task, err := store.CreateTask(context.Background(), protocol.SaveTaskRequest{
		Name: "Nightly scan", Prompt: "Scan for stale TODOs.", Runtime: protocol.RuntimeCodex,
		TimeoutSeconds: 3600, ConcurrencyLimit: 1, RepositoryIDs: []string{repository.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := store.RunTask(context.Background(), task.ID, protocol.RunTaskRequest{RequestKey: "task-one-workflow"})
	if err != nil {
		t.Fatal(err)
	}
	assertOneWorkflow(t, run.Sessions[0], "procedure", ".factory/workflows/procedure.md", "")
	admission, err := store.AdmitProcedureRun(context.Background(), protocol.ProcedureRunRequest{
		RequestKey: "procedure-one-workflow", Procedure: task.Name, AllRepositories: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertOneWorkflow(t, admission.Run.Sessions[0], "procedure", ".factory/workflows/procedure.md", "")
	now := time.Date(2026, time.August, 31, 8, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	scheduled, err := store.CreateTask(context.Background(), protocol.SaveTaskRequest{
		Name: "Scheduled scan", Prompt: "Scan on a schedule.", Runtime: protocol.RuntimePi,
		TimeoutSeconds: 3600, ConcurrencyLimit: 1, RepositoryIDs: []string{repository.ID},
		Schedule: protocol.TaskSchedule{Enabled: true, Cron: "0 9 * * *", Timezone: "UTC"},
	})
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return time.Date(2026, time.August, 31, 10, 0, 0, 0, time.UTC) }
	if err := store.AdmitDueTasks(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	page, err := store.RunPage(context.Background(), 20, "")
	if err != nil {
		t.Fatal(err)
	}
	var scheduledWork protocol.Work
	for _, run := range page.Runs {
		if run.Task.Name == scheduled.Name {
			detail, err := store.Run(context.Background(), run.ID)
			if err != nil {
				t.Fatal(err)
			}
			scheduledWork = detail.Sessions[0]
		}
	}
	assertOneWorkflow(t, scheduledWork, "procedure", ".factory/workflows/procedure.md", "")
}

func mustOnlyWork(t *testing.T, store *Store) protocol.Work {
	t.Helper()
	var id string
	if err := store.db.QueryRow(`SELECT id FROM sessions ORDER BY admitted_at, id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	work, err := store.Work(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return work
}

func assertOneWorkflow(t *testing.T, work protocol.Work, id, path, sha string) {
	t.Helper()
	if len(work.Stages) != 0 {
		t.Fatalf("Work stored a stage list: %#v", work.Stages)
	}
	if work.Workflow.ID != id || work.Workflow.Path != path {
		t.Fatalf("workflow id/path = %q %q, want %q %q", work.Workflow.ID, work.Workflow.Path, id, path)
	}
	if sha != "" && work.Workflow.CommitSHA != sha {
		t.Fatalf("workflow sha = %q, want %q", work.Workflow.CommitSHA, sha)
	}
	if work.Workflow.CommitSHA == "" || work.Workflow.Digest == "" || strings.TrimSpace(work.Workflow.Instructions) == "" {
		t.Fatalf("incomplete workflow snapshot: %#v", work.Workflow)
	}
}
