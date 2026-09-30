package container

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

func pageConversation(context.Context, string) (*analysisConversation, error) {
	agent := "agent-1"
	return &analysisConversation{
		ID: "conv-1", WorkspaceID: "ws-1", ContactID: "contact-1",
		Container: analysisContainer{ID: "page-1", Name: "Loja", Automation: conversation.ChannelAutomation{
			AgentID: &agent, EnableAnalysis: true, EnableAutoMemory: true,
		}},
	}, nil
}

func TestChannelAnalysisResolverNamesTheContactAndCarriesTheContainerSwitches(t *testing.T) {
	lead := "lead-1"
	resolve := channelAnalysisResolver(shared.EntryTypeFacebook, pageConversation,
		func(context.Context, string) (*analysisContact, error) {
			return &analysisContact{Label: "Ana", LeadID: &lead}, nil
		})

	subject, err := resolve(context.Background(), "conv-1")
	if err != nil {
		t.Fatal(err)
	}
	if subject.EntryType != shared.EntryTypeFacebook || subject.EntryID != "conv-1" || subject.WorkspaceID != "ws-1" {
		t.Fatalf("identity lost: %+v", subject)
	}
	if subject.ContactLabel != "Ana" || subject.LeadID != "lead-1" {
		t.Fatalf("contact lost: %+v", subject)
	}
	if subject.ContainerID != "page-1" || subject.ContainerName != "Loja" || subject.AgentID != "agent-1" {
		t.Fatalf("container lost: %+v", subject)
	}
	if !subject.EnableAnalysis || subject.EnableAutoStaging || !subject.EnableAutoMemory {
		t.Fatalf("switches lost: %+v", subject)
	}
}

func TestChannelAnalysisResolverFallsBackToTheContainerNameWithoutAContact(t *testing.T) {
	resolve := channelAnalysisResolver(shared.EntryTypeFacebook, pageConversation,
		func(context.Context, string) (*analysisContact, error) { return nil, errors.New("gone") })

	subject, err := resolve(context.Background(), "conv-1")
	if err != nil {
		t.Fatal(err)
	}
	if subject.ContactLabel != "Loja" || subject.LeadID != "" {
		t.Fatalf("got %+v", subject)
	}
}

func TestChannelAnalysisResolverPropagatesAConversationFailure(t *testing.T) {
	boom := errors.New("db down")
	resolve := channelAnalysisResolver(shared.EntryTypeFacebook,
		func(context.Context, string) (*analysisConversation, error) { return nil, boom },
		func(context.Context, string) (*analysisContact, error) {
			t.Fatal("contact read without a conversation")
			return nil, nil
		})

	if _, err := resolve(context.Background(), "conv-1"); !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
}
