package workspacetemplateaccess

func (r *GrantAccessRequest) Validate() map[string]string {
	if r.WorkspaceID == "" {
		return map[string]string{"workspaceId": "required"}
	}
	if r.TemplateID == "" {
		return map[string]string{"templateId": "required"}
	}
	return nil
}

func (r *RevokeAccessRequest) Validate() map[string]string {
	if r.WorkspaceID == "" {
		return map[string]string{"workspaceId": "required"}
	}
	if r.TemplateID == "" {
		return map[string]string{"templateId": "required"}
	}
	return nil
}
