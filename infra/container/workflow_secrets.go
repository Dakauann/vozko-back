package container

import (
	"log"

	"vozko/domain/workflowsecret"
	"vozko/infra/crypto/piigorm"
	workflowsecret_repository "vozko/infra/repositories/workflowsecret"
	workflowsecret_usecase "vozko/usecases/workflowsecret"
)

func (c *Container) newWorkflowSecrets() workflowsecret.UseCases {
	cipher := piigorm.Service()
	if cipher == nil {
		log.Fatalf("[container] workflow secrets need the PII encryption service")
	}
	svc, err := workflowsecret_usecase.New(workflowsecret_usecase.Deps{
		Secrets: workflowsecret_repository.New(c.db),
		Cipher:  cipher,
	})
	if err != nil {
		log.Fatalf("[container] workflow secrets: %v", err)
	}
	return svc
}
