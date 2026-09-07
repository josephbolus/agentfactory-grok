package protocol

import "strings"

const (
	NeedsAgentLabel      = "needs-agent"
	NeedsHumanLabel      = "needs-human"
	ProjectStatusReady   = "Ready"
	ProjectStatusTodo    = "Todo"
	ProjectInProgress    = "In Progress"
	ProjectStatusReview  = "Review"
	ProjectStatusBlocked = "Blocked"
	ProjectStatusDone    = "Done"
)

type GitHubIssueAdmission struct {
	Labels       []GitHubLabel       `json:"labels"`
	ProjectItems []GitHubProjectItem `json:"projectItems"`
}

type GitHubLabel struct {
	Name string `json:"name"`
}

type GitHubProjectItem struct {
	Status GitHubProjectStatus `json:"status"`
}

type GitHubProjectStatus struct {
	Name string `json:"name"`
}

func (issue GitHubIssueAdmission) HasLabel(name string) bool {
	want := strings.ToLower(strings.TrimSpace(name))
	for _, label := range issue.Labels {
		if strings.ToLower(strings.TrimSpace(label.Name)) == want {
			return true
		}
	}
	return false
}

func (issue GitHubIssueAdmission) HasNeedsAgent() bool {
	return issue.HasLabel(NeedsAgentLabel)
}

func (issue GitHubIssueAdmission) ProjectStatus() string {
	for _, item := range issue.ProjectItems {
		status := strings.TrimSpace(item.Status.Name)
		if status != "" {
			return status
		}
	}
	return ""
}

func (issue GitHubIssueAdmission) ProjectReady() bool {
	return strings.EqualFold(issue.ProjectStatus(), ProjectStatusReady)
}

func (issue GitHubIssueAdmission) Admissible() bool {
	return issue.HasNeedsAgent() && issue.ProjectReady()
}
