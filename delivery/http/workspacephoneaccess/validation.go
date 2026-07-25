package workspacephoneaccess

func (r *GrantPhoneAccessRequest) Validate() map[string]string {
	if r.WorkspaceID == "" {
		return map[string]string{"workspaceId": "required"}
	}
	if r.PhoneID == "" {
		return map[string]string{"phoneId": "required"}
	}
	return nil
}

func (r *RevokePhoneAccessRequest) Validate() map[string]string {
	if r.WorkspaceID == "" {
		return map[string]string{"workspaceId": "required"}
	}
	if r.PhoneID == "" {
		return map[string]string{"phoneId": "required"}
	}
	return nil
}
