package workflow

import "time"

type KeyQueue struct {
	keys    <-chan rune
	ended   <-chan struct{}
	pending []rune
}

func NewKeyQueue(keys <-chan rune, ended <-chan struct{}) *KeyQueue {
	return &KeyQueue{keys: keys, ended: ended}
}

func (q *KeyQueue) Ended() bool {
	select {
	case <-q.ended:
		return true
	default:
		return false
	}
}

func (q *KeyQueue) Pressed() bool {
	if len(q.pending) > 0 {
		return true
	}
	select {
	case key := <-q.keys:
		q.pending = append(q.pending, key)
		return true
	default:
		return false
	}
}

func (q *KeyQueue) Next(timeout time.Duration) (rune, bool, error) {
	if len(q.pending) > 0 {
		key := q.pending[0]
		q.pending = q.pending[1:]
		return key, true, nil
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case key := <-q.keys:
		return key, true, nil
	case <-timer.C:
		return 0, false, nil
	case <-q.ended:
		return 0, false, ErrCallEnded
	}
}
