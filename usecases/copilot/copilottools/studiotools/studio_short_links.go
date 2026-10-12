package studiotools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/shortlink"
	"vozko/domain/workspace"
)

const (
	studioShortLinksArg   = "short_links"
	studioShortLinkTitle  = "QR Code do Estúdio"
	studioLinksUnwired    = "os links curtos não estão disponíveis agora; use qr_link url para o endereço direto"
	studioLinksDenied     = "PERMISSÃO NEGADA: o usuário não pode %s links curtos neste workspace; use qr_link url para o endereço direto ou peça a permissão"
	studioLinkUnknown     = "short_link_id %q não existe neste workspace; mande qr_url para criar um link curto"
	studioLinkTargetWrong = "qr_url não serve para um link curto: %v"
	studioLinkFailed      = "o link curto não pôde ser criado agora; tente de novo ou use qr_link url"
)

type StudioShortLinks struct {
	Create      shortlink.CreateShortLinkUseCase
	Get         shortlink.GetShortLinkUseCase
	Delete      shortlink.DeleteShortLinkUseCase
	BaseURL     string
	Permissions workspace.CheckAccessUseCase
}

func (l StudioShortLinks) wired() bool {
	return l.Create != nil && l.Get != nil && l.Delete != nil && l.Permissions != nil && strings.TrimSpace(l.BaseURL) != ""
}

func (l StudioShortLinks) permit(cc copilot.Context, action workspace.Action, verb string) error {
	if cc.SystemAdmin {
		return nil
	}
	if err := l.Permissions.Execute(cc.UserID, cc.WorkspaceID, workspace.ResourceShortLinks, action); err != nil {
		return fmt.Errorf(studioLinksDenied, verb)
	}
	return nil
}

func qrShortLinkRequest(op string, a studioQRArgs) bool {
	if op != "add_qr_code" && op != "update_qr_code" {
		return false
	}
	return a.QRLink == qrLinkShort || (a.QRLink == "" && (a.QRURL != "" || a.ShortLinkID != ""))
}

func qrRequests(operations any) []*studioQRArgs {
	var found []*studioQRArgs
	switch ops := operations.(type) {
	case []studioImageOperation:
		for i := range ops {
			if qrShortLinkRequest(ops[i].Op, ops[i].studioQRArgs) {
				found = append(found, &ops[i].studioQRArgs)
			}
		}
	case []studioVideoOperation:
		for i := range ops {
			if qrShortLinkRequest(ops[i].Op, ops[i].studioQRArgs) {
				found = append(found, &ops[i].studioQRArgs)
			}
		}
	}
	return found
}

func (l StudioShortLinks) resolve(ctx context.Context, cc copilot.Context, requests []*studioQRArgs, create bool) (map[string]string, []string, error) {
	if len(requests) == 0 {
		return nil, nil, nil
	}
	if !l.wired() {
		return nil, nil, errors.New(studioLinksUnwired)
	}
	urls := map[string]string{}
	var pending []*studioQRArgs
	for _, request := range requests {
		if request.ShortLinkID != "" {
			url, err := l.existing(ctx, cc, request.ShortLinkID)
			if err != nil {
				return nil, nil, err
			}
			urls[request.ShortLinkID] = url
			continue
		}
		if err := l.creatable(cc, request.QRURL); err != nil {
			return nil, nil, err
		}
		pending = append(pending, request)
	}
	if !create {
		return urls, nil, nil
	}
	var created []string
	for _, request := range pending {
		link, err := l.created(ctx, cc, request.QRURL)
		if err != nil {
			l.discard(ctx, cc, created)
			return nil, nil, err
		}
		created = append(created, link.ID)
		urls[link.ID] = shortlink.ShortURL(l.BaseURL, link.Code)
		request.QRLink = qrLinkShort
		request.ShortLinkID = link.ID
		request.QRURL = ""
	}
	return urls, created, nil
}

func (l StudioShortLinks) discard(ctx context.Context, cc copilot.Context, ids []string) {
	for _, id := range ids {
		if err := l.Delete.Execute(context.WithoutCancel(ctx), cc.WorkspaceID, id); err != nil {
			log.Printf("[copilot] studio short link %s of a refused batch could not be removed: %v", id, err)
		}
	}
}

func (l StudioShortLinks) existing(ctx context.Context, cc copilot.Context, id string) (string, error) {
	if err := l.permit(cc, workspace.ActionRead, "ler"); err != nil {
		return "", err
	}
	link, err := l.Get.Execute(ctx, cc.WorkspaceID, id)
	if err != nil || link == nil {
		return "", fmt.Errorf(studioLinkUnknown, id)
	}
	return shortlink.ShortURL(l.BaseURL, link.Code), nil
}

func (l StudioShortLinks) creatable(cc copilot.Context, target string) error {
	if err := l.permit(cc, workspace.ActionCreate, "criar"); err != nil {
		return err
	}
	if err := shortlink.ValidateTargetURL(target); err != nil {
		return fmt.Errorf(studioLinkTargetWrong, err)
	}
	return nil
}

func (l StudioShortLinks) created(ctx context.Context, cc copilot.Context, target string) (*shortlink.ShortLink, error) {
	link, err := l.Create.Execute(ctx, shortlink.CreateShortLinkInput{WorkspaceID: cc.WorkspaceID, CreatedBy: cc.UserID, TargetURL: strings.TrimSpace(target), Title: studioShortLinkTitle})
	if err != nil {
		known := []error{shortlink.ErrTargetURLBlocked, shortlink.ErrTargetURLLoop, shortlink.ErrThreatDetected, shortlink.ErrMaxShortLinksReached}
		for _, reason := range known {
			if errors.Is(err, reason) {
				return nil, fmt.Errorf(studioLinkTargetWrong, err)
			}
		}
		log.Printf("[copilot] studio short link could not be created: %v", err)
		return nil, errors.New(studioLinkFailed)
	}
	return link, nil
}

func (t *studioEditTool) editorPayload(ctx context.Context, cc copilot.Context, operations any, create bool) (map[string]interface{}, []string, error) {
	links, created, err := t.deps.Links.resolve(ctx, cc, qrRequests(operations), create)
	if err != nil {
		return nil, nil, err
	}
	payload := map[string]interface{}{"operations": operations}
	if len(links) > 0 {
		payload[studioShortLinksArg] = links
	}
	return payload, created, nil
}
