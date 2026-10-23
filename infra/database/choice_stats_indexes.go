package database

const (
	ChoicePromptSentAtIndex  = "idx_choice_prompts_sent_at"
	ChoicePromptMessageIndex = "idx_choice_prompts_message"
	ChoiceCountTemplateIndex = "idx_choice_counts_template_day"
	ChoiceCountDayIndex      = "idx_choice_counts_day"
)

func choiceStatsPerformanceIndexes() []ddl {
	return []ddl{
		{
			name: ChoicePromptSentAtIndex,
			sql: `CREATE INDEX IF NOT EXISTS idx_choice_prompts_sent_at
				ON choice_prompts (sent_at)`,
		},
		{
			name: ChoicePromptMessageIndex,
			sql: `CREATE INDEX IF NOT EXISTS idx_choice_prompts_message
				ON choice_prompts (workspace_id, provider_message_id)
				WHERE provider_message_id <> ''`,
		},
		{
			name: ChoiceCountTemplateIndex,
			sql: `CREATE INDEX IF NOT EXISTS idx_choice_counts_template_day
				ON choice_daily_counts (workspace_id, template_id, day)
				WHERE template_id <> ''`,
		},
		{
			name: ChoiceCountDayIndex,
			sql: `CREATE INDEX IF NOT EXISTS idx_choice_counts_day
				ON choice_daily_counts (day)`,
		},
	}
}
