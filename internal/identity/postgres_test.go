package identity

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	postgresstore "github.com/router-for-me/CLIProxyAPI/v6/internal/storage/postgres"
)

func TestPostgresIdentityLifecycle(t *testing.T) {
	dsn := os.Getenv("CLIRELAY_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("CLIRELAY_POSTGRES_TEST_DSN is not set")
	}
	ctx := context.Background()
	db, err := postgresstore.OpenRuntimeDB(ctx, config.PostgresConfig{DSN: dsn, MaxOpenConns: 4, MaxIdleConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`TRUNCATE audit_logs,user_sessions,user_roles,role_permissions,users,roles,permissions,tenants CASCADE`); err != nil {
		t.Fatal(err)
	}
	service := NewService(db)
	if err = service.Bootstrap(ctx, "bootstrap-password-123"); err != nil {
		t.Fatal(err)
	}
	login, err := service.Login(ctx, "admin", "bootstrap-password-123", false, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !login.Principal.PlatformAdmin || login.Principal.HomeTenant.ID != SystemTenantID {
		t.Fatalf("principal=%+v", login.Principal)
	}
	principal, err := service.Authenticate(ctx, login.AccessToken, "")
	if err != nil {
		t.Fatal(err)
	}
	tenant, admin, err := service.CreateTenant(ctx, principal, CreateTenantInput{Slug: "tenant-a", Name: "Tenant A", ExpiresAt: time.Now().Add(time.Hour), AdminUsername: "tenant-admin", AdminDisplayName: "Tenant Admin", AdminPassword: "tenant-password-123"})
	if err != nil {
		t.Fatal(err)
	}
	if tenant.ID == "" || admin.TenantID != tenant.ID {
		t.Fatalf("tenant=%+v admin=%+v", tenant, admin)
	}
	tenantAdminLogin, err := service.Login(ctx, "tenant-admin", "tenant-password-123", false, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err = service.ChangePassword(ctx, tenantAdminLogin.Principal, "tenant-password-123", "tenant-password-456"); err != nil {
		t.Fatal(err)
	}
	tenantAdmin, err := service.Authenticate(ctx, tenantAdminLogin.AccessToken, "")
	if err != nil {
		t.Fatal(err)
	}
	roles, err := service.ListRoles(ctx, tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	var tenantAdminRoleID string
	for _, role := range roles {
		if role.Code == "tenant_admin" {
			tenantAdminRoleID = role.ID
		}
	}
	if tenantAdminRoleID == "" {
		t.Fatal("tenant admin role not found")
	}
	limitedRole, err := service.CreateRole(ctx, tenantAdmin, tenant.ID, "limited_user_manager", "Limited user manager", "", []string{
		"tenant.users.read",
		"tenant.users.create",
		"tenant.users.assign_roles",
	})
	if err != nil {
		t.Fatal(err)
	}
	limitedUser, err := service.CreateUser(ctx, tenantAdmin, tenant.ID, "limited-manager", "Limited Manager", "limited-password-123", []string{limitedRole.ID})
	if err != nil {
		t.Fatal(err)
	}
	limitedLogin, err := service.Login(ctx, "limited-manager", "limited-password-123", false, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err = service.ChangePassword(ctx, limitedLogin.Principal, "limited-password-123", "limited-password-456"); err != nil {
		t.Fatal(err)
	}
	limitedPrincipal, err := service.Authenticate(ctx, limitedLogin.AccessToken, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.CreateUser(ctx, limitedPrincipal, tenant.ID, "escalated-user", "Escalated User", "escalated-password-123", []string{tenantAdminRoleID}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("create user with non-delegable role err=%v", err)
	}
	rolelessUser, err := service.CreateUser(ctx, limitedPrincipal, tenant.ID, "roleless-user", "Roleless User", "roleless-password-123", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.AssignUserRoles(ctx, limitedPrincipal, tenant.ID, rolelessUser.ID, []string{tenantAdminRoleID}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("assign non-delegable role err=%v", err)
	}
	if err = service.DeleteUser(ctx, tenantAdmin, tenant.ID, limitedUser.ID); err != nil {
		t.Fatalf("delete audited user: %v", err)
	}
	past := time.Now().Add(-time.Minute)
	if _, err = service.UpdateTenant(ctx, principal, tenant.ID, "active", &past, tenant.Version); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Login(ctx, "tenant-admin", "tenant-password-456", false, "test"); !errors.Is(err, ErrTenantExpired) {
		t.Fatalf("expired login err=%v", err)
	}
}
