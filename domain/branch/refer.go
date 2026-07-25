package branch

import (
	"errors"
	"strings"
)

// Phone-initiated transfer (SIP REFER) resolution. This is the numbering-plan
// brain: when a registered phone presses its "Transfer" button and dials an
// extension, it sends Vozko a REFER whose Refer-To URI carries the dialed
// extension as its USER PART (exactly as Asterisk res_pjsip_refer reads it:
// `ast_copy_pj_str(exten, &target->user, ...)` then `ast_exists_extension(...)`).
// This resolver maps that extension to the target member, scoped to the referring
// phone's workspace (Asterisk's transfer "context"), and classifies the failure
// with the same SIP semantics Asterisk uses.
//
// Discovery note: there is NO SIP directory push. The phone user simply DIALS the
// extension number they already know (from a provisioned contact list, a printed
// directory, memory, or a BLF speed-dial key). Live presence of an extension is a
// separate opt-in mechanism (SUBSCRIBE Event: dialog -> NOTIFY dialog-info+xml,
// i.e. BLF), not part of the transfer itself.
var (
	// ErrReferNoTarget: the REFER carried no dialable extension (empty user part).
	// SIP: 400 Bad Request.
	ErrReferNoTarget = errors.New("refer: missing target extension")
	// ErrReferAttendedUnsupported: the Refer-To carried a Replaces parameter (an
	// ATTENDED phone transfer). Blind phone transfer is supported; attended
	// (REFER+Replaces from the phone) is not yet. SIP: 501 Not Implemented.
	ErrReferAttendedUnsupported = errors.New("refer: attended (Replaces) transfer from a phone is not supported yet")
	// ErrReferUnknownExtension: no enabled branch in the workspace answers to the
	// dialed extension. SIP: 404 Not Found (Asterisk's "target does not exist").
	ErrReferUnknownExtension = errors.New("refer: no branch for the dialed extension")
	// ErrReferSelf: the phone dialed its own extension. SIP: 400 Bad Request.
	ErrReferSelf = errors.New("refer: cannot transfer to the same extension")
)

// ReferTargetResolver resolves a phone-dialed extension to the branch (and thus
// member) a REFER should transfer the call to. Pure over the branch repository so
// the SIP wire layer (infra) stays free of numbering-plan logic and this is
// unit-testable.
type ReferTargetResolver struct {
	branches Repository
}

func NewReferTargetResolver(branches Repository) *ReferTargetResolver {
	return &ReferTargetResolver{branches: branches}
}

// ReferTarget is the resolved destination of a phone-initiated blind transfer.
type ReferTarget struct {
	// Branch is the target ramal (its UserID/MemberID identify the member the
	// transfer engine forks the offer to, exactly like a browser-initiated one).
	Branch *Branch
}

// Resolve maps the dialed extension (the Refer-To user part) to a transfer target,
// scoped to the referring phone's workspace. hasReplaces marks an attended REFER,
// which is rejected up front (blind only for now). The returned error maps to a
// SIP status at the wire (see the sentinels above).
func (r *ReferTargetResolver) Resolve(referrer *Branch, extension string, hasReplaces bool) (*ReferTarget, error) {
	if hasReplaces {
		return nil, ErrReferAttendedUnsupported
	}
	if referrer == nil {
		return nil, ErrReferUnknownExtension
	}
	extension = strings.TrimSpace(extension)
	if extension == "" {
		return nil, ErrReferNoTarget
	}
	if strings.EqualFold(extension, referrer.SIPUser) {
		return nil, ErrReferSelf
	}

	// Workspace-scoped lookup = Asterisk's per-endpoint transfer context, and the
	// cross-tenant isolation guarantee: a phone can only forward to an extension in
	// its own workspace.
	target, err := r.branches.FindBySIPUser(referrer.WorkspaceID, extension)
	if err != nil || target == nil {
		return nil, ErrReferUnknownExtension
	}
	// A disabled or DND target is treated as "no such reachable extension" (404),
	// same as Asterisk rejecting a transfer to an extension with no reachable
	// device. The transfer engine's own ring/forward policy then applies once the
	// offer is placed (busy / no-answer handling is not this resolver's job).
	if !target.Enabled || target.DND {
		return nil, ErrReferUnknownExtension
	}
	return &ReferTarget{Branch: target}, nil
}
