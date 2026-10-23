package queueworkers

import (
	"log/slog"

	"vozko/domain/messaging"
)

type Pool struct {
	slots chan struct{}
}

func NewPool(size int) *Pool {
	if size < 1 {
		size = 1
	}
	return &Pool{slots: make(chan struct{}, size)}
}

func (p *Pool) Handler(handle func(payload []byte, ack messaging.MessageAck)) func([]byte, messaging.MessageAck) {
	return func(payload []byte, ack messaging.MessageAck) {
		p.slots <- struct{}{}
		go func() {
			defer func() { <-p.slots }()
			defer func() {
				if r := recover(); r != nil {
					slog.Error("queueworkers.handler_panic", slog.Any("panic", r))
					_ = ack.Nack(true)
				}
			}()
			handle(payload, ack)
		}()
	}
}
