package container

import (
	"log"

	"vozko/delivery/http/conversationnotehttp"
	conversationnote_repository "vozko/infra/repositories/conversationnote"
	conversationnote_usecase "vozko/usecases/conversationnote"
)

func (c *Container) conversationNoteHandler() *conversationnotehttp.Handler {
	service, err := conversationnote_usecase.New(conversationnote_usecase.Deps{
		Notes:     conversationnote_repository.New(c.db),
		Access:    c.services.conversationAuth,
		People:    c.repositories.user,
		Announcer: c.services.conversationHub,
		Notifier:  c.notifications().Publisher,
		Contacts:  c.services.conversationHistory,
	})
	if err != nil {
		log.Fatalf("[container] %v", err)
	}
	return conversationnotehttp.NewHandler(service)
}
