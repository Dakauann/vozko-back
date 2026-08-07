package unofficial_whatsapp

import (
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	user_domain "vozko/domain/user"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
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

	// TEMPORARY: provisioning is platform-admin only until pricing is decided.
	//
	// Every other verb below keeps its normal workspace RBAC. Only CREATE is
	// held back, because creating an instance is the one action that consumes a
	// slot on the shared host and would bill a workspace under a model that does
	// not exist yet. Linking, resetting and messaging an already-provisioned
	// number cost nothing extra, so an attendant keeps them.
	//
	// The gate sits INSIDE the RBAC guard, not around it. Both must pass either
	// way, so the nesting is not a security choice — it keeps ActionCreate the
	// outermost wrapper, which is the invariant routes_test asserts for every
	// route here. Wrapping the other way makes the RBAC undetectable to that
	// test, and a test that can no longer see a missing RBAC guard is worse than
	// the temporary restriction is valuable.
	//
	// A platform admin still needs ActionCreate on the workspace, so this cannot
	// accidentally widen anyone's access. To lift it, unwrap h.CreateInstance
	// and delete requirePlatformAdmin — nothing else references either.
	r.HandleFunc("/instances", ac(res, workspace_domain.ActionCreate, requirePlatformAdmin(h.CreateInstance))).Methods(http.MethodPost)
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

// requirePlatformAdmin rejects everyone but a platform admin.
//
// TEMPORARY, paired with the CREATE route above: unofficial numbers occupy
// capacity on a shared host and there is no pricing for them yet, so the only
// people allowed to consume that capacity are the ones who own the host.
//
// It denies by default. Absent claims mean the request did not come through the
// auth middleware, which on a protected route is a wiring fault, and the safe
// reading of a wiring fault on a provisioning endpoint is "no".
//
// 403 rather than 404: the caller is authenticated and the resource exists, so
// hiding it would only cost them the round trips of guessing. The message names
// the restriction and that it is temporary, because "forbidden" alone reads as
// a permissions bug to an admin who knows they have the right role.
func requirePlatformAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := middleware.GetClaims(r)
		if claims == nil || claims.Role != string(user_domain.RoleAdmin) {
			response.WriteError(
				w,
				http.StatusForbidden,
				"provisioning unofficial WhatsApp numbers is temporarily restricted to platform administrators",
				nil,
			)
			return
		}
		next(w, r)
	}
}
