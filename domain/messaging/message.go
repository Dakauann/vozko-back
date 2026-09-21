package messaging

type QueueMessage struct {
	Topic   string
	Payload []byte
}
