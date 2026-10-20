package container

import (
	"time"

	ca "vozko/domain/commentautomation"
	"vozko/domain/privatereply"
	commentautomation_repository "vozko/infra/repositories/commentautomation"
	cauc "vozko/usecases/commentautomation"
)

type commentAutomationBundle struct {
	Rules      ca.Repository
	Replies    privatereply.Repository
	Executions ca.ExecutionClaims
	Evaluator  *cauc.Evaluator
	Sender     *cauc.PrivateReplySender
	Manager    *cauc.Manager
	Purge      *cauc.ExecutionPurge
}

func (c *Container) commentAutomation() *commentAutomationBundle {
	if c.commentAutomationShared == nil {
		rules := commentautomation_repository.NewRuleRepository(c.db)
		replies := commentautomation_repository.NewPrivateReplyRepository(c.db)
		executions := commentautomation_repository.NewExecutionClaimRepository(c.db)
		c.commentAutomationShared = &commentAutomationBundle{
			Rules:      rules,
			Replies:    replies,
			Executions: executions,
			Evaluator:  cauc.NewEvaluator(rules, executions),
			Sender:     cauc.NewPrivateReplySender(replies),
			Manager:    cauc.NewManager(rules, nil),
			Purge:      cauc.NewExecutionPurge(executions, func() time.Time { return time.Now().UTC() }),
		}
	}
	return c.commentAutomationShared
}
