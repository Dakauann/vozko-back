package middleware

import (
	"context"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/workspace"
)

type workspaceContextKey string

const WorkspaceIDContextKey workspaceContextKey = "workspaceId"

type WorkspaceAccessChecker interface {
	Execute(userID, workspaceID string, resource workspace.Resource, action workspace.Action) error
}

type WorkspaceMembershipChecker interface {
	GetMember(workspaceID, userID string) (*workspace.Member, error)
}

type DefaultWorkspaceResolver interface {
	Execute(userID, email, referralCode string) (*workspace.Workspace, error)
}

type WorkspaceMiddleware struct {
	accessChecker     WorkspaceAccessChecker
	membershipChecker WorkspaceMembershipChecker
	defaultResolver   DefaultWorkspaceResolver
}

func NewWorkspaceMiddleware(accessChecker WorkspaceAccessChecker, membershipChecker WorkspaceMembershipChecker, defaultResolver DefaultWorkspaceResolver) *WorkspaceMiddleware {
	return &WorkspaceMiddleware{
		accessChecker:     accessChecker,
		membershipChecker: membershipChecker,
		defaultResolver:   defaultResolver,
	}
}

func (m *WorkspaceMiddleware) ResolveWorkspace() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetClaims(r)
			if claims == nil {

				next.ServeHTTP(w, r)
				return
			}

			if existing, _ := r.Context().Value(WorkspaceIDContextKey).(string); existing != "" {
				next.ServeHTTP(w, r)
				return
			}

			wsID := r.Header.Get("X-Workspace-ID")
			if wsID == "" {
				wsID = r.URL.Query().Get("workspace_id")
			}

			if wsID != "" && claims.Role != "admin" && m.membershipChecker != nil {
				member, err := m.membershipChecker.GetMember(wsID, claims.UserID)
				if err != nil {

					log.Printf("workspace-resolver: membership check error for user %s workspace %s: %v", claims.UserID, wsID, err)
					response.WriteError(w, http.StatusForbidden, "Could not verify workspace membership. Please try again.", nil)
					return
				}
				if member == nil {
					// The requested workspace is not one this user belongs to, almost
					// always a stale/foreign X-Workspace-ID left in a cookie (an old
					// session or a second account). Do NOT 403 the whole request: that
					// bricks the entire session, including workspace-agnostic endpoints
					// like pending invites. Ignore the invalid id and fall back to the
					// user's default workspace below. This grants no extra access, the
					// per-route access checks still run against the resolved workspace.
					log.Printf("workspace-resolver: user %s is not a member of requested workspace %s, ignoring stale id, falling back to default", claims.UserID, wsID)
					wsID = ""
				}
			}

			if wsID == "" && m.defaultResolver != nil {
				ws, err := m.defaultResolver.Execute(claims.UserID, claims.Email, extractReferralCode(r))
				if err != nil {
					log.Printf("workspace-resolver: failed to resolve default workspace for user %s: %v", claims.UserID, err)
				} else if ws != nil {
					wsID = ws.ID
				}
			}

			if wsID != "" {
				r.Header.Set("X-Workspace-ID", wsID)
				ctx := context.WithValue(r.Context(), WorkspaceIDContextKey, wsID)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func (m *WorkspaceMiddleware) RequireAccess(resource workspace.Resource, action workspace.Action) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetClaims(r)
			if claims == nil {
				response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
				return
			}

			if claims.Role == "admin" {
				next.ServeHTTP(w, r)
				return
			}

			wsID := mux.Vars(r)["workspaceId"]
			if wsID == "" {
				wsID = r.Header.Get("X-Workspace-ID")
			}
			if wsID == "" {
				wsID = r.URL.Query().Get("workspace_id")
			}

			if wsID == "" {
				wsID, _ = r.Context().Value(WorkspaceIDContextKey).(string)
			}
			if wsID == "" {
				response.WriteError(w, http.StatusBadRequest, "workspace_id is required", map[string]string{
					"X-Workspace-ID": "string (required, as path param or header)",
				})
				return
			}

			if err := m.accessChecker.Execute(claims.UserID, wsID, resource, action); err != nil {
				response.WriteError(w, http.StatusForbidden,
					"Insufficient permissions: requires "+string(resource)+":"+string(action)+" in this workspace",
					map[string]string{
						"required_permission": string(resource) + ":" + string(action),
					})
				return
			}

			ctx := context.WithValue(r.Context(), WorkspaceIDContextKey, wsID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func (m *WorkspaceMiddleware) CheckAccess(resource workspace.Resource, action workspace.Action, handler http.HandlerFunc) http.HandlerFunc {
	wrapped := m.RequireAccess(resource, action)(handler)
	return wrapped.ServeHTTP
}

func (m *WorkspaceMiddleware) CheckMembership(handler http.HandlerFunc) http.HandlerFunc {
	wrapped := m.RequireMembership()(handler)
	return wrapped.ServeHTTP
}

func (m *WorkspaceMiddleware) RequireMembership() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetClaims(r)
			if claims == nil {
				response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
				return
			}

			wsID := mux.Vars(r)["workspaceId"]
			if wsID == "" {
				wsID = r.Header.Get("X-Workspace-ID")
			}
			if wsID == "" {
				wsID = r.URL.Query().Get("workspace_id")
			}
			if wsID == "" {
				response.WriteError(w, http.StatusBadRequest, "workspace_id is required", map[string]string{
					"X-Workspace-ID": "string (required, as path param or header)",
				})
				return
			}

			member, err := m.membershipChecker.GetMember(wsID, claims.UserID)
			if err != nil || member == nil {
				response.WriteError(w, http.StatusForbidden, "Not a member of this workspace", nil)
				return
			}

			ctx := context.WithValue(r.Context(), WorkspaceIDContextKey, wsID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetWorkspaceID(r *http.Request) string {
	wsID, _ := r.Context().Value(WorkspaceIDContextKey).(string)
	if wsID == "" {
		wsID = mux.Vars(r)["workspaceId"]
	}
	if wsID == "" {
		wsID = r.Header.Get("X-Workspace-ID")
	}
	if wsID == "" {
		wsID = r.URL.Query().Get("workspace_id")
	}
	return wsID
}

func ExtractReferralCode(r *http.Request) string {
	return extractReferralCode(r)
}

func extractReferralCode(r *http.Request) string {
	if r == nil {
		return ""
	}
	if v := strings.TrimSpace(r.Header.Get("X-Affiliate-Ref")); v != "" {
		return v
	}
	if c, err := r.Cookie("affiliate_ref"); err == nil && c != nil {
		return strings.TrimSpace(c.Value)
	}
	return ""
}
