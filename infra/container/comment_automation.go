package container

import (
	ca "vozko/domain/commentautomation"
	"vozko/domain/privatereply"
	commentautomation_repository "vozko/infra/repositories/commentautomation"
	cauc "vozko/usecases/commentautomation"
)

type commentAutomationBundle struct {
	Rules     ca.Repository
	Replies   privatereply.Repository
	Evaluator *cauc.Evaluator
	Sender    *cauc.PrivateReplySender
	Manager   *cauc.Manager
}

func (c *Container) commentAutomation() *commentAutomationBundle {
	if c.commentAutomationShared == nil {
		rules := commentautomation_repository.NewRuleRepository(c.db)
		replies := commentautomation_repository.NewPrivateReplyRepository(c.db)
		c.commentAutomationShared = &commentAutomationBundle{
			Rules:     rules,
			Replies:   replies,
			Evaluator: cauc.NewEvaluator(rules),
			Sender:    cauc.NewPrivateReplySender(replies),
			Manager:   cauc.NewManager(rules, nil),
		}
	}
	return c.commentAutomationShared
}
