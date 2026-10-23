package container

import (
	"log"

	"vozko/infra/emailhtml"
	emailsending_repository "vozko/infra/repositories/emailsending"
	emailinbox_usecase "vozko/usecases/emailinbox"
)

func (c *Container) emailHTMLViewer() *emailinbox_usecase.HTMLViewer {
	if c.email == nil || c.email.Campaigns == nil || c.email.Campaigns.Campaigns == nil || c.services.fileReader == nil {
		log.Fatalf("[email-html] email campaigns and the file reader must exist before the email view")
	}
	viewer, err := emailinbox_usecase.NewHTMLViewer(emailinbox_usecase.HTMLViewDeps{
		Visibility: c.conversationVisibility(),
		Messages:   c.repositories.conversation,
		Media:      c.repositories.conversationMedia,
		Files:      c.services.fileReader,
		Sent:       emailsending_repository.NewMessageRepository(c.db),
		Originals:  c.email.Campaigns.Campaigns,
		Sanitizer:  emailhtml.New(),
	})
	if err != nil {
		log.Fatalf("[email-html] %v", err)
	}
	return viewer
}
