package workspacedepartment

import (
	"time"

	workspacedepartmentdomain "vozko/domain/workspace/workspace_department"
)

type CreateDepartmentRequest struct {
	Name        string `json:"name" example:"Suporte"`
	Description string `json:"description" example:"Equipe de atendimento ao cliente"`
}

type UpdateDepartmentRequest struct {
	Name        *string `json:"name" example:"Suporte"`
	Description *string `json:"description" example:"Equipe de atendimento ao cliente"`
}

type AddMemberRequest struct {
	MemberID string `json:"memberId" example:"mem_a1b2c3"`
}

type DepartmentResponse struct {
	ID          string    `json:"id" example:"dept_a1b2c3"`
	WorkspaceID string    `json:"workspaceId" example:"ws_a1b2c3"`
	Name        string    `json:"name" example:"Suporte"`
	Description string    `json:"description,omitempty" example:"Equipe de atendimento ao cliente"`
	MemberCount int       `json:"memberCount,omitempty" example:"5"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type DepartmentMemberResponse struct {
	ID           string    `json:"id" example:"dm_a1b2c3"`
	DepartmentID string    `json:"departmentId" example:"dept_a1b2c3"`
	MemberID     string    `json:"memberId" example:"mem_a1b2c3"`
	UserID       string    `json:"userId,omitempty" example:"usr_a1b2c3"`
	Email        string    `json:"email,omitempty" example:"maria@empresa.com.br"`
	Username     string    `json:"username,omitempty" example:"Maria Silva"`
	Role         string    `json:"role,omitempty" example:"agent"`
	CreatedAt    time.Time `json:"createdAt"`
}

func departmentResponseFrom(d workspacedepartmentdomain.Department) DepartmentResponse {
	return DepartmentResponse{
		ID:          d.ID,
		WorkspaceID: d.WorkspaceID,
		Name:        d.Name,
		Description: d.Description,
		MemberCount: d.MemberCount,
		CreatedAt:   d.CreatedAt,
		UpdatedAt:   d.UpdatedAt,
	}
}

func toDepartmentResponse(d *workspacedepartmentdomain.Department) *DepartmentResponse {
	if d == nil {
		return nil
	}
	resp := departmentResponseFrom(*d)
	return &resp
}

func toDepartmentResponses(items []workspacedepartmentdomain.Department) []DepartmentResponse {
	if items == nil {
		return nil
	}
	out := make([]DepartmentResponse, len(items))
	for i, it := range items {
		out[i] = departmentResponseFrom(it)
	}
	return out
}

func departmentMemberResponseFrom(m workspacedepartmentdomain.DepartmentMember) DepartmentMemberResponse {
	return DepartmentMemberResponse{
		ID:           m.ID,
		DepartmentID: m.DepartmentID,
		MemberID:     m.MemberID,
		UserID:       m.UserID,
		Email:        m.Email,
		Username:     m.Username,
		Role:         m.Role,
		CreatedAt:    m.CreatedAt,
	}
}

func toDepartmentMemberResponse(m *workspacedepartmentdomain.DepartmentMember) *DepartmentMemberResponse {
	if m == nil {
		return nil
	}
	resp := departmentMemberResponseFrom(*m)
	return &resp
}

func toDepartmentMemberResponses(items []workspacedepartmentdomain.DepartmentMember) []DepartmentMemberResponse {
	if items == nil {
		return nil
	}
	out := make([]DepartmentMemberResponse, len(items))
	for i, it := range items {
		out[i] = departmentMemberResponseFrom(it)
	}
	return out
}
