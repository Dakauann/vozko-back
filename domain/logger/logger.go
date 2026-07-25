package logger

type Logger interface {
	LogAlert(msg string, values ...interface{})
}
