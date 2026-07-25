// Package branch models an internal SIP extension (a "branch") that a workspace
// member registers a real SIP client (softphone such as MicroSIP, or a fixed IP
// desk phone) to. A branch is the inverse of a sip_trunk: a trunk is Vozko
// acting as a client to a carrier, a branch is a device acting as a client to
// Vozko. See docs/BRANCH_RAMAL_TRANSFER_PLAN.md.
//
// This package is the Phase 0 control plane: the entity, its validation, the
// repository port and the use-case ports. It carries no infra imports and knows
// nothing about SIP signaling (that arrives in Phase 1 with the registrar).
package branch

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"
)

// RegistrationStatus tracks whether a branch currently has a live SIP binding.
// In Phase 0 it is always NeverRegistered; the Phase 1 registrar drives it.
type RegistrationStatus string

const (
	RegistrationStatusNeverRegistered RegistrationStatus = "never_registered"
	RegistrationStatusRegistered      RegistrationStatus = "registered"
	RegistrationStatusExpired         RegistrationStatus = "expired"
	RegistrationStatusUnreachable     RegistrationStatus = "unreachable"
)

func (s RegistrationStatus) IsValid() bool {
	switch s {
	case RegistrationStatusNeverRegistered, RegistrationStatusRegistered,
		RegistrationStatusExpired, RegistrationStatusUnreachable:
		return true
	default:
		return false
	}
}

// CodecID is the narrow set of audio codecs a branch leg may negotiate. Kept
// local to this package (rather than importing sip_trunk) so the branch domain
// stays self-contained. G.711 only for PSTN-facing narrowband (see the codec
// memo); telephone-event is DTMF (RFC 4733), always appended, never a voice
// codec chosen on its own.
type CodecID string

const (
	CodecPCMA           CodecID = "pcma"
	CodecPCMU           CodecID = "pcmu"
	CodecTelephoneEvent CodecID = "telephone-event"
)

func (c CodecID) IsValid() bool {
	switch c {
	case CodecPCMA, CodecPCMU, CodecTelephoneEvent:
		return true
	default:
		return false
	}
}

// Branch is a per-member SIP extension owned by a workspace.
//
// SecretHA1 is MD5(sip_user:realm:password) hex, the digest credential a SIP
// registrar verifies against; the plaintext password is shown to the operator
// exactly once at create/rotate time and is never persisted. Realm is stored
// alongside so a password rotation can recompute HA1 without changing the realm
// (changing the realm would invalidate every saved phone credential).
type Branch struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	// MemberID is the workspace membership this branch belongs to (the natural
	// RBAC anchor; the branch is cascade-removed with the member). UserID is
	// denormalized so runtime routing can key on it like a dialer session does.
	MemberID string `json:"memberId"`
	UserID   string `json:"userId"`

	SIPUser     string `json:"sipUser"`
	DisplayName string `json:"displayName,omitempty"`

	Realm     string `json:"realm"`
	SecretHA1 string `json:"-"`

	Codecs      []CodecID `json:"codecs,omitempty"`
	MaxContacts int       `json:"maxContacts"`
	DND         bool      `json:"dnd"`

	Enabled            bool               `json:"enabled"`
	RegistrationStatus RegistrationStatus `json:"registrationStatus"`
	LastRegisteredAt   *time.Time         `json:"lastRegisteredAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

const (
	DefaultMaxContacts = 2
	maxContactsCap     = 8

	maxSIPUserLen     = 64
	maxDisplayNameLen = 120
	maxRealmLen       = 128
	maxCodecsList     = 8

	// secretBytes is the entropy of a generated SIP password before hex encoding
	// (18 bytes -> 36 hex chars), comfortably strong for digest auth.
	secretBytes = 18
)

// Actor identifies who performs a mutating operation on a branch. It is set
// server-side by the delivery layer (never decoded from client JSON) and drives
// the authorization invariant at the use-case boundary, mirroring sip_trunk.Actor.
type Actor struct {
	WorkspaceID string
	IsAdmin     bool
}

func (b *Branch) IsOwnedBy(workspaceID string) bool {
	if workspaceID == "" || b.WorkspaceID == "" {
		return false
	}
	return b.WorkspaceID == workspaceID
}

// CanBeModifiedBy reports whether the actor may mutate this branch. Platform
// admins may modify any branch; a workspace actor may modify only a branch its
// own workspace owns.
func (b *Branch) CanBeModifiedBy(a Actor) error {
	if a.IsAdmin {
		return nil
	}
	if !b.IsOwnedBy(a.WorkspaceID) {
		return ErrBranchForbidden
	}
	return nil
}

func NewBranch(id, workspaceID, memberID, userID, sipUser, displayName string) *Branch {
	now := time.Now()
	return &Branch{
		ID:                 id,
		WorkspaceID:        workspaceID,
		MemberID:           memberID,
		UserID:             userID,
		SIPUser:            sipUser,
		DisplayName:        displayName,
		MaxContacts:        DefaultMaxContacts,
		Enabled:            true,
		RegistrationStatus: RegistrationStatusNeverRegistered,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

func (b *Branch) Normalize() {
	b.ID = strings.TrimSpace(b.ID)
	b.WorkspaceID = strings.TrimSpace(b.WorkspaceID)
	b.MemberID = strings.TrimSpace(b.MemberID)
	b.UserID = strings.TrimSpace(b.UserID)
	b.SIPUser = strings.TrimSpace(strings.ToLower(b.SIPUser))
	b.DisplayName = strings.TrimSpace(b.DisplayName)
	b.Realm = strings.TrimSpace(strings.ToLower(b.Realm))

	if b.MaxContacts <= 0 {
		b.MaxContacts = DefaultMaxContacts
	}
	if b.RegistrationStatus == "" {
		b.RegistrationStatus = RegistrationStatusNeverRegistered
	}

	if len(b.Codecs) > 0 {
		out := make([]CodecID, 0, len(b.Codecs))
		seen := make(map[CodecID]struct{}, len(b.Codecs))
		for _, c := range b.Codecs {
			v := CodecID(strings.ToLower(strings.TrimSpace(string(c))))
			if v == "" {
				continue
			}
			if _, dup := seen[v]; dup {
				continue
			}
			seen[v] = struct{}{}
			out = append(out, v)
		}
		b.Codecs = out
	}
}

func containsCtrlChar(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == 0x7f {
			return true
		}
	}
	return false
}

// validSIPUser accepts the characters a SIP AOR user part may safely hold and
// that a phone can type: letters, digits and a small punctuation set. Kept
// conservative to avoid ambiguity in a From/To user and dialplan collisions.
func validSIPUser(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '.' || c == '-' || c == '_':
		default:
			return false
		}
	}
	return true
}

func (b *Branch) Validate() error {
	b.Normalize()
	if b.WorkspaceID == "" {
		return ErrBranchWorkspaceRequired
	}
	if b.MemberID == "" || b.UserID == "" {
		return ErrBranchMemberRequired
	}
	if b.SIPUser == "" {
		return ErrBranchSIPUserRequired
	}
	if len(b.SIPUser) > maxSIPUserLen || !validSIPUser(b.SIPUser) {
		return ErrBranchSIPUserInvalid
	}
	if len(b.DisplayName) > maxDisplayNameLen || containsCtrlChar(b.DisplayName) {
		return ErrBranchDisplayNameInvalid
	}
	if len(b.Realm) > maxRealmLen {
		return ErrBranchRealmInvalid
	}
	if b.MaxContacts < 1 || b.MaxContacts > maxContactsCap {
		return ErrBranchMaxContactsInvalid
	}
	if len(b.Codecs) > maxCodecsList {
		return ErrBranchCodecInvalid
	}
	for _, c := range b.Codecs {
		if !c.IsValid() {
			return ErrBranchCodecInvalid
		}
	}
	if !b.RegistrationStatus.IsValid() {
		return ErrBranchRegistrationStatusInvalid
	}
	return nil
}

// GenerateSecret returns a fresh random SIP password (hex-encoded). It is the
// plaintext an operator types into the phone; only its HA1 is persisted.
func GenerateSecret() (string, error) {
	buf := make([]byte, secretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// ComputeHA1 returns the digest HA1 = MD5(sip_user:realm:password) as lowercase
// hex, the value a SIP registrar compares an Authorization response against.
func ComputeHA1(sipUser, realm, password string) string {
	sum := md5.Sum([]byte(sipUser + ":" + realm + ":" + password))
	return hex.EncodeToString(sum[:])
}

// SetSecret sets the realm and derives HA1 from a plaintext password. The caller
// is responsible for surfacing the plaintext to the operator exactly once and
// then discarding it.
func (b *Branch) SetSecret(realm, password string) {
	b.Realm = strings.TrimSpace(strings.ToLower(realm))
	b.SecretHA1 = ComputeHA1(strings.ToLower(strings.TrimSpace(b.SIPUser)), b.Realm, password)
	b.UpdatedAt = time.Now()
}

func (b *Branch) Touch() { b.UpdatedAt = time.Now() }
