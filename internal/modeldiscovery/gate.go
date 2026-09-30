package modeldiscovery

import sdkrouting "github.com/router-for-me/CLIProxyAPI/v6/sdk/routing"

func init() {
	// The request gate lives in sdk/cliproxy/auth, which cannot import this
	// package. Publishing the lookup here keeps "the manifest listed it" and
	// "the root channel will serve it" on the same store.
	sdkrouting.SetDiscoveredModel(func(tenantID, modelID string) bool {
		return Listed(tenantID, "codex", modelID)
	})
}
