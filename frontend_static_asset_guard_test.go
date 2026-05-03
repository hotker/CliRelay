package main

import (
	"os"
	"testing"
)

func TestRepositoryDoesNotVendorManagementPanelBuildOutput(t *testing.T) {
	for _, path := range []string{
		"assets",
		"manage.html",
		"management.html",
	} {
		if _, err := os.Stat(path); err == nil {
			t.Fatalf("backend repository must not vendor frontend panel build output: %s", path)
		} else if !os.IsNotExist(err) {
			t.Fatalf("stat %s: %v", path, err)
		}
	}
}
