package conversation

// AutoCloseJob finishes idle open conversations (cron tick).
type AutoCloseJob interface {
	ProcessIdleCloses() error
}
