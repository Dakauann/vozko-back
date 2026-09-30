package callrouting

import "errors"

var (
	ErrWorkspaceRequired      = errors.New("workspace is required")
	ErrQueueNameRequired      = errors.New("queue name is required (up to 80 characters)")
	ErrQueueMembersRequired   = errors.New("choose a department or the people who answer this queue")
	ErrQueueMembersAmbiguous  = errors.New("a queue takes its members from a department or from a list of people, not both")
	ErrInvalidStrategy        = errors.New("unknown queue strategy")
	ErrQueueTimingOutOfRange  = errors.New("queue timings are out of range")
	ErrHoldMusicAmbiguous     = errors.New("hold music is a preset or an uploaded audio, not both")
	ErrHoldMusicNotFound      = errors.New("hold music not found")
	ErrQueueNotFound          = errors.New("queue not found")
	ErrQueueDepartment        = errors.New("the department does not belong to this workspace")
	ErrQueueMemberOutside     = errors.New("every queue member must belong to this workspace")
	ErrCallNotFound           = errors.New("call not found")
	ErrNotCallOwner           = errors.New("only the operator on the call can transfer it")
	ErrInvalidTransferTarget  = errors.New("transfer to a queue or to a colleague")
	ErrTransferNotesTooLong   = errors.New("transfer notes are limited to 500 characters")
	ErrTargetUnavailable      = errors.New("the colleague is not available to take calls")
	ErrTransferToSelf         = errors.New("you cannot transfer a call to yourself")
	ErrTransferInProgress     = errors.New("this call is already being transferred")
	ErrNoTransferToCancel     = errors.New("there is no transfer to cancel")
	ErrInvalidStatsWindow     = errors.New("choose a period of up to 31 days")
	ErrTransferNotAllowed     = errors.New("you may not handle this kind of call")
	ErrConversationOutOfReach = errors.New("you cannot pass this conversation to that colleague")
)

var invalidInputErrors = []error{
	ErrWorkspaceRequired,
	ErrQueueNameRequired,
	ErrQueueMembersRequired,
	ErrQueueMembersAmbiguous,
	ErrInvalidStrategy,
	ErrQueueTimingOutOfRange,
	ErrHoldMusicAmbiguous,
	ErrHoldMusicNotFound,
	ErrQueueDepartment,
	ErrQueueMemberOutside,
	ErrInvalidTransferTarget,
	ErrTransferNotesTooLong,
	ErrTransferToSelf,
	ErrInvalidStatsWindow,
}

func IsInvalidInput(err error) bool {
	for _, target := range invalidInputErrors {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}
