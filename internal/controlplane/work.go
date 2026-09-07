package controlplane

import (
	"context"
	"database/sql"
	"errors"

	"github.com/josephbolus/agentfactory-grok/internal/protocol"
)

const (
	defaultWorkUpdatePageSize = 100
	maxWorkUpdatePageSize     = 200
)

func (s *Store) AppendAgentUpdate(
	ctx context.Context,
	attemptID string,
	input protocol.AttemptUpdateRequest,
) (protocol.WorkUpdate, error) {
	return protocol.WorkUpdate{}, conflict(
		"agent_update_removed",
		"agent_update was removed; continue via Project status and labels",
	)
}

func validCommitSHA(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func attemptOutcome(ctx context.Context, tx *sql.Tx, attemptID string) (protocol.WorkUpdate, bool, error) {
	var update protocol.WorkUpdate
	var acceptedAt int64
	err := tx.QueryRowContext(ctx, `
		SELECT id, work_id, COALESCE(attempt_id, ''), request_id, sequence, status, message,
		       pull_request_url, pull_request_head_branch, pull_request_head_sha,
		       checkpoint_sha, checkpoint_published, actor, accepted_at
		FROM work_updates WHERE attempt_id = ? AND status != 'running'
	`, attemptID).Scan(&update.ID, &update.WorkID, &update.AttemptID, &update.RequestID,
		&update.Sequence, &update.Status, &update.Message, &update.PullRequestURL,
		&update.PullRequestHeadBranch, &update.PullRequestHeadSHA, &update.CheckpointSHA,
		&update.CheckpointPublished, &update.Actor, &acceptedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return protocol.WorkUpdate{}, false, nil
	}
	if err != nil {
		return protocol.WorkUpdate{}, false, unavailable(err)
	}
	update.AcceptedAt = fromMillis(acceptedAt)
	return update, true, nil
}

// Work returns the product-facing Work record backed by the existing Session
// row. Keeping the adapter here lets existing Run and Session clients remain
// compatible while later operator surfaces adopt Work directly.
func (s *Store) Work(ctx context.Context, id string) (protocol.Work, error) {
	var runID string
	err := s.db.QueryRowContext(ctx, `SELECT run_id FROM sessions WHERE id = ?`, id).Scan(&runID)
	if errors.Is(err, sql.ErrNoRows) {
		return protocol.Work{}, ErrNotFound
	}
	if err != nil {
		return protocol.Work{}, unavailable(err)
	}
	detail, err := s.Run(ctx, runID)
	if err != nil {
		return protocol.Work{}, err
	}
	for _, work := range detail.Sessions {
		if work.ID == id {
			work.Stages = nil
			return work, nil
		}
	}
	return protocol.Work{}, ErrNotFound
}

func (s *Store) WorkUpdates(
	ctx context.Context,
	workID string,
	limit int,
	after int,
) (protocol.WorkUpdatePage, error) {
	if limit == 0 {
		limit = defaultWorkUpdatePageSize
	}
	if limit < 1 || limit > maxWorkUpdatePageSize {
		return protocol.WorkUpdatePage{}, invalid("invalid_limit", "limit must be between 1 and 200")
	}
	if after < 0 {
		return protocol.WorkUpdatePage{}, invalid("invalid_cursor", "after must not be negative")
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id = ?`, workID).Scan(&exists); err != nil {
		return protocol.WorkUpdatePage{}, unavailable(err)
	}
	if exists == 0 {
		return protocol.WorkUpdatePage{}, ErrNotFound
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, work_id, COALESCE(attempt_id, ''), request_id, sequence, status, message,
		       pull_request_url, pull_request_head_branch, pull_request_head_sha,
		       checkpoint_sha, checkpoint_published, actor, accepted_at
		FROM work_updates
		WHERE work_id = ? AND sequence > ?
		ORDER BY sequence
		LIMIT ?
	`, workID, after, limit+1)
	if err != nil {
		return protocol.WorkUpdatePage{}, unavailable(err)
	}
	defer rows.Close()
	updates := make([]protocol.WorkUpdate, 0, limit+1)
	for rows.Next() {
		var update protocol.WorkUpdate
		var acceptedAt int64
		if err := rows.Scan(&update.ID, &update.WorkID, &update.AttemptID, &update.RequestID,
			&update.Sequence, &update.Status, &update.Message, &update.PullRequestURL,
			&update.PullRequestHeadBranch, &update.PullRequestHeadSHA, &update.CheckpointSHA,
			&update.CheckpointPublished, &update.Actor, &acceptedAt); err != nil {
			return protocol.WorkUpdatePage{}, unavailable(err)
		}
		update.AcceptedAt = fromMillis(acceptedAt)
		updates = append(updates, update)
	}
	if err := rows.Err(); err != nil {
		return protocol.WorkUpdatePage{}, unavailable(err)
	}
	page := protocol.WorkUpdatePage{Updates: updates, NextAfter: after}
	if len(page.Updates) > limit {
		page.Updates = page.Updates[:limit]
		page.HasMore = true
	}
	if len(page.Updates) != 0 {
		page.NextAfter = page.Updates[len(page.Updates)-1].Sequence
	}
	return page, nil
}

func validateWorkRetryGuards(
	ctx context.Context,
	tx *sql.Tx,
	workID, repositoryID, targetKind, sourceKind, sourceKey string,
) error {
	var replacementCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sessions WHERE predecessor_work_id = ?
	`, workID).Scan(&replacementCount); err != nil {
		return unavailable(err)
	}
	if replacementCount != 0 {
		return conflict("work_replaced", "replaced Work cannot be retried")
	}
	var matchingNonterminal int
	var err error
	if targetKind == "work_item" {
		err = tx.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM sessions
			WHERE id != ? AND repository_id = ? AND source_kind = ? AND source_key = ?
			  AND state IN ('blocked', 'queued', 'preparing', 'running', 'needs-input')
		`, workID, repositoryID, sourceKind, sourceKey).Scan(&matchingNonterminal)
	} else {
		err = tx.QueryRowContext(ctx, `
			WITH RECURSIVE lineage(work_id, root_id) AS (
				SELECT id, id FROM sessions WHERE predecessor_work_id IS NULL
				UNION ALL
				SELECT child.id, lineage.root_id
				FROM sessions child
				JOIN lineage ON child.predecessor_work_id = lineage.work_id
			)
			SELECT COUNT(*)
			FROM sessions candidate
			JOIN lineage candidate_lineage ON candidate_lineage.work_id = candidate.id
			JOIN lineage retried_lineage ON retried_lineage.work_id = ?
			WHERE candidate.id != ?
			  AND candidate.repository_id = ?
			  AND candidate_lineage.root_id = retried_lineage.root_id
			  AND candidate.state IN ('blocked', 'queued', 'preparing', 'running', 'needs-input')
		`, workID, workID, repositoryID).Scan(&matchingNonterminal)
	}
	if err != nil {
		return unavailable(err)
	}
	if matchingNonterminal != 0 {
		return conflict("matching_work_active", "matching nonterminal Work already exists")
	}
	return nil
}
