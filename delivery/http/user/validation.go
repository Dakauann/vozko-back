package user

import (
	"strings"

	userdomain "vozko/domain/user"
)

func (r *updateRoleRequest) Validate() map[string]string {
	r.Role = strings.TrimSpace(r.Role)
	role := userdomain.Role(r.Role)
	if role != userdomain.RoleAdmin && role != userdomain.RoleUser {
		return map[string]string{"role": "must be 'admin' or 'user'"}
	}
	return nil
}

func (r *DeleteMeRequest) Validate() map[string]string {
	if strings.TrimSpace(r.CurrentPassword) == "" {
		return map[string]string{"currentPassword": "required"}
	}
	return nil
}
