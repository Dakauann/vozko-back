package queue

import (
	"errors"
	"fmt"
	"log"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"
)

const DefaultMaxChannelsPerConn = 100

var ErrPoolClosed = errors.New("connection pool is closed")

type amqpConnector interface {
	openChannel() (*amqp.Channel, error)
	isClosed() bool
	close() error
}

type realAMQPConnector struct{ conn *amqp.Connection }

func (r *realAMQPConnector) openChannel() (*amqp.Channel, error) { return r.conn.Channel() }
func (r *realAMQPConnector) isClosed() bool                      { return r.conn == nil || r.conn.IsClosed() }
func (r *realAMQPConnector) close() error {
	if r.conn != nil {
		return r.conn.Close()
	}
	return nil
}

type ConnectionPool struct {
	mu           sync.Mutex
	connFactory  func() (amqpConnector, error)
	entries      []*poolEntry
	maxChPerConn int
	closed       bool
}

type poolEntry struct {
	connector amqpConnector
	chanCount int
}

func NewConnectionPool(username, password string, maxChannelsPerConn int) *ConnectionPool {
	host := getRabbitMQHost()
	port := getRabbitMQPort()
	uri := fmt.Sprintf("amqp://%s:%s@%s:%s/", username, password, host, port)

	if maxChannelsPerConn <= 0 {
		maxChannelsPerConn = DefaultMaxChannelsPerConn
	}

	return &ConnectionPool{
		connFactory: func() (amqpConnector, error) {
			conn, err := amqp.Dial(uri)
			if err != nil {
				return nil, err
			}
			return &realAMQPConnector{conn: conn}, nil
		},
		maxChPerConn: maxChannelsPerConn,
	}
}

func (p *ConnectionPool) OpenChannel() (*amqp.Channel, func(), error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil, nil, ErrPoolClosed
	}

	var target *poolEntry
	alive := p.entries[:0]
	for _, e := range p.entries {
		if e.connector.isClosed() {
			_ = e.connector.close()
			log.Printf("rabbitmq pool: pruned dead connection (had %d channels)", e.chanCount)
			continue
		}
		alive = append(alive, e)
		if target == nil && e.chanCount < p.maxChPerConn {
			target = e
		}
	}
	p.entries = alive

	if target == nil {
		c, err := p.connFactory()
		if err != nil {
			return nil, nil, fmt.Errorf("pool: dial failed: %w", err)
		}
		target = &poolEntry{connector: c}
		p.entries = append(p.entries, target)
		log.Printf("rabbitmq pool: opened new connection (total: %d)", len(p.entries))
	}

	ch, err := target.connector.openChannel()
	if err != nil {

		_ = target.connector.close()
		for i, e := range p.entries {
			if e == target {
				p.entries = append(p.entries[:i], p.entries[i+1:]...)
				break
			}
		}
		return nil, nil, fmt.Errorf("pool: failed to open channel: %w", err)
	}

	target.chanCount++

	captured := target
	var once sync.Once
	release := func() {
		once.Do(func() {
			p.mu.Lock()
			captured.chanCount--
			p.mu.Unlock()
		})
	}

	return ch, release, nil
}

func (p *ConnectionPool) Stats() (connections, channels int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	connections = len(p.entries)
	for _, e := range p.entries {
		channels += e.chanCount
	}
	return
}

func (p *ConnectionPool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.closed = true
	var firstErr error
	for _, e := range p.entries {
		if err := e.connector.close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	p.entries = nil
	return firstErr
}
