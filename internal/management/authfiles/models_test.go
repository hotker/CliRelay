package authfiles

import (
	"context"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

type modelSourceStub struct {
	clientID string
	models   []*registry.ModelInfo
}

func (s *modelSourceStub) GetModelsForClient(clientID string) []*registry.ModelInfo {
	s.clientID = clientID
	return s.models
}

func TestModelLookupAuthIDUsesMatchingAuthID(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	if _, err := manager.Register(context.Background(), &coreauth.Auth{
		ID:       "auth-id",
		FileName: "codex.json",
		Provider: "codex",
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if got := ModelLookupAuthID(manager, "codex.json"); got != "auth-id" {
		t.Fatalf("ModelLookupAuthID() = %q, want auth-id", got)
	}
	if got := ModelLookupAuthID(manager, "missing.json"); got != "missing.json" {
		t.Fatalf("ModelLookupAuthID() fallback = %q, want missing.json", got)
	}
}

func TestListModelEntriesBuildsPublicPayload(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	if _, err := manager.Register(context.Background(), &coreauth.Auth{
		ID:       "auth-id",
		FileName: "codex.json",
		Provider: "codex",
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	source := &modelSourceStub{
		models: []*registry.ModelInfo{
			{
				ID:          "gpt-test",
				DisplayName: "GPT Test",
				Type:        "codex",
				OwnedBy:     "openai",
			},
			nil,
			{ID: "minimal"},
		},
	}

	got := ListModelEntries(manager, source, "codex.json")
	if source.clientID != "auth-id" {
		t.Fatalf("source clientID = %q, want auth-id", source.clientID)
	}
	if len(got) != 2 {
		t.Fatalf("entries length = %d, want 2: %#v", len(got), got)
	}
	if got[0]["id"] != "gpt-test" || got[0]["display_name"] != "GPT Test" || got[0]["type"] != "codex" || got[0]["owned_by"] != "openai" {
		t.Fatalf("entry[0] = %#v, want full public payload", got[0])
	}
	if _, ok := got[1]["display_name"]; ok {
		t.Fatalf("minimal entry has display_name: %#v", got[1])
	}
	if got[1]["id"] != "minimal" {
		t.Fatalf("minimal id = %#v, want minimal", got[1]["id"])
	}
}

type modelRegistrarStub struct {
	calls int
	last  struct {
		clientID string
		provider string
		models   []*registry.ModelInfo
	}
}

func (s *modelRegistrarStub) RegisterClient(clientID, clientProvider string, models []*registry.ModelInfo) {
	s.calls++
	s.last.clientID = clientID
	s.last.provider = clientProvider
	s.last.models = models
}

func TestListModelEntriesLiveForTenantCodexDoesNotReplaceRegistry(t *testing.T) {
	// Without a real upstream, refresh falls back to registry; registrar must not be called
	// when live is empty. When we inject via a custom path, codex updateRegistry is false.
	// Here we only assert the empty-live path never registers.
	manager := coreauth.NewManager(nil, nil, nil)
	if _, err := manager.Register(context.Background(), &coreauth.Auth{
		ID:       "codex-auth",
		FileName: "codex.json",
		Provider: "codex",
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	source := &modelSourceStub{
		models: []*registry.ModelInfo{{ID: "gpt-5.5", DisplayName: "GPT-5.5", Type: "codex"}},
	}
	reg := &modelRegistrarStub{}
	models, label := ListModelEntriesLiveForTenant(
		context.Background(),
		manager,
		source,
		reg,
		nil,
		"",
		"codex.json",
		true,
	)
	if label != "registry" {
		t.Fatalf("source label = %q, want registry (no live without credentials)", label)
	}
	if reg.calls != 0 {
		t.Fatalf("RegisterClient calls = %d, want 0 for failed/empty codex live", reg.calls)
	}
	if len(models) != 1 || models[0]["id"] != "gpt-5.5" {
		t.Fatalf("fallback models = %#v", models)
	}
}

func TestListModelEntriesLiveWithoutRefreshUsesRegistry(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	if _, err := manager.Register(context.Background(), &coreauth.Auth{
		ID:       "codex-auth",
		FileName: "codex.json",
		Provider: "codex",
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	source := &modelSourceStub{
		models: []*registry.ModelInfo{{ID: "gpt-5.5"}},
	}
	reg := &modelRegistrarStub{}
	models, label := ListModelEntriesLiveForTenant(
		context.Background(), manager, source, reg, nil, "", "codex.json", false,
	)
	if label != "registry" || reg.calls != 0 || len(models) != 1 {
		t.Fatalf("label=%q calls=%d models=%#v", label, reg.calls, models)
	}
}
