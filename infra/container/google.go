package container

import (
	"context"
	"log"
	"time"

	deliveryHttp "vozko/delivery/http"
	googleaccounthttp "vozko/delivery/http/googleaccount"
	googlesheethttp "vozko/delivery/http/googlesheet"
	cache_domain "vozko/domain/cache"
	calendar_domain "vozko/domain/calendar"
	"vozko/domain/googleaccount"
	cronPackage "vozko/infra/cron"
	"vozko/infra/googleoauth"
	"vozko/infra/googlesheets"
	calendar_repository "vozko/infra/repositories/calendar"
	googleaccount_repository "vozko/infra/repositories/googleaccount"
	googlesheet_repository "vozko/infra/repositories/googlesheet"
	calendar_usecase "vozko/usecases/calendar"
	gauc "vozko/usecases/googleaccount"
	gsuc "vozko/usecases/googlesheet"
	"vozko/usecases/shared/oauthstate"
)

const (
	googleStatePrefix        = "google:oauth"
	googleLegacyImportBudget = 2 * time.Minute
)

type googleBundle struct {
	Accounts       googleaccount.Repository
	OAuth          *googleoauth.Client
	Tokens         *gauc.TokenSource
	Connector      *gauc.Connector
	Disconnector   *gauc.Disconnector
	Status         *gauc.StatusReader
	RefreshTokens  *gauc.RefreshTokensJob
	CalendarAccess calendar_domain.AccessProvider
}

func (c *Container) initGoogle() {
	cfg := c.cfg.Google
	if c.redisProvider == nil || c.repositories == nil || c.repositories.calendar == nil || c.services == nil || c.services.googleCalendar == nil {
		log.Fatalf("[google] redis, the calendar repository and the calendar API must exist before the Google accounts")
	}
	client, err := googleoauth.New(googleoauth.Config{ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, RedirectURI: cfg.RedirectURI})
	if err != nil {
		log.Fatalf("[google] oauth client: %v", err)
	}
	accounts := googleaccount_repository.NewRepository(c.db)
	alerts := gauc.NewAlerts(c.notifications().Publisher)
	tokens := gauc.NewTokenSource(accounts, client, googleoauth.Classifier{}, alerts)
	calendarAccess := calendar_usecase.NewAccessProvider(tokens)

	hooks := map[googleaccount.Product]gauc.ProductHooks{
		googleaccount.ProductCalendar: calendar_usecase.NewWatchHooks(
			calendar_usecase.NewStartWatchUseCase(c.repositories.calendar, c.services.googleCalendar, calendarAccess),
			calendar_usecase.NewStopWatchUseCase(c.repositories.calendar, c.services.googleCalendar, calendarAccess),
		),
	}
	issuer := c.mustOAuthStateIssuer(googleStatePrefix, cfg.ClientSecret, gauc.IntegrationsPath)
	sealed, err := oauthstate.NewSealedIssuer(issuer, c.redisProvider.SharedState(), googleStatePrefix)
	if err != nil {
		log.Fatalf("[google] sealed state: %v", err)
	}

	c.google = &googleBundle{
		Accounts:       accounts,
		OAuth:          client,
		Tokens:         tokens,
		Connector:      gauc.NewConnector(gauc.ConnectDeps{Accounts: accounts, OAuth: client, States: sealed, Alerts: alerts, Hooks: hooks}),
		Disconnector:   gauc.NewDisconnector(accounts, client, alerts, hooks),
		Status:         gauc.NewStatusReader(accounts),
		RefreshTokens:  gauc.NewRefreshTokensJob(accounts, tokens),
		CalendarAccess: calendarAccess,
	}
	c.importLegacyCalendarConnections()
	c.initGoogleSheets()
	log.Printf("[google] accounts enabled (redirect_uri=%q must be listed on the Google OAuth client)", cfg.RedirectURI)
}

func (c *Container) importLegacyCalendarConnections() {
	ctx, cancel := context.WithTimeout(context.Background(), googleLegacyImportBudget)
	defer cancel()
	imported, err := calendar_usecase.NewImportLegacyConnections(
		calendar_repository.NewLegacyConnectionStore(c.db), c.google.Accounts, c.repositories.calendar,
	).Execute(ctx)
	if err != nil {
		log.Fatalf("[google] importing the legacy Google Calendar connections failed after %d: %v", imported, err)
	}
	if imported > 0 {
		log.Printf("[google] imported %d legacy Google Calendar connections into the shared Google accounts", imported)
	}
}

func googleRoutes(c *Container) deliveryHttp.GoogleRoutes {
	if c.google == nil {
		log.Fatalf("[google] routes requested before the Google accounts were built")
	}
	return deliveryHttp.GoogleRoutes{
		Accounts: googleaccounthttp.NewHandler(googleaccounthttp.HandlerDeps{
			Connect:         c.google.Connector,
			Disconnect:      c.google.Disconnector,
			Status:          c.google.Status,
			FrontendBaseURL: c.cfg.FrontendBaseURL,
		}),
		Sheets: googlesheethttp.NewHandler(c.googleSheets.Manager, c.googleSheets.Intake),
	}
}

func (c *Container) registerGoogleJobs() {
	if c.google == nil || c.google.RefreshTokens == nil {
		log.Fatalf("[google] the account jobs were not built")
	}
	c.jobRunner.SetGoogleAccountJobs(c.google.RefreshTokens)
}

type googleSheetsBundle struct {
	Writer    *gsuc.Writer
	Intake    *gsuc.Intake
	Manager   *gsuc.Manager
	Retention *gsuc.Retention
}

func (c *Container) initGoogleSheets() {
	cfg := c.cfg.Google
	if c.google == nil || c.repositories.lead == nil || c.repositories.agent == nil || c.repositories.workflow == nil || c.services.metrics == nil {
		log.Fatalf("[google-sheets] the Google accounts, the lead, agent and workflow repositories and metrics must exist first")
	}
	shared := c.redisProvider.SharedState()
	limiter, err := googlesheets.NewLimiter(shared, googlesheets.Quota{
		ProjectWritesPerMinute: cfg.SheetsProjectWritesPerMinute,
		ProjectReadsPerMinute:  cfg.SheetsProjectReadsPerMinute,
		UserWritesPerMinute:    cfg.SheetsUserWritesPerMinute,
		UserReadsPerMinute:     cfg.SheetsUserReadsPerMinute,
	})
	if err != nil {
		log.Fatalf("[google-sheets] limiter: %v", err)
	}
	writerConfig := gsuc.DefaultWriterConfig(cfg.SheetsDispatchConcurrency)
	leases, err := cache_domain.NewOwnedLock(shared, writerConfig.LeaseTTL, 50*time.Millisecond)
	if err != nil {
		log.Fatalf("[google-sheets] sheet leases: %v", err)
	}
	sheets := googlesheet_repository.NewSheetRepository(c.db)
	rows := googlesheet_repository.NewRowRepository(c.db)
	api := googlesheets.New(googlesheets.GoogleEndpoints)
	alerts := gsuc.NewAlerts(c.notifications().Publisher)
	writer := gsuc.NewWriter(gsuc.WriterDeps{
		Sheets: sheets, Rows: rows, API: api, Errors: googlesheets.Classifier{}, Access: c.google.Tokens, Limiter: limiter,
		Leases: leases, Marks: shared, Alerts: alerts, Metrics: c.services.metrics, Config: writerConfig,
	})
	resolver := gsuc.NewContextValues(gsuc.ContextValuesDeps{
		Leads:           c.repositories.lead,
		EntryLeads:      c.repositories.lead,
		AgentNames:      c.agentName,
		WorkflowNames:   c.workflowName,
		FrontendBaseURL: c.cfg.FrontendBaseURL,
	})
	c.googleSheets = &googleSheetsBundle{
		Writer: writer,
		Intake: gsuc.NewIntake(sheets, rows, resolver, writer),
		Manager: gsuc.NewManager(gsuc.ManagerDeps{
			Sheets: sheets, Rows: rows, API: api, Errors: googlesheets.Classifier{}, Access: c.google.Tokens,
			Limiter: limiter, Marks: shared, Alerts: alerts, Nudger: writer,
		}),
		Retention: gsuc.NewRetention(rows),
	}
}

func (c *Container) agentName(_ context.Context, workspaceID, id string) (string, error) {
	found, err := c.repositories.agent.FindByID(id)
	if err != nil || found == nil || found.WorkspaceID != workspaceID {
		return "", nil
	}
	return found.Name, nil
}

func (c *Container) workflowName(_ context.Context, workspaceID, id string) (string, error) {
	found, err := c.repositories.workflow.FindByID(id)
	if err != nil || found == nil || found.WorkspaceID != workspaceID {
		return "", nil
	}
	return found.Name, nil
}

func (c *Container) registerGoogleSheetsJobs() {
	if c.googleSheets == nil {
		log.Fatalf("[google-sheets] the jobs were requested before the pipeline was built")
	}
	c.jobRunner.SetGoogleSheetsJobs(
		cronPackage.CtxJobFunc(c.googleSheets.Writer.DispatchPending),
		cronPackage.CtxJobFunc(c.googleSheets.Retention.Execute),
	)
}

type sheetRows struct {
	*gsuc.Manager
	*gsuc.Intake
}

func (c *Container) sheetRows() sheetRows {
	if c.googleSheets == nil {
		log.Fatalf("[google-sheets] the sheet rows were requested before the pipeline was built")
	}
	return sheetRows{Manager: c.googleSheets.Manager, Intake: c.googleSheets.Intake}
}
