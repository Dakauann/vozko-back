package middleware

import (
	"context"
	"log"
	"net/http"
	"strings"

	"vozko/delivery/http/response"
	"vozko/domain/workspace"
	dept "vozko/domain/workspace/workspace_department"
)

type departmentContextKey string

const DepartmentFilterContextKey departmentContextKey = "departmentFilter"

type DepartmentIDsResolver interface {
	ListDepartments(workspaceID string) ([]dept.Department, error)
	GetMemberDepartmentIDs(workspaceID, userID string) ([]string, error)
	GetDepartmentByID(id string) (*dept.Department, error)
}

type DepartmentMiddleware struct {
	resolver          DepartmentIDsResolver
	membershipChecker WorkspaceMembershipChecker
}

func NewDepartmentMiddleware(resolver DepartmentIDsResolver, membershipChecker WorkspaceMembershipChecker) *DepartmentMiddleware {
	return &DepartmentMiddleware{
		resolver:          resolver,
		membershipChecker: membershipChecker,
	}
}

func (m *DepartmentMiddleware) ResolveDepartment() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetClaims(r)
			if claims == nil {
				next.ServeHTTP(w, r)
				return
			}

			wsID := GetWorkspaceID(r)
			if wsID == "" {
				next.ServeHTTP(w, r)
				return
			}

			selected := strings.TrimSpace(r.Header.Get("X-Department-ID"))

			isOwnerOrAdmin := claims.Role == "admin"
			if !isOwnerOrAdmin && m.membershipChecker != nil {
				member, err := m.membershipChecker.GetMember(wsID, claims.UserID)
				if err != nil {
					log.Printf("department-resolver: error fetching workspace member for user %s ws %s: %v", claims.UserID, wsID, err)
				} else if member != nil && (member.Role == workspace.RoleOwner || member.Role == workspace.RoleAdmin) {
					isOwnerOrAdmin = true
				}
			}

			if isOwnerOrAdmin {
				filter := &dept.DepartmentFilter{IsOwnerOrAdmin: true}
				if selected != "" {
					belongs, err := m.departmentBelongsToWorkspace(wsID, selected)
					if err != nil {
						log.Printf("department-resolver: error validating selected department %s for ws %s: %v", selected, wsID, err)
						response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
						return
					}
					if !belongs {
						response.WriteError(w, http.StatusForbidden,
							"Selected department does not belong to this workspace", nil)
						return
					}
					filter.SelectedDepartmentID = &selected
				}

				ctx := context.WithValue(r.Context(), DepartmentFilterContextKey, filter)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			deptIDs, err := m.resolver.GetMemberDepartmentIDs(wsID, claims.UserID)
			if err != nil {
				log.Printf("department-resolver: error fetching departments for user %s ws %s: %v", claims.UserID, wsID, err)

				deptIDs = nil
			}

			workspaceHasDepartments := len(deptIDs) > 0
			if !workspaceHasDepartments && m.resolver != nil {
				depts, listErr := m.resolver.ListDepartments(wsID)
				if listErr != nil {
					log.Printf("department-resolver: error listing departments for ws %s: %v", wsID, listErr)

					workspaceHasDepartments = true
				} else {
					workspaceHasDepartments = len(depts) > 0
				}
			}

			filter := &dept.DepartmentFilter{
				DepartmentIDs:           deptIDs,
				WorkspaceHasDepartments: workspaceHasDepartments,
			}

			if selected != "" {
				if len(deptIDs) == 0 {

				} else if !contains(deptIDs, selected) {
					response.WriteError(w, http.StatusForbidden,
						"Not a member of the selected department", nil)
					return
				} else {
					filter.SelectedDepartmentID = &selected
				}
			}

			ctx := context.WithValue(r.Context(), DepartmentFilterContextKey, filter)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetDepartmentFilter(r *http.Request) *dept.DepartmentFilter {
	f, _ := r.Context().Value(DepartmentFilterContextKey).(*dept.DepartmentFilter)
	return f
}

func contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

func (m *DepartmentMiddleware) departmentBelongsToWorkspace(workspaceID, departmentID string) (bool, error) {
	if departmentID == "" || m.resolver == nil {
		return false, nil
	}

	department, err := m.resolver.GetDepartmentByID(departmentID)
	if err != nil {
		return false, err
	}
	if department == nil {
		return false, nil
	}

	return department.WorkspaceID == workspaceID, nil
}
