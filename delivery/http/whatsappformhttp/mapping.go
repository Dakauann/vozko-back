package whatsappformhttp

import (
	"vozko/domain/whatsappform"
	whatsappform_usecase "vozko/usecases/whatsappform"
)

func toOptions(o whatsappform_usecase.Options) OptionsResponse {
	out := OptionsResponse{
		FieldKinds: o.FieldKinds, DisplayKinds: o.DisplayKinds, Categories: o.Categories, Limits: o.Limits,
		Starters: o.Starters, FlowJSONVersion: o.FlowJSONVersion,
		Bindings: make([]BindingOptionResponse, 0, len(o.Bindings)), CustomFields: make([]CustomFieldOptionResponse, 0, len(o.CustomFields)),
	}
	for _, b := range o.Bindings {
		out.Bindings = append(out.Bindings, BindingOptionResponse{Target: b.Target, FieldKinds: b.FieldKinds})
	}
	for _, c := range o.CustomFields {
		out.CustomFields = append(out.CustomFields, CustomFieldOptionResponse{Key: c.Key, Label: c.Label, Type: string(c.Type), Options: c.Options, FieldKinds: c.FieldKinds})
	}
	return out
}

func toDetail(d whatsappform_usecase.FormDetail) FormDetailResponse {
	pubs := d.Publications
	if pubs == nil {
		pubs = []whatsappform.Publication{}
	}
	return FormDetailResponse{Form: d.Form, Draft: d.Draft, Published: d.Published, Publications: pubs}
}

func toPublish(r whatsappform_usecase.PublishResult) PublishResponse {
	out := PublishResponse{Outcomes: make([]OutcomeResponse, 0, len(r.Outcomes)), Form: toDetail(r.Detail)}
	for _, o := range r.Outcomes {
		out.Outcomes = append(out.Outcomes, OutcomeResponse{MetaWABAID: o.MetaWABAID, Status: o.Status, Problems: o.Problems, Error: o.Error, Code: o.Code})
	}
	return out
}

func toSendable(forms []whatsappform_usecase.SendableForm) SendableResponse {
	out := SendableResponse{Items: make([]SendableFormResponse, 0, len(forms))}
	for _, f := range forms {
		out.Items = append(out.Items, SendableFormResponse{FormID: f.FormID, Name: f.Name, VersionID: f.VersionID, MetaFlowID: f.MetaFlowID, FirstScreen: f.FirstScreen})
	}
	return out
}

func toResponses(page whatsappform.Page[whatsappform_usecase.ResponseRow]) ResponsesResponse {
	out := ResponsesResponse{Items: make([]ResponseRowResponse, 0, len(page.Items)), Next: page.Next}
	for _, row := range page.Items {
		out.Items = append(out.Items, ResponseRowResponse{Response: row.Response, LeadName: row.LeadName})
	}
	return out
}

func toResponseDetail(d whatsappform_usecase.ResponseDetail) ResponseDetailResponse {
	out := ResponseDetailResponse{Response: d.Response, Version: d.Version, LeadName: d.LeadName, Revealed: d.Revealed}
	if d.Form != nil {
		out.Form = &FormRefResponse{ID: d.Form.ID, Name: d.Form.Name}
	}
	return out
}

func toAutomations(items []whatsappform_usecase.FormAutomation) AutomationsResponse {
	out := AutomationsResponse{Items: make([]AutomationResponse, 0, len(items))}
	for _, a := range items {
		out.Items = append(out.Items, AutomationResponse{WorkflowID: a.WorkflowID, Name: a.Name, Active: a.Active, Use: a.Use})
	}
	return out
}

func orEmpty[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}
