package workspace_repository

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/domain/workspace"
	"vozko/infra/database/schema"
)

func acceptInviteIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("VOZKO_TEST_DB") != "1" {
		t.Skip("set VOZKO_TEST_DB=1 (and DB_* vars) to run against Postgres")
	}
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		os.Getenv("DB_HOST"), os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"), os.Getenv("DB_PORT"))
	silent := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	admin, err := gorm.Open(postgres.Open(dsn), silent)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	schemaName := "invite_test_" + uuid.New().String()[:8]
	if err := admin.Exec("CREATE SCHEMA " + schemaName).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schemaName), silent)
	if err != nil {
		t.Fatalf("open schema: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		_ = admin.Exec("DROP SCHEMA " + schemaName + " CASCADE").Error
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&schema.User{}, &schema.Workspace{}, &schema.WorkspaceCustomRole{}, &schema.WorkspaceMember{},
		&schema.WorkspaceMemberPermission{}, &schema.WorkspaceInvite{}, &schema.WorkspaceDepartment{}, &schema.WorkspaceDepartmentMember{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

type acceptInviteFixture struct {
	db         *gorm.DB
	repo       workspace.Repository
	ws         string
	invitee    string
	inviteID   string
	sales      string
	support    string
	deletedDep string
}

func newAcceptInviteFixture(t *testing.T) acceptInviteFixture {
	t.Helper()
	db := acceptInviteIntegrationDB(t)
	user := func() string {
		u := schema.User{ID: uuid.New().String(), Username: "u-" + uuid.New().String()[:8], Email: uuid.New().String()[:8] + "@example.com", Password: "x"}
		if err := db.Create(&u).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
		return u.ID
	}
	owner, invitee := user(), user()
	ws := schema.Workspace{ID: uuid.New().String(), OwnerID: owner, Name: "Acme"}
	if err := db.Create(&ws).Error; err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	department := func(name string) string {
		d := schema.WorkspaceDepartment{ID: uuid.New().String(), WorkspaceID: ws.ID, Name: name}
		if err := db.Create(&d).Error; err != nil {
			t.Fatalf("seed department: %v", err)
		}
		return d.ID
	}
	f := acceptInviteFixture{db: db, repo: NewRepository(db), ws: ws.ID, invitee: invitee,
		sales: department("Vendas"), support: department("Suporte"), deletedDep: department("Antigo")}
	if err := db.Delete(&schema.WorkspaceDepartment{}, "id = ?", f.deletedDep).Error; err != nil {
		t.Fatalf("delete department: %v", err)
	}
	invite := &workspace.Invite{ID: uuid.New().String(), WorkspaceID: ws.ID, InviterID: owner, Email: "new@example.com",
		Role: workspace.RoleMember, Status: workspace.InviteStatusPending, Token: uuid.New().String(),
		DepartmentIDs: []string{f.sales, f.support, f.deletedDep}, ExpiresAt: time.Now().Add(time.Hour)}
	if err := f.repo.CreateInvite(invite); err != nil {
		t.Fatalf("seed invite: %v", err)
	}
	f.inviteID = invite.ID
	return f
}

func (f acceptInviteFixture) member() *workspace.Member {
	return &workspace.Member{ID: uuid.New().String(), WorkspaceID: f.ws, UserID: f.invitee, Role: workspace.RoleMember}
}

func TestAcceptInviteIntegration_JoinsTheInvitedDepartmentsAtOnce(t *testing.T) {
	f := newAcceptInviteFixture(t)
	member := f.member()
	perms := []*workspace.Permission{{ID: uuid.New().String(), MemberID: member.ID, Resource: workspace.ResourceConversations, Action: workspace.ActionRead}}

	if err := f.repo.AcceptInvite(f.inviteID, member, perms, []string{f.sales, f.support, f.deletedDep}); err != nil {
		t.Fatalf("accept: %v", err)
	}

	var departments []string
	f.db.Model(&schema.WorkspaceDepartmentMember{}).Where("member_id = ?", member.ID).Order("department_id").Pluck("department_id", &departments)
	want := []string{f.sales, f.support}
	if want[0] > want[1] {
		want[0], want[1] = want[1], want[0]
	}
	if len(departments) != 2 || departments[0] != want[0] || departments[1] != want[1] {
		t.Fatalf("departments %v, want the two live ones %v", departments, want)
	}
	var permissions int64
	f.db.Model(&schema.WorkspaceMemberPermission{}).Where("member_id = ?", member.ID).Count(&permissions)
	invite, _ := f.repo.GetInviteByID(f.inviteID)
	if permissions != 1 || invite.Status != workspace.InviteStatusAccepted {
		t.Fatalf("permissions %d, invite %s; want 1 and accepted", permissions, invite.Status)
	}
}

func TestAcceptInviteIntegration_ASecondAcceptChangesNothing(t *testing.T) {
	f := newAcceptInviteFixture(t)
	if err := f.repo.AcceptInvite(f.inviteID, f.member(), nil, []string{f.sales}); err != nil {
		t.Fatalf("first accept: %v", err)
	}

	if err := f.repo.AcceptInvite(f.inviteID, f.member(), nil, []string{f.sales}); !errors.Is(err, workspace.ErrInviteAlreadyProcessed) {
		t.Fatalf("second accept: want ErrInviteAlreadyProcessed, got %v", err)
	}
	var members int64
	f.db.Model(&schema.WorkspaceMember{}).Where("workspace_id = ?", f.ws).Count(&members)
	if members != 1 {
		t.Fatalf("want one member, got %d", members)
	}
}

func TestAcceptInviteIntegration_AFailureLeavesTheInvitePending(t *testing.T) {
	f := newAcceptInviteFixture(t)
	member := f.member()
	broken := []*workspace.Permission{{ID: "not-a-uuid", MemberID: member.ID, Resource: workspace.ResourceConversations, Action: workspace.ActionRead}}

	if err := f.repo.AcceptInvite(f.inviteID, member, broken, []string{f.sales}); err == nil {
		t.Fatal("a failing write must fail the accept")
	}
	var members, links int64
	f.db.Model(&schema.WorkspaceMember{}).Where("workspace_id = ?", f.ws).Count(&members)
	f.db.Model(&schema.WorkspaceDepartmentMember{}).Count(&links)
	invite, _ := f.repo.GetInviteByID(f.inviteID)
	if members != 0 || links != 0 || invite.Status != workspace.InviteStatusPending {
		t.Fatalf("members %d, links %d, invite %s; want nothing written and the invite still pending", members, links, invite.Status)
	}
}
