package controlplane

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

func githubIssueVisitRequestKey(sourceKey string, visitNumber int) string {
	return "github-issue:" + sourceKey + ":v" + strconv.Itoa(visitNumber)
}

func (s *Store) openGitHubIssueVisit(
	ctx context.Context,
	repositoryID string,
	issue githubIssue,
	matchingKey, sourceKey string,
) (string, int, bool, error) {
	matchingKey = strings.TrimSpace(matchingKey)
	if matchingKey == "" {
		return "", 0, false, invalid("invalid_github_issue_visit", "visit matching key is required")
	}
	var visitNumber int
	var existingKey string
	var open int
	err := s.db.QueryRowContext(ctx, `
		SELECT visit_number, matching_key, open FROM github_issue_visits
		WHERE repository_id = ? AND issue_number = ?
	`, repositoryID, issue.Number).Scan(&visitNumber, &existingKey, &open)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", 0, false, unavailable(err)
	}
	if err == nil && open == 1 && existingKey == matchingKey {
		return githubIssueVisitRequestKey(sourceKey, visitNumber), visitNumber, false, nil
	}
	next := 1
	if err == nil {
		next = visitNumber + 1
	}
	requestKey := githubIssueVisitRequestKey(sourceKey, next)
	now := s.now().UnixMilli()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO github_issue_visits(
			repository_id, issue_number, visit_number, matching_key, request_key, work_id, open, opened_at, closed_at
		) VALUES (?, ?, ?, ?, ?, NULL, 1, ?, NULL)
		ON CONFLICT(repository_id, issue_number) DO UPDATE SET
			visit_number = excluded.visit_number,
			matching_key = excluded.matching_key,
			request_key = excluded.request_key,
			work_id = NULL,
			open = 1,
			opened_at = excluded.opened_at,
			closed_at = NULL
	`, repositoryID, issue.Number, next, matchingKey, requestKey, now)
	if err != nil {
		return "", 0, false, unavailable(err)
	}
	return requestKey, next, true, nil
}

func (s *Store) recordGitHubIssueVisitWork(ctx context.Context, repositoryID string, issueNumber int, workID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE github_issue_visits SET work_id = ? WHERE repository_id = ? AND issue_number = ? AND open = 1
	`, workID, repositoryID, issueNumber)
	if err != nil {
		return unavailable(err)
	}
	return nil
}

func (s *Store) closeUnseenGitHubIssueVisits(ctx context.Context, repositoryID string, seen map[int]struct{}) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT issue_number FROM github_issue_visits WHERE repository_id = ? AND open = 1
	`, repositoryID)
	if err != nil {
		return unavailable(err)
	}
	defer rows.Close()
	var closing []int
	for rows.Next() {
		var number int
		if err := rows.Scan(&number); err != nil {
			return unavailable(err)
		}
		if _, ok := seen[number]; !ok {
			closing = append(closing, number)
		}
	}
	if err := rows.Err(); err != nil {
		return unavailable(err)
	}
	now := s.now().UnixMilli()
	for _, number := range closing {
		if _, err := s.db.ExecContext(ctx, `
			UPDATE github_issue_visits SET open = 0, closed_at = ? WHERE repository_id = ? AND issue_number = ? AND open = 1
		`, now, repositoryID, number); err != nil {
			return unavailable(fmt.Errorf("close github issue visit: %v", err))
		}
	}
	return nil
}
