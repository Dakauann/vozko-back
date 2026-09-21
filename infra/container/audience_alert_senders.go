package container

import (
	"context"
	"strings"

	businessphone "vozko/domain/whatsapp/business_phone"

	ca "vozko/domain/audience"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
)

type commentAlertSenderDirectory struct {
	instances         uw.InstanceRepository
	unofficialEnabled bool
	phones            businessphone.ListUseCase
}

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
		Options:     shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: 100}},
	})
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
