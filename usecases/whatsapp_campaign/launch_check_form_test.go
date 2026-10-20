package whatsapp_campaign_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/campaign/launchcheck"
	"vozko/domain/whatsapp/template"
	wf "vozko/domain/whatsappform"
)

type launchPublications struct {
	pub *wf.Publication
	err error
	got []string
}

func (l *launchPublications) FindByMetaFlow(_ context.Context, metaWABAID, metaFlowID string) (*wf.Publication, error) {
	l.got = append(l.got, metaWABAID+"|"+metaFlowID)
	return l.pub, l.err
}

func launchFormTemplate() *template.Template {
	tmpl := formCampaignTemplate()
	tmpl.ID, tmpl.ExternalID, tmpl.WABAId, tmpl.Category = "tpl-1", "x1", "waba-1", template.TemplateCategoryUtility
	return tmpl
}

func publishedFor(workspaceID string, status wf.PublicationStatus) *wf.Publication {
	return &wf.Publication{ID: "pub-1", WorkspaceID: workspaceID, MetaWABAID: "waba-1", MetaFlowID: "F1", Status: status}
}

func TestACampaignWhoseFormCannotBeSentIsBlocked(t *testing.T) {
	cases := []struct {
		name      string
		tmpl      *template.Template
		pubs      *launchPublications
		noPubs    bool
		wantBlock launchcheck.Code
	}{
		{name: "form published and sendable", tmpl: launchFormTemplate(), pubs: &launchPublications{pub: publishedFor("ws-1", wf.PublicationPublished)}},
		{name: "form throttled by Meta still sends", tmpl: launchFormTemplate(), pubs: &launchPublications{pub: publishedFor("ws-1", wf.PublicationThrottled)}},
		{name: "form blocked by Meta", tmpl: launchFormTemplate(), pubs: &launchPublications{pub: publishedFor("ws-1", wf.PublicationBlocked)}, wantBlock: launchcheck.CodeFormNotSendable},
		{name: "form no longer published", tmpl: launchFormTemplate(), pubs: &launchPublications{err: wf.ErrPublicationNotFound}, wantBlock: launchcheck.CodeFormNotSendable},
		{name: "form of another workspace", tmpl: launchFormTemplate(), pubs: &launchPublications{pub: publishedFor("ws-other", wf.PublicationPublished)}, wantBlock: launchcheck.CodeFormNotSendable},
		{name: "no publication reader", tmpl: launchFormTemplate(), noPubs: true, wantBlock: launchcheck.CodeFormNotSendable},
		{name: "publications unreadable", tmpl: launchFormTemplate(), pubs: &launchPublications{err: errors.New("db down")}, wantBlock: launchcheck.CodeHealthUnreadable},
		{name: "template without a form", tmpl: launchDeps().Templates.(launchTemplates)["tpl-1"], noPubs: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps := launchDeps()
			deps.Templates = launchTemplates{"tpl-1": tc.tmpl}
			if !tc.noPubs {
				deps.Forms = tc.pubs
			}

			report := runCheck(t, deps)

			if tc.wantBlock == "" {
				if len(report.Findings) != 0 {
					t.Fatalf("findings = %+v, want none", report.Findings)
				}
				return
			}
			if !found(report, launchcheck.Block, tc.wantBlock) {
				t.Fatalf("findings = %+v, want block %s", report.Findings, tc.wantBlock)
			}
			if tc.pubs != nil && (len(tc.pubs.got) != 1 || tc.pubs.got[0] != "waba-1|F1") {
				t.Fatalf("looked up %v, want the template's account and flow", tc.pubs.got)
			}
		})
	}
}
