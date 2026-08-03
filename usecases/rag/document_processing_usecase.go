package rag_usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"vozko/domain/messaging"
	"vozko/domain/rag"
)

type publishDocumentProcessingUseCase struct {
	queuePub messaging.MessageQueuePub
}

func NewPublishDocumentProcessingUseCase(
	queuePub messaging.MessageQueuePub,
) rag.PublishDocumentProcessingUseCase {
	return &publishDocumentProcessingUseCase{queuePub: queuePub}
}

func (uc *publishDocumentProcessingUseCase) Execute(ctx context.Context, documentID string) error {
	msg := rag.DocumentProcessingMessage{
		DocumentID: documentID,
		Attempt:    1,
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal document processing message: %w", err)
	}

	if err := uc.queuePub.Publish(rag.DocumentProcessingTopic, payload); err != nil {
		return fmt.Errorf("failed to publish document processing message: %w", err)
	}

	log.Printf("[RAG] published processing job for document %s", documentID)
	return nil
}

type consumeDocumentProcessingUseCase struct {
	queueSub  messaging.MessageQueueSub
	queuePub  messaging.MessageQueuePub
	processor rag.DocumentProcessor
	docRepo   rag.DocumentRepository
	semaphore chan struct{}
}

func NewConsumeDocumentProcessingUseCase(
	queueSub messaging.MessageQueueSub,
	queuePub messaging.MessageQueuePub,
	processor rag.DocumentProcessor,
	docRepo rag.DocumentRepository,
) rag.ConsumeDocumentProcessingUseCase {
	return &consumeDocumentProcessingUseCase{
		queueSub:  queueSub,
		queuePub:  queuePub,
		processor: processor,
		docRepo:   docRepo,
		semaphore: make(chan struct{}, 3),
	}
}

func (uc *consumeDocumentProcessingUseCase) Start() error {
	return uc.queueSub.Subscribe(rag.DocumentProcessingTopic, func(payload []byte, ack messaging.MessageAck) {
		uc.handle(payload, ack)
	})
}

func (uc *consumeDocumentProcessingUseCase) handle(payload []byte, ack messaging.MessageAck) {

	var msg rag.DocumentProcessingMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		log.Printf("[RAG] consumer: invalid message, nacking without requeue: %v", err)
		_ = ack.Nack(false)
		return
	}

	uc.semaphore <- struct{}{}

	go func(m rag.DocumentProcessingMessage) {
		defer func() { <-uc.semaphore }()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[RAG] consumer: panic processing document %s: %v", m.DocumentID, r)

				_ = ack.Nack(m.Attempt <= 1)
				return
			}

			if err := ack.Ack(); err != nil {
				log.Printf("[RAG] consumer: failed to ack for document %s: %v", m.DocumentID, err)
			}
		}()

		doc, err := uc.docRepo.FindByID(context.Background(), m.DocumentID)
		if err != nil {
			log.Printf("[RAG] consumer: document %s not found, discarding: %v", m.DocumentID, err)
			return
		}

		if doc.Status == rag.DocumentStatusReady {
			log.Printf("[RAG] consumer: document %s already ready, skipping", m.DocumentID)
			return
		}

		if err := uc.processor.Process(context.Background(), doc); err != nil {
			log.Printf("[RAG] consumer: processing failed for document %s (attempt %d/%d): %v",
				m.DocumentID, m.Attempt, rag.MaxProcessingAttempts, err)

			if m.Attempt < rag.MaxProcessingAttempts {
				uc.retryLater(m)
			} else {
				log.Printf("[RAG] consumer: document %s exhausted all %d attempts, marked as failed",
					m.DocumentID, rag.MaxProcessingAttempts)
			}
			return
		}
	}(msg)
}

func (uc *consumeDocumentProcessingUseCase) retryLater(msg rag.DocumentProcessingMessage) {
	retry := rag.DocumentProcessingMessage{
		DocumentID:      msg.DocumentID,
		KnowledgeBaseID: msg.KnowledgeBaseID,
		Attempt:         msg.Attempt + 1,
	}

	payload, err := json.Marshal(retry)
	if err != nil {
		log.Printf("[RAG] consumer: failed to marshal retry message for %s: %v", msg.DocumentID, err)
		return
	}

	if err := uc.queuePub.Publish(rag.DocumentProcessingTopic, payload); err != nil {
		log.Printf("[RAG] consumer: failed to publish retry for %s: %v", msg.DocumentID, err)
		return
	}

	log.Printf("[RAG] consumer: scheduled retry %d/%d for document %s",
		retry.Attempt, rag.MaxProcessingAttempts, msg.DocumentID)
}
