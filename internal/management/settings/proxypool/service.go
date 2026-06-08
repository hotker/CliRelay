package proxypool

import (
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

func StoreAvailable() bool {
	return usage.ProxyPoolStoreAvailable()
}

func List() []config.ProxyPoolEntry {
	return usage.ListProxyPool()
}

func Get(id string) *config.ProxyPoolEntry {
	return usage.GetProxyPoolEntry(id)
}

func Replace(entries []config.ProxyPoolEntry) error {
	return usage.ReplaceProxyPool(entries)
}
