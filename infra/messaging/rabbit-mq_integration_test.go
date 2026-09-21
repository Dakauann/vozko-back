package queue

import (
	"os"
	"testing"
	"time"

	"vozko/domain/messaging"
)

func integrationBroker(t *testing.T) *ConnectionPool {
	t.Helper()
	if os.Getenv("VOZKO_TEST_RABBITMQ") != "1" {
		t.Skip("set VOZKO_TEST_RABBITMQ=1 to run broker integration tests")
	}
	user, pass := os.Getenv("RABBITMQ_USERNAME"), os.Getenv("RABBITMQ_PASSWORD")
	pool := NewConnectionPool(user, pass, DefaultMaxChannelsPerConn)
	pub := NewRabbitMQQueuePub(pool, "vozko_test_exchange")
	if err := pub.ValidateConnection(); err != nil {
		t.Skipf("no broker reachable: %v", err)
	}
	return pool
}

func TestIntegration_PublishBeforeSubscribeIsNotLost(t *testing.T) {
	pool := integrationBroker(t)
	exchange := "vozko_test_exchange"
	topic := "vozko.test.publish.before.subscribe"

	pub := NewRabbitMQQueuePub(pool, exchange)
	sub := NewRabbitMQQueueSub(pool, exchange)
	t.Cleanup(func() { _ = sub.DeleteQueue(topic) })
	_ = sub.DeleteQueue(topic)

	if err := pub.Publish(topic, []byte(`{"hello":"queue"}`)); err != nil {
		t.Fatalf("publish: %v", err)
	}

	got := make(chan string, 1)
	err := sub.Subscribe(topic, func(message []byte, ack messaging.MessageAck) {
		_ = ack.Ack()
		select {
		case got <- string(message):
		default:
		}
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	select {
	case body := <-got:
		if body != `{"hello":"queue"}` {
			t.Fatalf("body = %q", body)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the message published before the subscriber existed was discarded by the exchange")
	}
}
