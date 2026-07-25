package container

import (
	"log"

	"vozko/infra/crypto/pii"
	"vozko/infra/crypto/piigorm"
)

func (c *Container) initPII() {
	svc, err := pii.LoadFromEnv()
	if err != nil {
		log.Fatalf("Failed to initialize PII encryption service: %v", err)
	}
	piigorm.SetService(svc)
	log.Printf("PII encryption service initialised (active KEK v%d)", svc.ActiveKEKVersion())
}
