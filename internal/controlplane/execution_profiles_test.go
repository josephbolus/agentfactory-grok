package controlplane

import (
	"context"
	"testing"

	"github.com/josephbolus/agentfactory-grok/internal/protocol"
)

func TestCreateExecutionProfileRejectsCloudRun(t *testing.T) {
	store := newTestStore(t)
	_, err := store.CreateExecutionProfile(context.Background(), protocol.SaveExecutionProfileRequest{
		Name: "API cloud", Kind: "removed", Runtime: protocol.RuntimeCodex,
		Provider: "openrouter", Model: "test",
	})
	if !serviceErrorCode(err, "cloud_run_removed") {
		t.Fatalf("create profile = %v", err)
	}
}
