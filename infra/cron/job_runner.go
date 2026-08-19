package cron

import (
	"context"
	"log"
	"time"
	billing "vozko/domain/billing"
	"vozko/domain/cache"
	calendar_domain "vozko/domain/calendar"
	"vozko/domain/conversation"
	"vozko/domain/cron"
	shortlink_domain "vozko/domain/shortlink"
	businessphone "vozko/domain/whatsapp/business_phone"
	whatsapp_template "vozko/domain/whatsapp/template"
	wc_usecase "vozko/domain/whatsapp_campaign"
	workflow_domain "vozko/domain/workflow"
	workspace_plan "vozko/domain/workspace/workspace_plan"
	balance_usecase "vozko/usecases/balance"
)

type JobRunner struct {
	orderCleanupJob                    cron.OrderCleanupChecker
	startScheduledWhatsappCampaignsJob wc_usecase.StartScheduleJob
	shared                             cache.SharedState
	analysisDebounceJob                conversation.AnalysisDebounceJob
	autoCloseJob                       conversation.AutoCloseJob
	workflowManager                    workflow_domain.WorkflowManager
	expireSubscriptions                workspace_plan.ExpireSubscriptionsUseCase
	remindExpiringSubscriptions        workspace_plan.RemindExpiringSubscriptionsUseCase
	monitorLowBalance                  *balance_usecase.MonitorLowBalanceUseCase
	renewCalendarChannels              calendar_domain.RenewExpiringChannelsUseCase
	reconcileWhatsAppTemplates         whatsapp_template.ReconcileTemplatesUseCase
	// Instagram jobs are registered with setters rather than through the
	// constructor: the channel is optional, and threading two more positional
	// arguments through a 17-arg constructor for an optional feature is not worth
	// the churn. StartAll runs after the container has had a chance to set them.
	// channelJobs holds the optional per-channel periodic jobs, appended by the
	// Set*Jobs methods below.
	channelJobs                   []channelJob
	reconcileWhatsAppEntitlements businessphone.EntitlementReconciler
	emitMonthlyInvoices           billing.EmitMonthlyInvoicesUseCase
	cancelBillingSweep            billing.CancelSweepUseCase
	vendorChannelReconciler       businessphone.VendorChannelReconciler
	channelStatusReconciler       businessphone.ChannelStatusReconciler
	purgeShortLinkClicks          shortlink_domain.PurgeClicksUseCase
}

func NewJobRunner(orderCleanupJob cron.OrderCleanupChecker, startScheduledWhatsappCampaignsJob wc_usecase.StartScheduleJob, shared cache.SharedState, analysisDebounceJob conversation.AnalysisDebounceJob, autoCloseJob conversation.AutoCloseJob, workflowManager workflow_domain.WorkflowManager, expireSubscriptions workspace_plan.ExpireSubscriptionsUseCase, remindExpiringSubscriptions workspace_plan.RemindExpiringSubscriptionsUseCase, monitorLowBalance *balance_usecase.MonitorLowBalanceUseCase, renewCalendarChannels calendar_domain.RenewExpiringChannelsUseCase, reconcileWhatsAppTemplates whatsapp_template.ReconcileTemplatesUseCase, reconcileWhatsAppEntitlements businessphone.EntitlementReconciler, emitMonthlyInvoices billing.EmitMonthlyInvoicesUseCase, cancelBillingSweep billing.CancelSweepUseCase, vendorChannelReconciler businessphone.VendorChannelReconciler, channelStatusReconciler businessphone.ChannelStatusReconciler, purgeShortLinkClicks shortlink_domain.PurgeClicksUseCase) *JobRunner {
	return &JobRunner{
		orderCleanupJob:                    orderCleanupJob,
		startScheduledWhatsappCampaignsJob: startScheduledWhatsappCampaignsJob,
		shared:                             shared,
		analysisDebounceJob:                analysisDebounceJob,
		autoCloseJob:                       autoCloseJob,
		workflowManager:                    workflowManager,
		expireSubscriptions:                expireSubscriptions,
		remindExpiringSubscriptions:        remindExpiringSubscriptions,
		monitorLowBalance:                  monitorLowBalance,
		renewCalendarChannels:              renewCalendarChannels,
		reconcileWhatsAppTemplates:         reconcileWhatsAppTemplates,
		reconcileWhatsAppEntitlements:      reconcileWhatsAppEntitlements,
		emitMonthlyInvoices:                emitMonthlyInvoices,
		cancelBillingSweep:                 cancelBillingSweep,
		vendorChannelReconciler:            vendorChannelReconciler,
		channelStatusReconciler:            channelStatusReconciler,
		purgeShortLinkClicks:               purgeShortLinkClicks,
	}
}

// ctxJob is a context-aware periodic job. Newer usecases take a context, unlike
// the older Execute() jobs above.
type ctxJob interface {
	Execute(ctx context.Context) error
}

// CtxJobFunc adapts a plain method to ctxJob.
//
// It exists because a use case can own more than one periodic sweep — the
// unofficial WhatsApp health check runs a cheap session backstop and a slower
// integrity pass over the same dependencies — and only one of them can be
// called Execute. The alternative, a wrapper type per extra sweep, is
// boilerplate that says nothing.
type CtxJobFunc func(ctx context.Context) error

func (f CtxJobFunc) Execute(ctx context.Context) error { return f(ctx) }

// channelJobs are the optional per-channel periodic jobs.
//
// Every one of them is the same shape, a distributed lock, a ticker, a
// context-aware Execute, so they are declared as data rather than as one
// hand-written 25-line method each. That is what stopped Telegram's two jobs
// from being a copy of Instagram's two.
type channelJob struct {
	name   string
	period time.Duration
	job    ctxJob
}

// SetInstagramJobs registers the Instagram periodic jobs. Safe to skip entirely:
// nil jobs are not started.
func (r *JobRunner) SetInstagramJobs(tokenRefresh, eventPurge ctxJob) {
	// Instagram tokens last 60 days, cannot be refreshed in their first 24 hours,
	// and die permanently if unused for 60 days, with no recovery except full
	// re-auth. The usecase refreshes ~20 days ahead of expiry, so an hourly tick
	// gives many chances to recover from a transient failure before a tenant is
	// locked out.
	r.addChannelJob("instagram_token_refresh", time.Hour, tokenRefresh)
	r.addChannelJob("instagram_event_purge", 24*time.Hour, eventPurge)
}

// SetTelegramJobs registers the Telegram periodic jobs.
func (r *JobRunner) SetTelegramJobs(webhookHealth, eventPurge ctxJob) {
	// Telegram has no token to refresh, a bot token never expires. What it has
	// instead is a webhook that can start failing silently, and undelivered
	// updates are DISCARDED after 24 hours with no history API to recover them.
	// So the hourly job here is the data-loss alarm, not hygiene.
	r.addChannelJob("telegram_webhook_health", time.Hour, webhookHealth)
	r.addChannelJob("telegram_event_purge", 24*time.Hour, eventPurge)
}

// SetUnofficialWhatsAppJobs registers the linked-device WhatsApp periodic jobs.
//
// Three cadences, and the split is deliberate rather than cosmetic:
//
//   - Session health is a BACKSTOP, every 15 minutes. The provider pushes a
//     `connection` event on every state change, so a dropped session is already
//     known within seconds through the normal pipeline; this only covers the
//     case where that pipeline is itself broken, and it skips any instance the
//     webhook recently spoke for.
//   - Integrity is hourly. It answers the two questions no event can — is our
//     webhook still registered on the host, and is WhatsApp restricting this
//     number — plus reads the host's short delivery-failure log. Three extra
//     calls per instance, so it does not belong on the backstop's schedule.
//   - Capacity reconciliation is daily: it corrects counter drift and names
//     instances stranded on a host. Neither is urgent, and both sweep every
//     configured host.
func (r *JobRunner) SetUnofficialWhatsAppJobs(sessionHealth, verifyIntegrity, reconcileCapacity, purgeEvents ctxJob) {
	r.addChannelJob("unofficial_whatsapp_session_health", 15*time.Minute, sessionHealth)
	r.addChannelJob("unofficial_whatsapp_integrity", time.Hour, verifyIntegrity)
	r.addChannelJob("unofficial_whatsapp_capacity_reconcile", 24*time.Hour, reconcileCapacity)
	r.addChannelJob("unofficial_whatsapp_event_purge", 24*time.Hour, purgeEvents)
}

// SetWhatsAppTemplateSendJobs registers the paid-send reconciliation sweep.
//
// Hourly, and the cadence is a money decision rather than a load one. The sweep
// refunds sends that took a customer's balance and never reached a terminal
// state — a crash between the debit and the provider call, or between the
// provider's answer and our recording it. Every hour it does not run is an hour
// somebody's money is held for a message that may not exist. Running it more
// often would start refunding sends whose delivery webhook is merely late.
func (r *JobRunner) SetWhatsAppTemplateSendJobs(reconcile ctxJob) {
	r.addChannelJob("whatsapp_template_send_reconcile", time.Hour, reconcile)
}

// SetScheduledMessageJobs registers the scheduled-message periodic jobs.
//
// Registration rather than construction, for the reason the channel jobs above
// give: these are data (a name, a period, a ctxJob), and threading two more
// positional arguments through a 17-argument constructor buys nothing. Neither
// can arrive nil — the container validates both at construction and refuses to
// boot without them.
//
//   - The sweep is the BACKSTOP that makes delivery correct without a broker: a
//     lost delayed message, an outage during create, a consumer that was down.
//     A minute is the worst-case lateness an operator would ever see.
//   - The purge is hygiene on terminal rows only; daily is ample.
func (r *JobRunner) SetScheduledMessageJobs(sweep, purge ctxJob) {
	r.addChannelJob("scheduled_message_sweep", time.Minute, sweep)
	r.addChannelJob("scheduled_message_purge", 24*time.Hour, purge)
}

func (r *JobRunner) addChannelJob(name string, period time.Duration, job ctxJob) {
	if job == nil {
		return
	}
	r.channelJobs = append(r.channelJobs, channelJob{name: name, period: period, job: job})
}

// runChannelJob ticks one channel job under a distributed lock, so only one
// replica runs it.
func (r *JobRunner) runChannelJob(j channelJob) {
	ticker := time.NewTicker(j.period)
	defer ticker.Stop()

	for range ticker.C {
		if r.tryLock(j.name, 2*j.period) {
			func() {
				defer r.releaseLock(j.name)
				if err := j.job.Execute(context.Background()); err != nil {
					log.Printf("[cron] %s error: %v", j.name, err)
				}
			}()
		}
	}
}

func (r *JobRunner) tryLock(name string, ttl time.Duration) bool {
	if r.shared == nil {

		log.Printf("[cron] no shared state configured for %s, skipping (fail-closed)", name)
		return false
	}
	acquired, err := r.shared.SetNX("lock:cron:"+name, "1", ttl)
	if err != nil {
		log.Printf("[cron] lock acquire error for %s: %v, skipping", name, err)
		return false
	}
	return acquired
}

func (r *JobRunner) releaseLock(name string) {
	if r.shared == nil {
		return
	}
	if err := r.shared.Del("lock:cron:" + name); err != nil {
		log.Printf("[cron] lock release error for %s: %v", name, err)
	}
}

func (r *JobRunner) StartAll() {
	go r.runOrderCleanupEvery5Minutes()
	go r.runStartScheduledWhatsappCampaignsJob()
	go r.runAnalysisDebounceEveryMinute()
	go r.runConversationAutoCloseEvery5Minutes()
	go r.runWorkflowManagerEveryMinute()
	go r.runExpireSubscriptionsEveryHour()
	go r.runPlanExpiryRemindersDaily()
	go r.runMonitorLowBalanceDaily()
	go r.runRenewCalendarChannelsEvery30Minutes()
	go r.runReconcileWhatsAppTemplatesEvery15Minutes()
	go r.runReconcileWhatsAppEntitlementsEvery15Minutes()
	go r.runEmitMonthlyInvoicesHourly()
	go r.runCancelBillingSweepHourly()
	go r.runVendorChannelReconcileDaily()
	go r.runReconcileChannelStatusEvery10Minutes()
	go r.runShortLinkRetentionDaily()
	for _, job := range r.channelJobs {
		go r.runChannelJob(job)
	}
}

func (r *JobRunner) runReconcileWhatsAppTemplatesEvery15Minutes() {
	if r.reconcileWhatsAppTemplates == nil {
		return
	}

	period := 15 * time.Minute
	ticker := time.NewTicker(period)
	defer ticker.Stop()

	for range ticker.C {
		if r.tryLock("reconcile_whatsapp_templates", 2*period) {
			func() {
				defer r.releaseLock("reconcile_whatsapp_templates")
				if err := r.reconcileWhatsAppTemplates.Execute(); err != nil {
					log.Printf("[cron] reconcile_whatsapp_templates error: %v", err)
				}
			}()
		}
	}
}

// runReconcileChannelStatusEvery10Minutes syncs local dialog360 numbers with the
// partner's actual channel state: backfilling metadata that lags channel_live (the
// display number, WABA name) and SUSPENDING numbers whose channel was deactivated at
// 360dialog/Meta (so a now-invalid API key is never used). One ListChannels per run,
// so it never risks 360dialog's rate limit. Idempotent; a locked/missed tick self-heals.
func (r *JobRunner) runReconcileChannelStatusEvery10Minutes() {
	if r.channelStatusReconciler == nil {
		return
	}
	period := 10 * time.Minute
	// Run once shortly after startup so a fresh deploy backfills the number/metadata
	// immediately instead of waiting a full period. The short delay lets the app settle
	// (DB, redis, partner client) before the first partner API call.
	time.Sleep(20 * time.Second)
	r.reconcileChannelStatusOnce()

	ticker := time.NewTicker(period)
	defer ticker.Stop()

	for range ticker.C {
		r.reconcileChannelStatusOnce()
	}
}

func (r *JobRunner) reconcileChannelStatusOnce() {
	if r.tryLock("reconcile_channel_status", 20*time.Minute) {
		defer r.releaseLock("reconcile_channel_status")
		rep, err := r.channelStatusReconciler.Execute()
		if err != nil {
			log.Printf("[cron] reconcile_channel_status error: %v", err)
		} else if rep.Suspended > 0 || rep.Updated > 0 || rep.WABAsNamed > 0 {
			log.Printf("[cron] reconcile_channel_status: suspended=%d updated=%d wabas_named=%d", rep.Suspended, rep.Updated, rep.WABAsNamed)
		}
	}
}

func (r *JobRunner) runReconcileWhatsAppEntitlementsEvery15Minutes() {
	if r.reconcileWhatsAppEntitlements == nil {
		return
	}

	period := 15 * time.Minute
	ticker := time.NewTicker(period)
	defer ticker.Stop()

	for range ticker.C {
		if r.tryLock("reconcile_whatsapp_entitlements", 2*period) {
			func() {
				defer r.releaseLock("reconcile_whatsapp_entitlements")
				n, err := r.reconcileWhatsAppEntitlements.Execute()
				if err != nil {
					log.Printf("[cron] reconcile_whatsapp_entitlements error: %v", err)
				} else if n > 0 {
					log.Printf("[cron] reconcile_whatsapp_entitlements: reconciled %d workspace(s)", n)
				}
			}()
		}
	}
}

// runEmitMonthlyInvoicesHourly issues the unified monthly invoices during the emit window. It ticks
// hourly (robust against deploy timing) but only acts on BRT days [EMIT_DAY, DUE_DAY): the invoice
// anchor is the current month's due day, so emitting on or after it would bill next month's cycle weeks
// early. The emit is idempotent per workspace/anchor, so the repeated in-window ticks are no-ops after
// the first, and a tick missed to a deploy self-heals on the next hour.
func (r *JobRunner) runEmitMonthlyInvoicesHourly() {
	if r.emitMonthlyInvoices == nil {
		return
	}
	period := time.Hour
	ticker := time.NewTicker(period)
	defer ticker.Stop()

	for range ticker.C {
		day := time.Now().In(billing.LocationBRT()).Day()
		if day < billing.DefaultEmitDay || day >= billing.DefaultDueDay {
			continue
		}
		if r.tryLock("emit_monthly_invoices", 2*period) {
			func() {
				defer r.releaseLock("emit_monthly_invoices")
				n, err := r.emitMonthlyInvoices.Execute()
				if err != nil {
					log.Printf("[cron] emit_monthly_invoices error: %v (emitted %d before failure)", err, n)
				} else if n > 0 {
					log.Printf("[cron] emit_monthly_invoices: emitted %d invoice(s)", n)
				}
			}()
		}
	}
}

// runCancelBillingSweepHourly registers the 360dialog cancellation for every workspace whose unified
// invoice went unpaid through dunning. The use case self-gates to the cutoff day (a no-op before it),
// so ticking hourly just makes the sweep robust and idempotent across the cutoff-to-month-end window.
func (r *JobRunner) runCancelBillingSweepHourly() {
	if r.cancelBillingSweep == nil {
		return
	}
	period := time.Hour
	ticker := time.NewTicker(period)
	defer ticker.Stop()

	for range ticker.C {
		if r.tryLock("cancel_billing_sweep", 2*period) {
			func() {
				defer r.releaseLock("cancel_billing_sweep")
				n, err := r.cancelBillingSweep.Execute()
				if err != nil {
					log.Printf("[cron] cancel_billing_sweep error: %v (swept %d before failure)", err, n)
				} else if n > 0 {
					log.Printf("[cron] cancel_billing_sweep: swept %d workspace(s)", n)
				}
			}()
		}
	}
}

// runVendorChannelReconcileDaily compares the platform's dialog360 channel state against the partner's actual
// channel listing once a day, re-cancelling a channel still live at the vendor that the platform already
// suspended (a lost cancellation) and alerting on an orphan. It is the financial backstop, so a daily
// cadence is enough; it makes an external ListChannels call, so it is not run more often.
func (r *JobRunner) runVendorChannelReconcileDaily() {
	if r.vendorChannelReconciler == nil {
		return
	}
	period := 24 * time.Hour
	ticker := time.NewTicker(period)
	defer ticker.Stop()

	for range ticker.C {
		if r.tryLock("vendor_channel_reconcile", 2*time.Hour) {
			func() {
				defer r.releaseLock("vendor_channel_reconcile")
				report, err := r.vendorChannelReconciler.Execute()
				if err != nil {
					log.Printf("[cron] vendor_channel_reconcile error: %v", err)
				} else if report.Orphans > 0 || report.Leaks > 0 {
					log.Printf("[cron] vendor_channel_reconcile: billing=%d orphans=%d leaks=%d recancelled=%d",
						report.VendorBilling, report.Orphans, report.Leaks, report.Recancelled)
				}
			}()
		}
	}
}

func (r *JobRunner) runStartScheduledWhatsappCampaignsJob() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		if r.startScheduledWhatsappCampaignsJob != nil && r.tryLock("start_scheduled_wc_campaigns", 2*time.Minute) {
			_ = r.startScheduledWhatsappCampaignsJob.StartScheduledWhatsappCampaigns()
		}
	}
}

func (r *JobRunner) runOrderCleanupEvery5Minutes() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		if r.tryLock("order_cleanup", 10*time.Minute) {
			if err := r.orderCleanupJob.CheckExpiredOrders(); err != nil {
				continue
			}
		}
	}
}

func (r *JobRunner) runAnalysisDebounceEveryMinute() {
	if r.analysisDebounceJob == nil {
		return
	}

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {

		if err := r.analysisDebounceJob.ProcessPendingAnalyses(); err != nil {
			log.Printf("[cron] analysis debounce error: %v", err)
		}
	}
}

// runConversationAutoCloseEvery5Minutes finishes idle open chats (system close).
// Distributed lock so only one replica closes; batch capped inside the job.
func (r *JobRunner) runConversationAutoCloseEvery5Minutes() {
	if r.autoCloseJob == nil {
		return
	}
	period := 5 * time.Minute
	// Immediate first tick after boot (short delay so DB is ready).
	time.Sleep(30 * time.Second)
	if r.tryLock("conversation_auto_close", period) {
		func() {
			defer r.releaseLock("conversation_auto_close")
			if err := r.autoCloseJob.ProcessIdleCloses(); err != nil {
				log.Printf("[cron] conversation_auto_close error: %v", err)
			}
		}()
	}

	ticker := time.NewTicker(period)
	defer ticker.Stop()
	for range ticker.C {
		if r.tryLock("conversation_auto_close", period) {
			func() {
				defer r.releaseLock("conversation_auto_close")
				if err := r.autoCloseJob.ProcessIdleCloses(); err != nil {
					log.Printf("[cron] conversation_auto_close error: %v", err)
				}
			}()
		}
	}
}

func (r *JobRunner) runWorkflowManagerEveryMinute() {
	if r.workflowManager == nil {
		return
	}

	if r.tryLock("workflow_manager", 50*time.Second) {
		log.Println("[cron] workflow_manager: immediate tick")
		r.workflowManager.Tick()
	}

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		if r.tryLock("workflow_manager", 50*time.Second) {
			log.Println("[cron] workflow_manager: tick")
			r.workflowManager.Tick()
		}
	}
}

func (r *JobRunner) runExpireSubscriptionsEveryHour() {
	if r.expireSubscriptions == nil {
		return
	}

	period := time.Hour
	ticker := time.NewTicker(1 * period)
	defer ticker.Stop()

	for range ticker.C {
		log.Println("[cron] expire_subscriptions: tick")
		if r.tryLock("expire_subscriptions", 2*period) {
			n, err := r.expireSubscriptions.Execute()
			if err != nil {
				log.Printf("[cron] expire_subscriptions error: %v (expired %d before failure)", err, n)
			} else if n > 0 {
				log.Printf("[cron] expire_subscriptions: expired %d subscriptions", n)
			}
		}
	}
}

func (r *JobRunner) runPlanExpiryRemindersDaily() {
	if r.remindExpiringSubscriptions == nil {
		return
	}

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		if r.tryLock("plan_expiry_reminders", 2*time.Hour) {
			func() {
				defer r.releaseLock("plan_expiry_reminders")
				n, err := r.remindExpiringSubscriptions.RemindExpiring()
				if err != nil {
					log.Printf("[cron] plan_expiry_reminders error: %v", err)
				} else if n > 0 {
					log.Printf("[cron] plan_expiry_reminders: sent %d reminder(s)", n)
				}
			}()
		}
	}
}

func (r *JobRunner) runMonitorLowBalanceDaily() {
	if r.monitorLowBalance == nil {
		return
	}

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		if r.tryLock("monitor_low_balance", 2*time.Hour) {
			func() {
				defer r.releaseLock("monitor_low_balance")
				n, err := r.monitorLowBalance.Run()
				if err != nil {
					log.Printf("[cron] monitor_low_balance error: %v", err)
				} else if n > 0 {
					log.Printf("[cron] monitor_low_balance: %d low wallet(s) found", n)
				}
			}()
		}
	}
}

func (r *JobRunner) runRenewCalendarChannelsEvery30Minutes() {
	if r.renewCalendarChannels == nil {
		return
	}

	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		if r.tryLock("renew_calendar_channels", 60*time.Minute) {
			n, err := r.renewCalendarChannels.Execute()
			if err != nil {
				log.Printf("[cron] renew_calendar_channels error: %v", err)
			} else if n > 0 {
				log.Printf("[cron] renew_calendar_channels: renewed %d channels", n)
			}
		}
	}
}

func (r *JobRunner) runShortLinkRetentionDaily() {
	if r.purgeShortLinkClicks == nil {
		return
	}

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		if r.tryLock("shortlink_retention", 2*time.Hour) {
			func() {
				defer r.releaseLock("shortlink_retention")
				if err := r.purgeShortLinkClicks.Execute(context.Background()); err != nil {
					log.Printf("[cron] shortlink_retention error: %v", err)
				}
			}()
		}
	}
}
