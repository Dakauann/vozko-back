package unofficial_whatsapp

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

// RegisterProtectedRoutes wires the authenticated endpoints.
//
// Read/write splits along the RBAC actions used elsewhere: provisioning is
// ActionCreate, reading is ActionRead, everything that changes a live number —
// linking it, editing it, rotating its webhook, resetting it — is ActionUpdate,
// and removing it is ActionDelete.
//
// Linking is an UPDATE rather than a CREATE on purpose: the slot already exists
// by then, and an attendant allowed to reconnect a dropped session should not
// thereby be able to provision new numbers against the workspace's capacity.
func RegisterProtectedRoutes(
	protected *mux.Router,
	h *Handler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	// A nil handler means the channel is not wired; register nothing rather
	// than routes whose methods would nil-panic on the first request.
	if h == nil {
		return
	}

	res := workspace_domain.ResourceUnofficialWhatsAppInstances
	r := protected.PathPrefix("/unofficial-whatsapp").Subrouter()

	// Provisioning is workspace RBAC plus an ENTITLEMENT, no longer a
	// platform-admin lockout.
	//
	// It used to be admin-only, as a holding position "until pricing is decided",
	// because creating an instance consumes a slot on a host we operate and there
	// was no model for who may have how many. That model now exists: a platform
	// administrator grants an allowance per workspace and addons top it up, and
	// ProvisionInstanceUseCase refuses past it — so the restriction that stood in
	// for pricing is replaced by the pricing.
	//
	// The gate moved DOWN a layer deliberately. A route guard could only ever
	// answer "is this person an admin"; the use case answers "does this workspace
	// have a slot left", which is the question, and it answers it identically for
	// every caller — the HTTP client, a future self-serve flow, an internal tool.
	r.HandleFunc("/instances", ac(res, workspace_domain.ActionCreate, h.CreateInstance)).Methods(http.MethodPost)

	// What the connect screen reads before offering the button. ActionRead: it
	// reports a limit and a usage, not a way to change either.
	r.HandleFunc("/instances/allowance", ac(res, workspace_domain.ActionRead, h.GetAllowance)).Methods(http.MethodGet)
	r.HandleFunc("/instances", ac(res, workspace_domain.ActionRead, h.ListInstances)).Methods(http.MethodGet)
	r.HandleFunc("/instances/{id}", ac(res, workspace_domain.ActionRead, h.GetInstance)).Methods(http.MethodGet)
	r.HandleFunc("/instances/{id}", ac(res, workspace_domain.ActionUpdate, h.UpdateInstance)).Methods(http.MethodPut)
	r.HandleFunc("/instances/{id}", ac(res, workspace_domain.ActionDelete, h.DeleteInstance)).Methods(http.MethodDelete)

	// Linking. The status endpoint is polled every couple of seconds while a
	// code is on screen, which is why it is a plain read of our own row plus one
	// host call rather than anything heavier.
	r.HandleFunc("/instances/{id}/connect", ac(res, workspace_domain.ActionUpdate, h.Connect)).Methods(http.MethodPost)
	r.HandleFunc("/instances/{id}/link-status", ac(res, workspace_domain.ActionRead, h.LinkStatus)).Methods(http.MethodGet)
	r.HandleFunc("/instances/{id}/disconnect", ac(res, workspace_domain.ActionUpdate, h.Disconnect)).Methods(http.MethodPost)

	// Repair actions.
	r.HandleFunc("/instances/{id}/reset", ac(res, workspace_domain.ActionUpdate, h.Reset)).Methods(http.MethodPost)
	r.HandleFunc("/instances/{id}/webhook/rotate", ac(res, workspace_domain.ActionUpdate, h.RotateWebhookToken)).Methods(http.MethodPost)

	// ActionSend, not ActionUpdate. Answering someone who wrote to us and
	// messaging a stranger are different privileges: the second is cold outbound
	// and is the fastest way to lose an unofficial number, so an attendant with
	// full attendance rights does not get it by default.
	r.HandleFunc("/instances/{id}/conversations", ac(res, workspace_domain.ActionSend, h.StartConversation)).Methods(http.MethodPost)

}

// RegisterGroupRoutes wires the group panel.
//
// The permission split is the point of this function, and it is sharper than the
// one above. Everything here acts inside the CUSTOMER'S OWN WhatsApp groups, on
// people who are not our users and did not consent to our RBAC:
//
//	ActionRead   — list, open, read the roster
//	ActionUpdate — rename, describe, re-picture, invite, promote, demote, approve
//	ActionDelete — remove a member, reject a request, leave the group
//
// Removal is ActionDelete rather than ActionUpdate deliberately. Evicting a
// customer from a group they are in is irreversible from our side and visible to
// everyone in it, and an attendant trusted to invite people is not thereby
// trusted to throw them out. The handler re-checks which class of verb the body
// asked for, so the two participant routes cannot be used interchangeably.
//
// The group id travels in the path as its JID rather than as our row id: a
// workspace can act on a group the CRM has never opened and therefore has no row
// for, and requiring one would make "sync this group for the first time"
// impossible to express.
func RegisterGroupRoutes(
	protected *mux.Router,
	h *GroupHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	if h == nil {
		return
	}

	res := workspace_domain.ResourceUnofficialWhatsAppInstances
	r := protected.PathPrefix("/unofficial-whatsapp").Subrouter()

	r.HandleFunc("/instances/{id}/groups", ac(res, workspace_domain.ActionRead, h.ListGroups)).Methods(http.MethodGet)
	r.HandleFunc("/instances/{id}/groups/{groupJid}", ac(res, workspace_domain.ActionRead, h.GetGroup)).Methods(http.MethodGet)
	r.HandleFunc("/instances/{id}/groups/{groupJid}/invite-link", ac(res, workspace_domain.ActionRead, h.GetInviteLink)).Methods(http.MethodGet)

	r.HandleFunc("/instances/{id}/groups/{groupJid}", ac(res, workspace_domain.ActionUpdate, h.UpdateGroup)).Methods(http.MethodPatch)
	r.HandleFunc("/instances/{id}/groups/{groupJid}/participants", ac(res, workspace_domain.ActionUpdate, h.UpdateParticipants)).Methods(http.MethodPost)

	r.HandleFunc("/instances/{id}/groups/{groupJid}/participants", ac(res, workspace_domain.ActionDelete, h.RemoveParticipants)).Methods(http.MethodDelete)
	r.HandleFunc("/instances/{id}/groups/{groupJid}/leave", ac(res, workspace_domain.ActionDelete, h.LeaveGroup)).Methods(http.MethodPost)

	// The same endpoints addressed by CONVERSATION.
	//
	// The CRM holds an entry id and an entry type, never a WhatsApp JID. Giving
	// it a second route shape costs one lookup per call; the alternative was
	// leaking a channel-specific `group_jid` into InboxEntry, the
	// channel-neutral shape every list in the CRM renders — which would have
	// made a WhatsApp detail visible to Instagram, Telegram and the Cloud API.
	//
	// Identical RBAC, deliberately: the same act must not become cheaper by
	// being reached through a different id.
	r.HandleFunc("/conversations/{entryId}/group", ac(res, workspace_domain.ActionRead, h.GetGroup)).Methods(http.MethodGet)
	r.HandleFunc("/conversations/{entryId}/group/invite-link", ac(res, workspace_domain.ActionRead, h.GetInviteLink)).Methods(http.MethodGet)
	r.HandleFunc("/conversations/{entryId}/group", ac(res, workspace_domain.ActionUpdate, h.UpdateGroup)).Methods(http.MethodPatch)
	r.HandleFunc("/conversations/{entryId}/group/participants", ac(res, workspace_domain.ActionUpdate, h.UpdateParticipants)).Methods(http.MethodPost)
	r.HandleFunc("/conversations/{entryId}/group/participants", ac(res, workspace_domain.ActionDelete, h.RemoveParticipants)).Methods(http.MethodDelete)
	r.HandleFunc("/conversations/{entryId}/group/leave", ac(res, workspace_domain.ActionDelete, h.LeaveGroup)).Methods(http.MethodPost)
}
