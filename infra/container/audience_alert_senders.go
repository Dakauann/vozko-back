package container

import (
	"context"
	"strings"

	businessphone "vozko/domain/whatsapp/business_phone"

	ca "vozko/domain/audience"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
)

// commentAlertSenderDirectory answers "can this workspace send an alert on this
// channel, and from which number".
//
// The composition root is the only place that knows an alert channel maps to an
// unofficial instance or to a business phone, exactly as commentAlertDispatcher
// is the only place that knows it maps to two outbound use cases. The domain
// holds the port and nothing else.
//
// Both halves are optional. A nil lister means the channel is not configured in
// this deployment, which is reported as such rather than silently omitted: an
// operator being told "this channel is off here" can stop looking, while a
// channel that just is not in the list looks like a bug.
type commentAlertSenderDirectory struct {
	instances uw.InstanceRepository
	// unofficialEnabled is the deployment switch, separate from whether the
	// workspace has connected anything.
	unofficialEnabled bool
	phones            businessphone.ListUseCase
}

// sendableInstanceStatuses are the states a number can actually send from.
//
// AWAITING_SCAN and PROVISIONING are not among them: a session that has never
// completed cannot open a conversation, and treating it as a sender is how the
// rule ends up armed against a number that has never worked. HIBERNATED is
// included because the channel wakes it on demand.
var sendableInstanceStatuses = map[uw.Status]bool{
	uw.StatusConnected:  true,
	uw.StatusHibernated: true,
}

func (d commentAlertSenderDirectory) ChannelStatus(ctx context.Context, workspaceID string) ([]ca.AlertChannelStatus, error) {
	return []ca.AlertChannelStatus{
		d.officialStatus(workspaceID),
		d.unofficialStatus(ctx, workspaceID),
	}, nil
}

func (d commentAlertSenderDirectory) unofficialStatus(ctx context.Context, workspaceID string) ca.AlertChannelStatus {
	out := ca.AlertChannelStatus{
		Channel: ca.AlertChannelUnofficial,
		Senders: []ca.AlertSender{},
	}
	if !d.unofficialEnabled || d.instances == nil {
		out.Reason = ca.AlertChannelReasonNotEnabled
		return out
	}

	page, err := d.instances.ListByWorkspace(ctx, uw.ListInstancesInput{
		WorkspaceID: workspaceID,
		// Unrestricted scope on purpose: this asks what the WORKSPACE can send
		// from, not what the current operator may manage. Narrowing it here
		// would hide a perfectly good number from the person writing the rule.
		Options: shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: 100}},
	})
	// A read failure is not a verdict. Reporting "no numbers" because the table
	// was briefly unreachable would tell the operator to go connect one they
	// already have.
	if err != nil || page == nil {
		out.Available = true
		return out
	}
	for _, inst := range page.Items {
		if inst == nil || !sendableInstanceStatuses[inst.Status] {
			continue
		}
		out.Senders = append(out.Senders, ca.AlertSender{
			ID:    inst.ID,
			Label: instanceLabel(inst),
		})
	}
	out.Available = len(out.Senders) > 0
	if !out.Available {
		out.Reason = ca.AlertChannelReasonNoSender
	}
	return out
}

func (d commentAlertSenderDirectory) officialStatus(workspaceID string) ca.AlertChannelStatus {
	out := ca.AlertChannelStatus{
		Channel: ca.AlertChannelOfficial,
		Senders: []ca.AlertSender{},
	}
	if d.phones == nil {
		out.Reason = ca.AlertChannelReasonNotEnabled
		return out
	}
	page, err := d.phones.Execute(businessphone.ListInput{
		OwnerWorkspaceID: workspaceID,
		Options:          shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: 100}},
	})
	if err != nil || page == nil {
		out.Available = true
		return out
	}
	for _, p := range page.Items {
		if p == nil {
			continue
		}
		out.Senders = append(out.Senders, ca.AlertSender{
			ID:    p.ID,
			Label: phoneLabel(p),
		})
	}
	out.Available = len(out.Senders) > 0
	if !out.Available {
		out.Reason = ca.AlertChannelReasonNoSender
	}
	return out
}

// instanceLabel is what the operator will recognise in a picker, never an id.
func instanceLabel(inst *uw.Instance) string {
	for _, candidate := range []string{inst.DisplayName, inst.ProfileName, inst.PhoneNumber, inst.ProviderName} {
		if s := strings.TrimSpace(candidate); s != "" {
			return s
		}
	}
	return inst.ID
}

func phoneLabel(p *businessphone.WhatsAppBusinessPhoneNumber) string {
	number := strings.TrimSpace(p.DisplayPhoneNumber)
	name := strings.TrimSpace(p.VerifiedName)
	switch {
	case name != "" && number != "":
		return name + " · " + number
	case number != "":
		return number
	case name != "":
		return name
	}
	return p.ID
}
