package branch_usecase

import (
	"errors"
	"net"
	"strings"
	"time"

	"vozko/domain/branch"
)

type handleRegisterUseCase struct {
	repo     branch.Repository
	store    branch.BindingStore
	presence branch.BranchPresenceListener
	realm    string
	now      func() time.Time
	nonces   *branch.NonceService
}

// NewHandleRegisterUseCase builds the REGISTER handler. realm is the fallback
// digest realm when a branch has none stored (branches store their own realm at
// secret time; the two must agree). presence (nil-safe) is notified when a branch
// comes online / goes offline so it is added to / removed from the routing
// registry, which is what makes a registered phone a transfer target. now is
// injected for testability. nonces issues/verifies the signed digest nonces (must
// be non-nil; a random-secret service is fine on a single VPS).
func NewHandleRegisterUseCase(repo branch.Repository, store branch.BindingStore, presence branch.BranchPresenceListener, realm string, now func() time.Time, nonces *branch.NonceService) branch.HandleRegisterUseCase {
	if strings.TrimSpace(realm) == "" {
		realm = defaultRealm
	}
	if now == nil {
		now = time.Now
	}
	if nonces == nil {
		nonces = branch.NewRandomNonceService(now)
	}
	return &handleRegisterUseCase{repo: repo, store: store, presence: presence, realm: realm, now: now, nonces: nonces}
}

func (uc *handleRegisterUseCase) Execute(req branch.RegisterRequest) (branch.RegisterResult, error) {
	su := strings.ToLower(strings.TrimSpace(req.SIPUser))
	sourceIP := hostOnly(req.ReceivedFrom)

	// Resolve the branch, but NEVER let the SIP status code reveal whether an
	// extension exists: an unknown, ambiguous, or disabled AOR must be
	// indistinguishable from a valid one to an unauthenticated prober (Asterisk
	// pjsip's identical-challenge behaviour, the successor to alwaysauthreject=yes).
	// A failed lookup therefore falls through to the SAME challenge / 401 dance as a
	// wrong password, under the default realm.
	b, authable, err := uc.lookupAuthable(su)
	if err != nil {
		return branch.RegisterResult{}, err
	}

	realm := uc.realm
	if authable && strings.TrimSpace(b.Realm) != "" {
		realm = b.Realm
	}

	// No Authorization yet: challenge (even for unknown users). The realm MUST be the
	// one HA1 was derived under so a legitimate phone computes a matching response.
	if !req.HasAuth {
		return branch.RegisterResult{Action: branch.RegisterChallenge, Realm: realm, Nonce: uc.nonces.Issue(sourceIP, realm)}, nil
	}

	// Verify the nonce is one WE issued, for THIS source, and still fresh BEFORE
	// checking the digest. This bounds replay to the nonce lifetime from the same IP
	// and rejects forged/relayed nonces outright.
	nonceOK, stale := uc.nonces.Verify(req.Nonce, sourceIP, realm)
	if !nonceOK {
		return branch.RegisterResult{Action: branch.RegisterChallenge, Realm: realm, Nonce: uc.nonces.Issue(sourceIP, realm), Stale: stale}, nil
	}

	// Unknown/disabled extension OR wrong password: both return an identical 401 with
	// a fresh nonce, so an attacker cannot tell a bad password from a missing
	// extension.
	if !authable || !branch.VerifyDigest(b.SecretHA1, req.Method, req.URI, req.Nonce, req.QOP, req.NC, req.CNonce, req.Response) {
		return branch.RegisterResult{Action: branch.RegisterUnauthorized, Realm: realm, Nonce: uc.nonces.Issue(sourceIP, realm)}, nil
	}

	granted, tooBrief := branch.ClampExpires(req.RequestedExpires)
	if tooBrief {
		return branch.RegisterResult{Action: branch.RegisterIntervalTooBrief, MinExpires: branch.MinExpiresSeconds}, nil
	}
	if granted == 0 {
		if err := uc.store.Remove(su, req.CallID); err != nil {
			return branch.RegisterResult{}, err
		}
		// If that was the branch's last live contact, it is no longer reachable: drop
		// it from the routing registry so transfers stop targeting a dead phone, and
		// reflect that on the branch row so the dashboard shows it offline.
		if n, _ := uc.store.CountLive(su); n == 0 {
			if uc.presence != nil {
				uc.presence.OnBranchUnreachable(b.ID)
			}
			if b.RegistrationStatus != branch.RegistrationStatusUnreachable {
				_ = uc.repo.UpdateRegistrationStatus(b.ID, branch.RegistrationStatusUnreachable)
			}
		}
		return branch.RegisterResult{Action: branch.RegisterDeregistered}, nil
	}

	// Cap concurrent contacts per AOR (Asterisk max_contacts) so one credential cannot
	// exhaust memory or fan an INVITE out to unbounded contacts. A refresh of an
	// already-bound contact (same call_id) never counts against the cap.
	maxContacts := b.MaxContacts
	if maxContacts <= 0 {
		maxContacts = branch.DefaultMaxContacts
	}
	if live, _ := uc.store.ListLive(su); len(live) >= maxContacts && !containsCallID(live, req.CallID) {
		return branch.RegisterResult{Action: branch.RegisterForbidden}, nil
	}

	now := uc.now()
	binding := branch.RegistrationBinding{
		SIPUser:      su,
		BranchID:     b.ID,
		WorkspaceID:  b.WorkspaceID,
		UserID:       b.UserID,
		Contact:      strings.TrimSpace(req.Contact),
		ReceivedFrom: strings.TrimSpace(req.ReceivedFrom),
		Transport:    req.Transport,
		CallID:       req.CallID,
		CSeq:         req.CSeq,
		UserAgent:    req.UserAgent,
		CreatedAt:    now,
		ExpiresAt:    now.Add(time.Duration(granted) * time.Second),
	}
	if err := uc.store.Upsert(binding); err != nil {
		return branch.RegisterResult{}, err
	}
	// The branch is reachable: register it as a routing/transfer target. Idempotent,
	// so a re-REGISTER refresh is a no-op on an already-online branch.
	if uc.presence != nil {
		uc.presence.OnBranchReachable(branch.RegisteredBranch{
			BranchID: b.ID, SIPUser: su, WorkspaceID: b.WorkspaceID, UserID: b.UserID,
		})
	}
	// Reflect "online" on the branch row (stamping last_registered_at) so the
	// dashboard shows reality. Only write when it actually changed, so a 300s
	// re-REGISTER refresh does not hammer the DB.
	if b.RegistrationStatus != branch.RegistrationStatusRegistered {
		_ = uc.repo.UpdateRegistrationStatus(b.ID, branch.RegistrationStatusRegistered)
	}
	return branch.RegisterResult{Action: branch.RegisterOK, GrantedExpires: granted}, nil
}

// lookupAuthable resolves the branch for su and reports whether it can be
// authenticated (found, unambiguous, and enabled). A not-found or ambiguous AOR is
// NOT an error here (authable=false, no leak); only an unexpected repository error
// propagates. The caller treats authable=false exactly like a wrong password, so the
// SIP status code never reveals whether the extension exists.
func (uc *handleRegisterUseCase) lookupAuthable(su string) (*branch.Branch, bool, error) {
	if su == "" {
		return nil, false, nil
	}
	b, err := uc.repo.FindByGlobalSIPUser(su)
	if err != nil {
		if errors.Is(err, branch.ErrBranchNotFound) || errors.Is(err, branch.ErrBranchSIPUserTaken) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if b == nil || !b.Enabled {
		return b, false, nil
	}
	return b, true, nil
}

// hostOnly returns the IP/host part of an "ip:port" source, used to bind a nonce to
// the requester's address. Falls back to the raw value if it has no port.
func hostOnly(ipPort string) string {
	s := strings.TrimSpace(ipPort)
	if s == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(s); err == nil {
		return host
	}
	return s
}

func containsCallID(bindings []branch.RegistrationBinding, callID string) bool {
	for _, b := range bindings {
		if b.CallID == callID {
			return true
		}
	}
	return false
}
