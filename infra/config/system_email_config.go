package config

type SystemEmailConfig struct {
	WorkspaceID string
	SenderID    string
}

func LoadSystemEmailConfig() SystemEmailConfig {
	return SystemEmailConfig{
		WorkspaceID: mustGetEnvTrimmed("SYSTEM_EMAIL_WORKSPACE_ID"),
		SenderID:    mustGetEnvTrimmed("SYSTEM_EMAIL_SENDER_ID"),
	}
}
