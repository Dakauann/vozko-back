package queue

import (
	"os"
	"testing"
	"time"

	"vozko/domain/messaging"
)

// A round trip against a real broker, opt-in behind VOZKO_TEST_RABBITMQ=1 like
// the database tests are behind VOZKO_TEST_DB=1.
//
// This exists because the failure it covers is invisible to every unit test and
// to the broker's own error reporting. A direct exchange discards a message that
// matches no binding and CONFIRMS the publish while doing so, so a publisher
// whose consumer had not subscribed yet was told each message was delivered
// while each one was dropped. In production that silently lost alerts: the rule
// recorded a firing, the operator was shown "sent", and nothing left the
// process. Only a real broker can show the difference, because the whole bug is
// a property of what RabbitMQ does with an unroutable message.
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

// Publishing BEFORE anyone subscribes must still reach the subscriber.
func TestIntegration_PublishBeforeSubscribeIsNotLost(t *testing.T) {
	pool := integrationBroker(t)
	exchange := "vozko_test_exchange"
	topic := "vozko.test.publish.before.subscribe"

	pub := NewRabbitMQQueuePub(pool, exchange)
	sub := NewRabbitMQQueueSub(pool, exchange)
	t.Cleanup(func() { _ = sub.DeleteQueue(topic) })
	// A queue left over from an earlier run would hide a regression by holding
	// the binding this test is meant to prove the publisher creates.
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
