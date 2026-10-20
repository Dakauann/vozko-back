package container

import (
	"log"

	personalinbox_repository "vozko/infra/repositories/personalinbox"
	personalinbox_usecase "vozko/usecases/personalinbox"
)

func (c *Container) wirePersonalInbox() {
	service, err := personalinbox_usecase.New(personalinbox_usecase.Deps{
		Marks:     personalinbox_repository.New(c.db),
		Access:    c.services.conversationAuth,
		Announcer: c.services.conversationHub,
	})
	if err != nil {
		log.Fatalf("[container] %v", err)
	}
	c.services.personalInbox = service
	c.services.conversationHub.SetPersonalMarks(service)
}
