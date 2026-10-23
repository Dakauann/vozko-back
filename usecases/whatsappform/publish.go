package whatsappform_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"time"

	businessphone "vozko/domain/whatsapp/business_phone"
	wf "vozko/domain/whatsappform"
)

const (
	stuckAfter = 15 * time.Minute
	stuckBatch = 100
)

type PublishOutcome struct {
	MetaWABAID string               `json:"metaWabaId"`
	Status     wf.PublicationStatus `json:"status,omitempty"`
	Problems   wf.Problems          `json:"problems,omitempty"`
	Error      string               `json:"error,omitempty"`
	Code       string               `json:"code,omitempty"`
}

type PublishResult struct {
	Outcomes []PublishOutcome `json:"outcomes"`
	Detail   FormDetail       `json:"form"`
}

type Preview struct {
	URL       string     `json:"previewUrl"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

type account struct {
	metaWABAID       string
	flow             wf.FlowAccount
	supported        bool
	businessVerified bool
}

func publishable(p *businessphone.WhatsAppBusinessPhoneNumber, workspaceID string) bool {
	return p != nil && p.BelongsToWorkspace(workspaceID) && p.IsVerified() && p.WABAId != ""
}

func (s *Service) accounts(ctx context.Context, workspaceID string) ([]account, error) {
	phones, err := s.deps.OwnedPhones.OwnedPhones(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("numbers of workspace %s: %w", workspaceID, err)
	}
	byWABA := map[string]account{}
	for _, p := range phones {
		if !publishable(p, workspaceID) {
			continue
		}
		current := byWABA[p.WABAId]
		current.metaWABAID = p.WABAId
		current.businessVerified = current.businessVerified || p.BusinessVerified()
		if !current.supported && p.Provider.Normalized() == businessphone.ProviderMeta && p.AccessToken != "" {
			current.supported, current.flow = true, wf.FlowAccount{MetaWABAID: p.WABAId, AccessToken: p.AccessToken}
		}
		byWABA[p.WABAId] = current
	}
	out := make([]account, 0, len(byWABA))
	for _, a := range byWABA {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].metaWABAID < out[j].metaWABAID })
	return out, nil
}

func (s *Service) accountFor(ctx context.Context, workspaceID, metaWABAID string) (account, error) {
	accounts, err := s.accounts(ctx, workspaceID)
	if err != nil {
		return account{}, err
	}
	return pick(accounts, metaWABAID)
}

func pick(accounts []account, metaWABAID string) (account, error) {
	for _, a := range accounts {
		if a.metaWABAID == metaWABAID {
			if !a.supported {
				return account{}, wf.ErrProviderUnsupported
			}
			return a, nil
		}
	}
	return account{}, wf.ErrNotOwner
}

type publishing struct {
	form    *wf.Form
	version wf.Version
	sources wf.SourceMap
}

func (s *Service) prepared(ctx context.Context, form *wf.Form, versionID string) (publishing, error) {
	version, err := s.deps.Versions.Get(ctx, form.WorkspaceID, versionID)
	if err != nil {
		return publishing{}, err
	}
	problems, assets, err := s.problemsOf(ctx, form.WorkspaceID, version.Document)
	if err != nil {
		return publishing{}, err
	}
	if len(problems) > 0 {
		return publishing{}, problems
	}
	if !version.Compiled() {
		return publishing{}, version.Problems.Err()
	}
	compiled, err := wf.Compile(version.Document, assets)
	if err != nil {
		return publishing{}, err
	}
	return publishing{form: form, version: *version, sources: compiled.SourceMap}, nil
}

func (s *Service) chosen(ctx context.Context, workspaceID string, metaWABAIDs []string) ([]account, error) {
	accounts, err := s.accounts(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, wf.ErrNoEligibleAccount
	}
	out := make([]account, 0, len(metaWABAIDs))
	seen := map[string]bool{}
	for _, id := range metaWABAIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		acct, err := pick(accounts, id)
		if err != nil {
			return nil, err
		}
		out = append(out, acct)
	}
	if len(out) == 0 {
		return nil, wf.ErrNoAccountChosen
	}
	return out, nil
}

func (s *Service) Publish(ctx context.Context, a Actor, formID string, metaWABAIDs []string) (PublishResult, error) {
	if err := s.require(a, wf.CapabilityView, wf.CapabilityPublish); err != nil {
		return PublishResult{}, err
	}
	form, err := s.activeForm(ctx, a.WorkspaceID, formID)
	if err != nil {
		return PublishResult{}, err
	}
	accounts, err := s.chosen(ctx, a.WorkspaceID, metaWABAIDs)
	if err != nil {
		return PublishResult{}, err
	}
	job, err := s.prepared(ctx, form, form.DraftVersionID)
	if err != nil {
		return PublishResult{}, err
	}
	result := PublishResult{}
	for _, acct := range accounts {
		result.Outcomes = append(result.Outcomes, s.publishOn(ctx, job, acct))
	}
	if err := s.rememberPublished(ctx, job, result.Outcomes); err != nil {
		return PublishResult{}, err
	}
	detail, err := s.Get(ctx, a, formID)
	if err != nil {
		return PublishResult{}, err
	}
	result.Detail = detail
	return result, nil
}

func (s *Service) rememberPublished(ctx context.Context, job publishing, outcomes []PublishOutcome) error {
	for _, o := range outcomes {
		if o.Status == wf.PublicationPublished && o.Error == "" && len(o.Problems) == 0 {
			return s.deps.Forms.SetPublished(ctx, job.form.WorkspaceID, job.form.ID, job.version.ID, s.now())
		}
	}
	return nil
}

func failure(acct account, status wf.PublicationStatus, err error) PublishOutcome {
	return PublishOutcome{MetaWABAID: acct.metaWABAID, Status: status, Error: err.Error(), Code: wf.ErrorCode(err)}
}

func (s *Service) claim(ctx context.Context, job publishing, acct account) (wf.Publication, bool, error) {
	existing, err := s.deps.Publications.Find(ctx, job.form.WorkspaceID, job.form.ID, acct.metaWABAID)
	switch {
	case err == nil:
		return *existing, false, nil
	case !errors.Is(err, wf.ErrPublicationNotFound):
		return wf.Publication{}, false, err
	}
	now := s.now()
	pub := wf.Publication{
		ID: s.deps.NewID(), WorkspaceID: job.form.WorkspaceID, FormID: job.form.ID, MetaWABAID: acct.metaWABAID,
		MetaName: wf.MetaFlowName(job.form.Name, job.form.ID), VersionID: job.version.ID, Status: wf.PublicationCreating,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.deps.Publications.Insert(ctx, pub); err != nil {
		again, findErr := s.deps.Publications.Find(ctx, job.form.WorkspaceID, job.form.ID, acct.metaWABAID)
		if findErr != nil {
			return wf.Publication{}, false, err
		}
		return *again, false, nil
	}
	return pub, true, nil
}

func (s *Service) save(ctx context.Context, pub *wf.Publication) error {
	pub.UpdatedAt = s.now()
	return s.deps.Publications.Save(ctx, *pub)
}

func (s *Service) publishOn(ctx context.Context, job publishing, acct account) PublishOutcome {
	pub, fresh, err := s.claim(ctx, job, acct)
	if err != nil {
		return failure(acct, "", err)
	}
	switch pub.Status {
	case wf.PublicationBlocked:
		return failure(acct, pub.Status, wf.ErrPublicationNotSendable)
	case wf.PublicationDeprecated:
		pub.MetaFlowID, pub.Status = "", wf.PublicationCreating
		pub.MetaName = wf.RepublishedFlowName(job.form.Name, job.form.ID, job.version.Number)
		if err := s.save(ctx, &pub); err != nil {
			return failure(acct, "", err)
		}
	}
	if pub.MetaFlowID == "" && !fresh {
		remote, err := s.deps.Gateway.FindByName(ctx, acct.flow, pub.MetaName)
		if err != nil {
			return failure(acct, pub.Status, err)
		}
		if remote != nil {
			pub.MetaFlowID = remote.ID
		}
	}
	if pub.MetaFlowID == "" {
		return s.createFlow(ctx, job, acct, pub)
	}
	return s.updateFlow(ctx, job, acct, pub)
}

func (s *Service) rejected(ctx context.Context, job publishing, acct account, pub wf.Publication, keep bool, err error) PublishOutcome {
	var rejection *wf.MetaRejection
	if !errors.As(err, &rejection) {
		return failure(acct, pub.Status, err)
	}
	pub.LastError = rejection.Message
	if len(rejection.Problems) > 0 {
		pub.Problems = job.sources.Problems(rejection.Problems)
	}
	if !keep {
		pub.Status = wf.PublicationFailed
	}
	outcome := failure(acct, pub.Status, err)
	outcome.Problems = pub.Problems
	if saveErr := s.save(ctx, &pub); saveErr != nil {
		return failure(acct, pub.Status, saveErr)
	}
	return outcome
}

func (s *Service) createFlow(ctx context.Context, job publishing, acct account, pub wf.Publication) PublishOutcome {
	if pub.Status != wf.PublicationCreating {
		pub.Status = wf.PublicationCreating
		if err := s.save(ctx, &pub); err != nil {
			return failure(acct, "", err)
		}
	}
	remote, err := s.deps.Gateway.Create(ctx, acct.flow, wf.CreateFlowInput{
		Name: pub.MetaName, Categories: job.version.Document.Categories, JSON: job.version.FlowJSON, Publish: true,
	})
	if err != nil {
		return s.rejected(ctx, job, acct, pub, false, err)
	}
	pub.MetaFlowID = remote.ID
	if len(remote.ValidationErrors) > 0 {
		pub.Status, pub.Problems = wf.PublicationFailed, job.sources.Problems(remote.ValidationErrors)
		if err := s.save(ctx, &pub); err != nil {
			return failure(acct, pub.Status, err)
		}
		return PublishOutcome{MetaWABAID: acct.metaWABAID, Status: pub.Status, Problems: pub.Problems, Code: wf.ErrorCode(wf.ErrDocumentInvalid)}
	}
	status, err := s.remoteStatus(ctx, acct, remote)
	if err != nil {
		if saveErr := s.save(ctx, &pub); saveErr != nil {
			return failure(acct, pub.Status, saveErr)
		}
		return failure(acct, pub.Status, err)
	}
	if status != wf.PublicationPublished {
		return s.publishFlow(ctx, job, acct, pub)
	}
	return s.published(ctx, job, acct, pub)
}

func (s *Service) remoteStatus(ctx context.Context, acct account, remote wf.RemoteFlow) (wf.PublicationStatus, error) {
	if remote.Status != "" {
		return wf.PublicationStatusFromMeta(remote.Status), nil
	}
	read, err := s.deps.Gateway.Get(ctx, acct.flow, remote.ID)
	if err != nil {
		return "", err
	}
	return wf.PublicationStatusFromMeta(read.Status), nil
}

func (s *Service) updateFlow(ctx context.Context, job publishing, acct account, pub wf.Publication) PublishOutcome {
	live := pub.Status == wf.PublicationPublished || pub.Status == wf.PublicationThrottled
	validation, err := s.deps.Gateway.UploadJSON(ctx, acct.flow, pub.MetaFlowID, job.version.FlowJSON)
	if err != nil {
		return s.rejected(ctx, job, acct, pub, live, err)
	}
	if len(validation) > 0 {
		pub.Problems = job.sources.Problems(validation)
		if !live {
			pub.Status = wf.PublicationFailed
		}
		if err := s.save(ctx, &pub); err != nil {
			return failure(acct, pub.Status, err)
		}
		return PublishOutcome{MetaWABAID: acct.metaWABAID, Status: pub.Status, Problems: pub.Problems, Code: wf.ErrorCode(wf.ErrDocumentInvalid)}
	}
	return s.publishFlow(ctx, job, acct, pub)
}

func (s *Service) publishFlow(ctx context.Context, job publishing, acct account, pub wf.Publication) PublishOutcome {
	live := pub.Status == wf.PublicationPublished || pub.Status == wf.PublicationThrottled
	if err := s.deps.Gateway.Publish(ctx, acct.flow, pub.MetaFlowID); err != nil {
		return s.rejected(ctx, job, acct, pub, live, err)
	}
	return s.published(ctx, job, acct, pub)
}

func (s *Service) published(ctx context.Context, job publishing, acct account, pub wf.Publication) PublishOutcome {
	now := s.now()
	if pub.Status != wf.PublicationThrottled {
		pub.Status = wf.PublicationPublished
	}
	pub.VersionID, pub.PublishedAt, pub.Problems, pub.LastError = job.version.ID, &now, nil, ""
	if err := s.save(ctx, &pub); err != nil {
		return failure(acct, pub.Status, err)
	}
	return PublishOutcome{MetaWABAID: acct.metaWABAID, Status: wf.PublicationPublished}
}

func (s *Service) Deprecate(ctx context.Context, a Actor, formID, metaWABAID string) error {
	if err := s.require(a, wf.CapabilityView, wf.CapabilityPublish); err != nil {
		return err
	}
	if _, err := s.deps.Forms.Get(ctx, a.WorkspaceID, formID); err != nil {
		return err
	}
	pub, err := s.deps.Publications.Find(ctx, a.WorkspaceID, formID, metaWABAID)
	if err != nil {
		return err
	}
	return s.deprecate(ctx, pub)
}

func (s *Service) deprecate(ctx context.Context, pub *wf.Publication) error {
	if pub.Status == wf.PublicationDeprecated {
		return nil
	}
	if pub.MetaFlowID != "" {
		acct, err := s.accountFor(ctx, pub.WorkspaceID, pub.MetaWABAID)
		if err != nil {
			return err
		}
		if err := s.deps.Gateway.Deprecate(ctx, acct.flow, pub.MetaFlowID); err != nil {
			return err
		}
	}
	pub.Status = wf.PublicationDeprecated
	return s.save(ctx, pub)
}

func (s *Service) Archive(ctx context.Context, a Actor, formID string) error {
	if err := s.require(a, wf.CapabilityView, wf.CapabilityDelete); err != nil {
		return err
	}
	form, err := s.deps.Forms.Get(ctx, a.WorkspaceID, formID)
	if err != nil {
		return err
	}
	if form.ArchivedAt != nil {
		return nil
	}
	pubs, err := s.deps.Publications.ListForForm(ctx, a.WorkspaceID, formID)
	if err != nil {
		return err
	}
	for i := range pubs {
		if err := s.deprecate(ctx, &pubs[i]); err != nil {
			return fmt.Errorf("form %s is still served on %s: %w", formID, pubs[i].MetaWABAID, err)
		}
	}
	return s.deps.Forms.Archive(ctx, a.WorkspaceID, formID, s.now())
}

func (s *Service) Preview(ctx context.Context, a Actor, formID string) (Preview, error) {
	if err := s.require(a, wf.CapabilityView, wf.CapabilityBuild); err != nil {
		return Preview{}, err
	}
	form, err := s.activeForm(ctx, a.WorkspaceID, formID)
	if err != nil {
		return Preview{}, err
	}
	job, err := s.prepared(ctx, form, form.DraftVersionID)
	if err != nil {
		return Preview{}, err
	}
	accounts, err := s.accounts(ctx, a.WorkspaceID)
	if err != nil {
		return Preview{}, err
	}
	for _, acct := range accounts {
		if acct.supported {
			return s.previewOn(ctx, job, acct)
		}
	}
	return Preview{}, wf.ErrNoEligibleAccount
}

func (s *Service) previewOn(ctx context.Context, job publishing, acct account) (Preview, error) {
	name := wf.PreviewFlowName(job.form.ID)
	remote, err := s.deps.Gateway.FindByName(ctx, acct.flow, name)
	if err != nil {
		return Preview{}, err
	}
	var validation []wf.MetaValidationError
	var flowID string
	if remote == nil {
		created, err := s.deps.Gateway.Create(ctx, acct.flow, wf.CreateFlowInput{Name: name, Categories: job.version.Document.Categories, JSON: job.version.FlowJSON})
		if err != nil {
			return Preview{}, err
		}
		flowID, validation = created.ID, created.ValidationErrors
	} else {
		flowID = remote.ID
		if validation, err = s.deps.Gateway.UploadJSON(ctx, acct.flow, flowID, job.version.FlowJSON); err != nil {
			return Preview{}, err
		}
	}
	if len(validation) > 0 {
		return Preview{}, job.sources.Problems(validation)
	}
	shown, err := s.deps.Gateway.Preview(ctx, acct.flow, flowID, true)
	if err != nil {
		return Preview{}, err
	}
	return Preview{URL: shown.PreviewURL, ExpiresAt: shown.PreviewExpiresAt}, nil
}

func (s *Service) RecoverStuck(ctx context.Context) error {
	stuck, err := s.deps.Publications.ListStuck(ctx, s.now().Add(-stuckAfter), stuckBatch)
	if err != nil {
		return err
	}
	var failures []error
	for _, pub := range stuck {
		if err := s.recover(ctx, pub); err != nil {
			log.Printf("[whatsapp-forms] publication %s is still not resolved: %v", pub.ID, err)
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (s *Service) recover(ctx context.Context, pub wf.Publication) error {
	form, err := s.deps.Forms.Get(ctx, pub.WorkspaceID, pub.FormID)
	if err != nil {
		return err
	}
	job, err := s.prepared(ctx, form, pub.VersionID)
	if err != nil {
		return err
	}
	acct, err := s.accountFor(ctx, pub.WorkspaceID, pub.MetaWABAID)
	if err != nil {
		return err
	}
	outcome := s.publishOn(ctx, job, acct)
	if outcome.Error != "" {
		return errors.New(outcome.Error)
	}
	if outcome.Status == wf.PublicationPublished {
		return s.rememberPublished(ctx, job, []PublishOutcome{outcome})
	}
	return nil
}

type PublishTarget struct {
	MetaWABAID       string `json:"metaWabaId"`
	Supported        bool   `json:"supported"`
	BusinessVerified bool   `json:"businessVerified"`
}

func (s *Service) PublishTargets(ctx context.Context, a Actor) ([]PublishTarget, error) {
	if err := s.require(a, wf.CapabilityView, wf.CapabilityPublish); err != nil {
		return nil, err
	}
	accounts, err := s.accounts(ctx, a.WorkspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]PublishTarget, 0, len(accounts))
	for _, acct := range accounts {
		out = append(out, PublishTarget{MetaWABAID: acct.metaWABAID, Supported: acct.supported, BusinessVerified: acct.businessVerified})
	}
	return out, nil
}
