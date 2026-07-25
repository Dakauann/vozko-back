// Package branch_usecase implements the branch (branch) control-plane use cases.
// Phase 0: pure CRUD + per-workspace cap + SIP secret provisioning. No SIP
// signaling yet. See docs/BRANCH_RAMAL_TRANSFER_PLAN.md.
package branch_usecase

import (
	"errors"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/branch"
)

const defaultRealm = "vozko"

type createBranchUseCase struct {
	repo    branch.Repository
	members branch.MemberDirectory
	gate    branch.ProvisioningGate
	realm   string
}

// NewCreateUseCase builds the create use case. realm is the fixed SIP digest
// realm HA1 is derived under (kept stable so saved phone credentials survive);
// gate enforces the per-workspace branch cap from the plan entitlement
// (PlanDefinition.MaxBranches plus addons), the same plan-driven model
// MaxCallChannels uses. Admins bypass the gate.
func NewCreateUseCase(repo branch.Repository, members branch.MemberDirectory, gate branch.ProvisioningGate, realm string) branch.CreateUseCase {
	if strings.TrimSpace(realm) == "" {
		realm = defaultRealm
	}
	return &createBranchUseCase{repo: repo, members: members, gate: gate, realm: realm}
}

func (uc *createBranchUseCase) Execute(input branch.CreateBranchInput) (*branch.SecretResult, error) {
	memberID, err := uc.members.ResolveMember(input.WorkspaceID, strings.TrimSpace(input.TargetUserID))
	if err != nil {
		return nil, err
	}

	b := branch.NewBranch(uuid.New().String(), input.WorkspaceID, memberID, strings.TrimSpace(input.TargetUserID), input.SIPUser, input.DisplayName)
	if len(input.Codecs) > 0 {
		b.Codecs = input.Codecs
	}
	if input.MaxContacts > 0 {
		b.MaxContacts = input.MaxContacts
	}
	b.DND = input.DND

	if err := b.Validate(); err != nil {
		return nil, err
	}

	// Friendly uniqueness pre-check; the DB unique index is the hard guarantee and
	// is translated below in case of a race. sip_user is GLOBALLY unique so the
	// registrar can resolve a bare REGISTER to one branch.
	if existing, err := uc.repo.FindByGlobalSIPUser(b.SIPUser); err == nil && existing != nil {
		return nil, branch.ErrBranchSIPUserTaken
	} else if err != nil && !errors.Is(err, branch.ErrBranchNotFound) {
		return nil, err
	}

	// Plan-driven cap (admins bypass): the workspace's PlanDefinition.MaxBranches
	// plus active addons, resolved exactly like MaxCallChannels.
	if !input.Actor.IsAdmin && uc.gate != nil {
		ok, err := uc.gate.CanCreateBranch(b.WorkspaceID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, branch.ErrBranchLimitReached
		}
	}

	secret, err := branch.GenerateSecret()
	if err != nil {
		return nil, err
	}
	b.SetSecret(uc.realm, secret)

	if err := uc.repo.Create(b); err != nil {
		return nil, translateWriteErr(err)
	}

	return &branch.SecretResult{Branch: b, Secret: secret, Realm: b.Realm, SIPUser: b.SIPUser}, nil
}

// translateWriteErr maps a Postgres unique-violation on the (workspace_id,
// sip_user) index to the friendly taken error, so a create race surfaces the
// same way as the pre-check.
func translateWriteErr(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "ux_branches_sip_user") ||
		(strings.Contains(msg, "duplicate key") && strings.Contains(msg, "sip_user")) {
		return branch.ErrBranchSIPUserTaken
	}
	return err
}
