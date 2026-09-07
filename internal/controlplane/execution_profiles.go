package controlplane

import (
	"context"
	"database/sql"
	"errors"

	"github.com/josephbolus/agentfactory-grok/internal/protocol"
)

func syntheticWorkerID(profileID string) string { return "removed-profile-" + profileID }

func (s *Store) CreateExecutionProfile(ctx context.Context, input protocol.SaveExecutionProfileRequest) (protocol.ExecutionProfile, error) {
	return protocol.ExecutionProfile{}, invalid("cloud_run_removed", "Cloud Run execution profiles were removed")
}

func (s *Store) UpdateExecutionProfile(ctx context.Context, id string, input protocol.SaveExecutionProfileRequest) (protocol.ExecutionProfile, error) {
	return protocol.ExecutionProfile{}, invalid("cloud_run_removed", "Cloud Run execution profiles were removed")
}

func persistentAutoProfile() protocol.ExecutionProfile {
	return protocol.ExecutionProfile{
		ID: protocol.PersistentAutoProfileID, Name: "Persistent auto", Kind: protocol.BackendPersistent,
		Version: 1, Provider: "worker", Model: "worker-default", ResourceClass: "worker",
		MaxConcurrent: protocol.MaxWorkerCapacity, Enabled: true, Healthy: true,
	}
}

func (s *Store) ExecutionProfiles(ctx context.Context) (protocol.ExecutionProfilePage, error) {
	page := protocol.ExecutionProfilePage{Profiles: []protocol.ExecutionProfile{persistentAutoProfile()}}
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.name, v.kind, p.current_version, v.runtime, v.provider, v.model,
		       v.timeout_seconds, v.resource_class, v.max_concurrent, p.enabled, p.healthy,
		       p.health_reason, v.fake_outcome, v.fake_result, v.fake_error, p.created_at, p.updated_at
		FROM execution_profiles p
		JOIN execution_profile_versions v ON v.profile_id = p.id AND v.version = p.current_version
		ORDER BY p.name_key, p.id
	`)
	if err != nil {
		return page, unavailable(err)
	}
	defer rows.Close()
	for rows.Next() {
		profile, err := scanExecutionProfile(rows)
		if err != nil {
			return page, unavailable(err)
		}
		page.Profiles = append(page.Profiles, profile)
	}
	return page, rows.Err()
}

func (s *Store) ExecutionProfile(ctx context.Context, id string) (protocol.ExecutionProfile, error) {
	if id == "" || id == protocol.PersistentAutoProfileID {
		return persistentAutoProfile(), nil
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT p.id, p.name, v.kind, p.current_version, v.runtime, v.provider, v.model,
		       v.timeout_seconds, v.resource_class, v.max_concurrent, p.enabled, p.healthy,
		       p.health_reason, v.fake_outcome, v.fake_result, v.fake_error, p.created_at, p.updated_at
		FROM execution_profiles p
		JOIN execution_profile_versions v ON v.profile_id = p.id AND v.version = p.current_version
		WHERE p.id = ?
	`, id)
	profile, err := scanExecutionProfile(row)
	if errors.Is(err, sql.ErrNoRows) {
		return profile, ErrNotFound
	}
	if err != nil {
		return profile, unavailable(err)
	}
	return profile, nil
}

func scanExecutionProfile(row scanner) (protocol.ExecutionProfile, error) {
	var profile protocol.ExecutionProfile
	var enabled, healthy int
	var created, updated int64
	err := row.Scan(&profile.ID, &profile.Name, &profile.Kind, &profile.Version, &profile.Runtime,
		&profile.Provider, &profile.Model, &profile.TimeoutSeconds, &profile.ResourceClass,
		&profile.MaxConcurrent, &enabled, &healthy, &profile.HealthReason, &profile.FakeOutcome,
		&profile.FakeResult, &profile.FakeError, &created, &updated)
	profile.Enabled, profile.Healthy = enabled != 0, healthy != 0
	profile.SyntheticWorkerID = syntheticWorkerID(profile.ID)
	profile.CreatedAt, profile.UpdatedAt = fromMillis(created), fromMillis(updated)
	return profile, err
}
