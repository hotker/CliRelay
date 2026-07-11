package identity

import (
	"errors"
	"testing"
	"time"
)

func TestNormalizeUsername(t *testing.T) {
	if got := NormalizeUsername("  Admin.User  "); got != "admin.user" {
		t.Fatalf("NormalizeUsername() = %q", got)
	}
}

func TestHashPasswordPolicyAndVerification(t *testing.T) {
	if _, err := HashPassword("too-short"); err == nil {
		t.Fatal("expected short password to be rejected")
	}
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil || hash == "" {
		t.Fatalf("HashPassword() hash=%q err=%v", hash, err)
	}
}

func TestValidateTenant(t *testing.T) {
	now := time.Now().UTC()
	active := Tenant{Type: "standard", Status: "active", ExpiresAt: ptrTime(now.Add(time.Hour))}
	if err := validateTenant(active, now); err != nil {
		t.Fatalf("active tenant rejected: %v", err)
	}
	expired := active
	expired.ExpiresAt = ptrTime(now)
	if !errors.Is(validateTenant(expired, now), ErrTenantExpired) {
		t.Fatal("expected expired tenant")
	}
	suspended := active
	suspended.Status = "suspended"
	if !errors.Is(validateTenant(suspended, now), ErrTenantSuspended) {
		t.Fatal("expected suspended tenant")
	}
	if err := validateTenant(Tenant{Type: "system", Status: "active"}, now); err != nil {
		t.Fatalf("system tenant rejected: %v", err)
	}
}

func TestRandomTokenHashesOnlyStableToken(t *testing.T) {
	token, hash, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || hash == "" || token == hash {
		t.Fatalf("token/hash invalid token=%q hash=%q", token, hash)
	}
	if got := tokenHash(token); got != hash {
		t.Fatalf("tokenHash() = %q, want %q", got, hash)
	}
}

func TestEnsureActorTenantScope(t *testing.T) {
	actor := Principal{EffectiveTenant: Tenant{ID: "tenant-a"}}
	if err := ensureActorTenantScope(actor, "tenant-a"); err != nil {
		t.Fatalf("current tenant rejected: %v", err)
	}
	if err := ensureActorTenantScope(actor, "tenant-b"); !errors.Is(err, ErrTenantScope) {
		t.Fatalf("cross-tenant scope error = %v", err)
	}
	actor.PlatformAdmin = true
	if err := ensureActorTenantScope(actor, "tenant-b"); err != nil {
		t.Fatalf("platform admin tenant scope rejected: %v", err)
	}
}

func ptrTime(value time.Time) *time.Time { return &value }
