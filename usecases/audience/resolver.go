package audience_usecase

import (
	"context"
	"errors"
	"strings"

	ca "vozko/domain/audience"
	"vozko/domain/shared"
)

// Settings resolution: the ONE place the "post override falls back to the
// account" rule lives. The engine and the ingest path ask this; the API's
// per-post editor reads and writes through the same use cases so what the
// operator sees as "effective" is what the engine will run with.

type settingsResolver struct {
	settings ca.SettingsRepository
}

// NewSettingsResolver builds the resolver over the settings repository.
func NewSettingsResolver(settings ca.SettingsRepository) ca.SettingsResolver {
	return &settingsResolver{settings: settings}
}

func (r *settingsResolver) Resolve(ctx context.Context, ref ca.ContainerRef) (*ca.Settings, error) {
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	account, err := r.settings.Find(ctx, ref.Source, ref.AccountID)
	if err != nil {
		if !errors.Is(err, ca.ErrNotFound) {
			return nil, err
		}
		// Never configured. What that MEANS depends on the subject, and
		// getting it wrong is silent in both directions.
		//
		// For a COMMENT, this row is the switch: an operator configures an
		// account's topic set, threshold and cap, and nothing runs until they
		// do. Defaulting to off is the whole safety model, and a post override
		// cannot exist without an account row that owns it, so nothing to
		// layer either.
		//
		// For a CONVERSATION there is no such row and never will be: the
		// operator's decision lives on the CHANNEL (a WhatsApp campaign's
		// EnableAnalysis, an instance's, an account's), and the ingest path
		// already refused to enqueue anything that switch had turned off.
		// Demanding a second row here meant every conversation was enqueued and
		// then immediately skipped as analysis_disabled, so the channel toggle
		// appeared to do nothing at all.
		def := ca.NewSettings("", ref.Source, ref.AccountID, ca.VerticalServices)
		def.Enabled = ref.Normalized().Kind == ca.SubjectKindConversation
		return &def, nil
	}
	override, err := r.settings.FindOverride(ctx, ref)
	if err != nil && !errors.Is(err, ca.ErrNotFound) {
		return nil, err
	}
	effective := account.WithOverride(override)
	return &effective, nil
}

// ---- per-post settings use cases ----

type containerSettingsUseCases struct {
	settings  ca.SettingsRepository
	resolver  ca.SettingsResolver
	verifiers map[ca.Source]AccountVerifier
	clock     ca.Clock
}

// NewContainerSettingsUseCases builds the get/put/delete trio and the
// workspace listing over one set of dependencies; all share the ownership
// check and the resolver.
func NewContainerSettingsUseCases(
	settings ca.SettingsRepository,
	resolver ca.SettingsResolver,
	verifiers map[ca.Source]AccountVerifier,
	clock ca.Clock,
) (ca.GetContainerSettingsUseCase, ca.PutContainerSettingsUseCase, ca.DeleteContainerSettingsUseCase, ca.ListAccountSettingsUseCase) {
	if clock == nil {
		clock = shared.SystemClock{}
	}
	uc := &containerSettingsUseCases{settings: settings, resolver: resolver, verifiers: verifiers, clock: clock}
	return getContainerSettings{uc}, putContainerSettings{uc}, deleteContainerSettings{uc}, listAccountSettings{uc}
}

func (uc *containerSettingsUseCases) verify(ctx context.Context, workspaceID string, ref ca.ContainerRef) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	v, ok := uc.verifiers[ref.Source]
	if !ok {
		return ca.ErrContainerInvalid
	}
	owns, err := v.AccountBelongsTo(ctx, workspaceID, ref.AccountID)
	if err != nil {
		return err
	}
	if !owns {
		return ca.ErrNotFound
	}
	return nil
}

func (uc *containerSettingsUseCases) view(ctx context.Context, ref ca.ContainerRef) (*ca.ContainerSettings, error) {
	override, err := uc.settings.FindOverride(ctx, ref)
	if err != nil && !errors.Is(err, ca.ErrNotFound) {
		return nil, err
	}
	effective, err := uc.resolver.Resolve(ctx, ref)
	if err != nil {
		return nil, err
	}
	return &ca.ContainerSettings{Override: override, Effective: *effective}, nil
}

type getContainerSettings struct{ *containerSettingsUseCases }

func (g getContainerSettings) Execute(ctx context.Context, workspaceID string, ref ca.ContainerRef) (*ca.ContainerSettings, error) {
	if err := g.verify(ctx, strings.TrimSpace(workspaceID), ref); err != nil {
		return nil, err
	}
	return g.view(ctx, ref)
}

type putContainerSettings struct{ *containerSettingsUseCases }

func (p putContainerSettings) Execute(ctx context.Context, o ca.ContainerOverride) (*ca.ContainerSettings, error) {
	o.Normalize()
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if err := p.verify(ctx, o.WorkspaceID, o.Ref()); err != nil {
		return nil, err
	}
	// An override that changes nothing is not stored: "inherits everything"
	// is the absence of a row, so the UI's "reset to account" is honest.
	if o.IsEmpty() {
		if err := p.settings.DeleteOverride(ctx, o.Ref()); err != nil {
			return nil, err
		}
		return p.view(ctx, o.Ref())
	}
	o.UpdatedAt = p.clock.Now()
	if err := p.settings.SaveOverride(ctx, &o); err != nil {
		return nil, err
	}
	return p.view(ctx, o.Ref())
}

type deleteContainerSettings struct{ *containerSettingsUseCases }

func (d deleteContainerSettings) Execute(ctx context.Context, workspaceID string, ref ca.ContainerRef) (*ca.ContainerSettings, error) {
	if err := d.verify(ctx, strings.TrimSpace(workspaceID), ref); err != nil {
		return nil, err
	}
	if err := d.settings.DeleteOverride(ctx, ref); err != nil {
		return nil, err
	}
	return d.view(ctx, ref)
}

type listAccountSettings struct{ *containerSettingsUseCases }

func (l listAccountSettings) Execute(ctx context.Context, workspaceID string) ([]*ca.Settings, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, ca.ErrWorkspaceRequired
	}
	rows, err := l.settings.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []*ca.Settings{}
	}
	return rows, nil
}
