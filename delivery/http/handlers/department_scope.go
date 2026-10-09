package handlers

import (
	"net/http"

	"vozko/delivery/http/httpx"
)

func withDepartmentCreationScope(r *http.Request, explicitDepartmentID string) *http.Request {
	return httpx.WithCreationScope(r, explicitDepartmentID)
}

func departmentFilterIDs(r *http.Request) []string {
	return httpx.DepartmentFilterIDs(r)
}

func shouldReturnEmptyDepartmentList(r *http.Request) bool {
	return httpx.ShouldReturnEmptyDepartmentList(r)
}

func canAccessDepartment(r *http.Request, resourceDepartmentID string) bool {
	return httpx.CanAccessDepartment(r, resourceDepartmentID)
}
