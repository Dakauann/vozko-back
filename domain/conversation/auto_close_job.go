package conversation

type AutoCloseJob interface {
	ProcessIdleCloses() error
}
