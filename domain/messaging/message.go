package messaging

type QueueMessage struct {
	Topic string
	// TODO: update for clearer structure
	Payload []byte
}
