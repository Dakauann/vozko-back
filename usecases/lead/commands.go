package lead_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/actor"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/recordevent"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/workspace"
)

var errCommandsIncomplete = errors.New("lead commands: a required dependency is missing")

type Actor = conversation.Viewer

type Permissions interface {
	HasWorkspacePermission(userID, workspaceID, resource, action string, isSystemAdmin bool) bool
}

type MemberVisibility interface {
	CanView(callerUserID, targetUserID, workspaceID string, isPlatformAdmin bool) (bool, error)
}

type PhoneDirectory interface {
	FindByID(id string) (*businessphone.WhatsAppBusinessPhoneNumber, error)
}

type MetaBlocking interface {
	BlockUser(phoneNumberID string, userNumber string, accessToken string) error
	UnblockUser(phoneNumberID string, userNumber string, accessToken string) error
}

type CommandDeps struct {
	Store       lead.Store
	Permissions Permissions
	Owners      actor.OwnerDirectory
	Visibility  MemberVisibility
	Notifier    lead.ChangeNotifier
	Phones      PhoneDirectory
	PhoneGrants businessphone.AccessGrantReader
	Meta        MetaBlocking
	Entries     lead.EntryUsage
	Relations   lead.RelationStore
	Duplicates  lead.DuplicateFinder
	Definitions DefinitionSource
	Anonymizer  lead.Anonymizer
	Now         func() time.Time
	NewID       func() string
}

type Commands struct {
	deps    CommandDeps
	writer  recordWriter
	viewers viewers
	blocker *WhatsAppBlocker
	reach   *OwnerReach
}

func NewCommands(deps CommandDeps) (*Commands, error) {
	missing := map[string]bool{
		"store":        deps.Store == nil,
		"permissions":  deps.Permissions == nil,
		"owners":       deps.Owners == nil,
		"visibility":   deps.Visibility == nil,
		"notifier":     deps.Notifier == nil,
		"phones":       deps.Phones == nil,
		"phone grants": deps.PhoneGrants == nil,
		"meta":         deps.Meta == nil,
		"entries":      deps.Entries == nil,
		"relations":    deps.Relations == nil,
		"duplicates":   deps.Duplicates == nil,
		"definitions":  deps.Definitions == nil,
		"anonymizer":   deps.Anonymizer == nil,
	}
	for name, absent := range missing {
		if absent {
			return nil, fmt.Errorf("%w: %s", errCommandsIncomplete, name)
		}
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.NewID == nil {
		deps.NewID = uuid.NewString
	}
	blocker, err := NewWhatsAppBlocker(deps.Phones, deps.PhoneGrants, deps.Meta)
	if err != nil {
		return nil, err
	}
	reach, err := NewOwnerReach(deps.Owners, deps.Visibility)
	if err != nil {
		return nil, err
	}
	return &Commands{
		deps:    deps,
		writer:  recordWriter{store: deps.Store, notifier: deps.Notifier},
		viewers: viewers{permissions: deps.Permissions, definitions: deps.Definitions},
		blocker: blocker,
		reach:   reach,
	}, nil
}

type BlockInput struct {
	Blocked         bool
	BusinessPhoneID string
}

type BlockResult struct {
	Lead        *lead.Lead
	MetaApplied bool
}

func (c *Commands) Create(ctx context.Context, a Actor, d lead.Draft) (CreateResult, error) {
	v, err := c.authorize(a, workspace.ActionCreate)
	if err != nil {
		return CreateResult{}, err
	}
	if d.SetsPins() {
		if err := c.permit(a, workspace.ActionUpdate, workspace.ActionReadAddresses); err != nil {
			return CreateResult{}, err
		}
	}
	created, err := c.draft(a.WorkspaceID, v, d)
	if err != nil {
		return CreateResult{}, err
	}
	duplicates, err := c.duplicatesOf(ctx, v, created, "")
	if err != nil {
		return CreateResult{}, err
	}
	event := lead.Changes(lead.EventCreated, a.UserID, nil, created, v.Definitions)
	if err := c.deps.Store.Insert(ctx, created, []recordevent.Event{event}); err != nil {
		return CreateResult{}, err
	}
	c.writer.notify(created, event.Fields())
	return CreateResult{Lead: created.VisibleTo(v, event.Fields()), Duplicates: duplicates}, nil
}

func (c *Commands) draft(workspaceID string, v lead.Viewer, d lead.Draft) (*lead.Lead, error) {
	values := d.CustomFields
	d.CustomFields = nil
	created, err := lead.New(workspaceID, d, c.now())
	if err != nil {
		return nil, err
	}
	if err := created.SetCustomFields(values, v.Definitions, v.Fields); err != nil {
		return nil, err
	}
	return created, nil
}

func (c *Commands) Update(ctx context.Context, a Actor, id string, expected *int64, e lead.Edit) (*lead.Lead, error) {
	return c.edit(ctx, a, id, expected, e, lead.EventUpdated)
}

func (c *Commands) Rename(ctx context.Context, a Actor, id string, expected *int64, name string) (*lead.Lead, error) {
	return c.edit(ctx, a, id, expected, lead.Edit{Name: &name}, lead.EventRenamed)
}

func (c *Commands) ChangeIdentity(ctx context.Context, a Actor, id string, expected *int64, number string) (*lead.Lead, error) {
	return c.edit(ctx, a, id, expected, lead.Edit{Number: &number}, lead.EventUpdated)
}

func (c *Commands) edit(ctx context.Context, a Actor, id string, expected *int64, e lead.Edit, kind recordevent.Kind) (*lead.Lead, error) {
	v, err := c.authorize(a, workspace.ActionUpdate)
	if err != nil {
		return nil, err
	}
	if e.Addresses != nil && !v.ReadsAddresses {
		return nil, lead.ErrAddressesForbidden
	}
	sent := e.Fields()
	current, err := c.writer.load(ctx, a.WorkspaceID, id)
	if err != nil {
		return nil, err
	}
	if err := shared.ExpectVersion(current.Version, expected); err != nil {
		if errors.Is(err, shared.ErrVersionConflict) {
			return nil, &lead.VersionConflict{Current: current.VisibleTo(v, sent)}
		}
		return nil, err
	}

	next := *current
	if e.Number != nil {
		if err := c.changeIdentity(ctx, &next, *e.Number); err != nil {
			return nil, err
		}
		e.Number = nil
	}
	if e.CustomFields != nil {
		if err := next.SetCustomFields(e.CustomFields, v.Definitions, v.Fields); err != nil {
			return nil, err
		}
		e.CustomFields = nil
	}
	if err := next.ApplyEdit(e, c.now()); err != nil {
		return nil, err
	}
	event := lead.Changes(kind, a.UserID, current, &next, v.Definitions)
	if event.Empty() {
		return current.VisibleTo(v, sent), nil
	}
	if err := c.deps.Store.Save(ctx, &next, current.Version, []recordevent.Event{event}); err != nil {
		if errors.Is(err, shared.ErrVersionConflict) {
			return nil, c.conflict(ctx, a, v, id, sent)
		}
		return nil, err
	}
	c.writer.notify(&next, event.Fields())
	return next.VisibleTo(v, sent), nil
}

func (c *Commands) changeIdentity(ctx context.Context, l *lead.Lead, number string) error {
	if !l.WouldChangeIdentity(number) {
		return nil
	}
	inUse, err := c.deps.Entries.HasEntries(ctx, l.WorkspaceID, l.ID)
	if err != nil {
		return fmt.Errorf("conversations of lead %s: %w", l.ID, err)
	}
	_, err = l.ChangeIdentity(number, inUse)
	return err
}

func (c *Commands) conflict(ctx context.Context, a Actor, v lead.Viewer, id string, sent []string) error {
	return c.writer.conflict(ctx, a.WorkspaceID, id, v, sent)
}

func (c *Commands) Block(ctx context.Context, a Actor, id string, in BlockInput) (BlockResult, error) {
	v, err := c.authorize(a, workspace.ActionBlock)
	if err != nil {
		return BlockResult{}, err
	}
	kind := lead.EventUnblocked
	if in.Blocked {
		kind = lead.EventBlocked
	}
	sent := []string{lead.FieldBlocked}
	blocked, err := c.targeted(ctx, a, v, id, kind, sent, func(l *lead.Lead) (bool, error) {
		if in.Blocked {
			return l.Block(a.UserID, c.now()), nil
		}
		return l.Unblock(), nil
	})
	if err != nil {
		return BlockResult{}, err
	}
	applied := c.applyWhatsAppBlock(a.WorkspaceID, in.BusinessPhoneID, blocked.Number, in.Blocked)
	return BlockResult{Lead: blocked.VisibleTo(v, sent), MetaApplied: applied}, nil
}

func (c *Commands) SetOwner(ctx context.Context, a Actor, id, owner string) (*lead.Lead, error) {
	v, err := c.authorize(a, workspace.ActionAssign)
	if err != nil {
		return nil, err
	}
	owner = strings.TrimSpace(owner)
	if err := c.reach.CheckOwner(a, owner); err != nil {
		return nil, err
	}
	sent := []string{lead.FieldOwner}
	owned, err := c.targeted(ctx, a, v, id, lead.EventOwnerChange, sent, func(l *lead.Lead) (bool, error) {
		return l.SetOwner(owner)
	})
	if err != nil {
		return nil, err
	}
	return owned.VisibleTo(v, sent), nil
}

func (c *Commands) CheckOwner(a Actor, owner string) error {
	return c.reach.CheckOwner(a, owner)
}

func (c *Commands) OptOut(ctx context.Context, a Actor, id string, source lead.OptOutSource) (*lead.Lead, error) {
	v, err := c.authorize(a, workspace.ActionUpdate)
	if err != nil {
		return nil, err
	}
	source = source.OrDefault()
	if !source.Valid() {
		return nil, lead.ErrLeadOptOutSourceInvalid
	}
	sent := []string{lead.FieldOptedOut, lead.FieldWhatsAppOptIn}
	opted, err := c.targeted(ctx, a, v, id, lead.EventOptedOut, sent, func(l *lead.Lead) (bool, error) {
		return l.OptOut(c.now(), source)
	})
	if err != nil {
		return nil, err
	}
	return opted.VisibleTo(v, sent), nil
}

func (c *Commands) SetArea(ctx context.Context, a Actor, id string, area lead.Area) (*lead.Lead, error) {
	v, err := c.authorize(a, workspace.ActionUpdate)
	if err != nil {
		return nil, err
	}
	sent := []string{lead.FieldAddresses}
	placed, err := c.targeted(ctx, a, v, id, lead.EventUpdated, sent, func(l *lead.Lead) (bool, error) {
		return l.SetPrimaryArea(area)
	})
	if err != nil {
		return nil, err
	}
	return placed.VisibleTo(v, sent), nil
}

func (c *Commands) Anonymize(ctx context.Context, a Actor, id string) (lead.Erasure, error) {
	if err := c.permit(a, workspace.ActionAnonymize); err != nil {
		return lead.Erasure{}, err
	}
	erasure, err := c.deps.Anonymizer.Anonymize(ctx, a.WorkspaceID, id, a.UserID, c.now())
	if err != nil {
		return lead.Erasure{}, err
	}
	c.deps.Notifier.LeadChanged(lead.Change{WorkspaceID: a.WorkspaceID, LeadID: erasure.LeadID, Version: erasure.Version, Fields: []string{lead.FieldAnonymized}})
	c.announceTallies(a.WorkspaceID, erasure.Counterparts)
	return erasure, nil
}

func (c *Commands) targeted(ctx context.Context, a Actor, v lead.Viewer, id string, kind recordevent.Kind, sent []string, change func(*lead.Lead) (bool, error)) (*lead.Lead, error) {
	changed, err := c.writer.retrying(ctx, a.WorkspaceID, a.UserID, id, kind, func(l *lead.Lead) ([]string, error) {
		applied, err := change(l)
		if err != nil || !applied {
			return nil, err
		}
		return sent, nil
	})
	if errors.Is(err, errStillContended) {
		return nil, c.conflict(ctx, a, v, id, sent)
	}
	return changed, err
}
func (c *Commands) applyWhatsAppBlock(workspaceID, businessPhoneID, contactNumber string, block bool) bool {
	if strings.TrimSpace(businessPhoneID) == "" || strings.TrimSpace(contactNumber) == "" {
		return false
	}
	phone, err := c.blocker.Phone(workspaceID, businessPhoneID)
	if err != nil {
		log.Printf("[lead-block] the Meta block is skipped: %v", err)
		return false
	}
	if err := phone.Apply(contactNumber, block); err != nil {
		log.Printf("[lead-block] Meta block (block=%v) failed on phone %s: %v", block, businessPhoneID, err)
		return false
	}
	return true
}

func (c *Commands) permit(a Actor, actions ...workspace.Action) error {
	return c.viewers.permit(a, actions...)
}

func (c *Commands) authorize(a Actor, actions ...workspace.Action) (lead.Viewer, error) {
	return c.viewers.authorize(a, actions...)
}

func (c *Commands) now() time.Time {
	return c.deps.Now().UTC()
}
