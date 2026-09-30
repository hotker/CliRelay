package auth

import (
	"testing"

	sdkrouting "github.com/router-for-me/CLIProxyAPI/v6/sdk/routing"
)

func TestDefaultGroupFollowsModelsTheUpstreamAddsLater(t *testing.T) {
	const tenant = "9e003dfb-751f-4898-b186-45f765c763a6"
	sdkrouting.SetDiscoveredModel(func(tenantID, modelID string) bool {
		return tenantID == tenant && modelID == "gpt-6.1-sol"
	})
	t.Cleanup(func() { sdkrouting.SetDiscoveredModel(nil) })

	if !routingGroupModelAllowed("default", []string{"gpt-5.5"}, nil, "gpt-6.1-sol", tenant) {
		t.Fatal("default group hid a model the upstream manifest listed")
	}
	if routingGroupModelAllowed("default", []string{"gpt-5.5"}, []string{"gpt-6.1-sol"}, "gpt-6.1-sol", tenant) {
		t.Fatal("an exclusion lost to discovery")
	}
	if routingGroupModelAllowed("deepseekv4flash+chatgpt", []string{"deepseek-v4-flash"}, nil, "gpt-6.1-sol", tenant) {
		t.Fatal("a curated group inherited a discovered model")
	}
	if routingGroupModelAllowed("default", []string{"gpt-5.5"}, nil, "not-on-the-manifest", tenant) {
		t.Fatal("the allow list stopped applying to models discovery did not list")
	}
}
