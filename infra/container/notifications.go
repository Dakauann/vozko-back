package container

import (
	"context"
	"log"
	"time"

	"vozko/delivery/http/notificationhttp"
	"vozko/domain/notification"
	cronPackage "vozko/infra/cron"
	notification_service "vozko/infra/notifications"
	notification_repository "vozko/infra/repositories/notification"
	notification_usecase "vozko/usecases/notification"
	webhook_usecase "vozko/usecases/webhook"
	businessphone_usecase "vozko/usecases/whatsapp/business_phone"
)

const (
	notificationFanoutConcurrency    = 8
	notificationFanoutTimeout        = 5 * time.Minute
	notificationBroadcastConcurrency = 2
	notificationBroadcastTimeout     = 15 * time.Minute
)

type notificationWiring struct {
	Publisher  *notification_usecase.Publisher
	Inbox      *notification_usecase.InboxService
	Broadcasts *notification_usecase.Broadcasts
	Images     *notification_usecase.Images
	Delivery   *notification_usecase.Delivery
	Sweeper    *notification_usecase.Sweeper
	Retention  *notification_usecase.Retention
	Digests    *notification_usecase.Digests

	store       *notification_repository.Store
	directory   *notification_repository.Directory
	preferences *notification_repository.PreferenceStore
}

func (c *Container) notifications() *notificationWiring {
	if c.notificationCenter != nil {
		return c.notificationCenter
	}
	if c.services.notificationsQueuePub == nil || c.services.fileStorage == nil {
		log.Fatalf("[notifications] the notifications queue and the file storage must exist first")
	}
	store := notification_repository.NewStore(c.db)
	broadcastStore := notification_repository.NewBroadcastStore(c.db)
	directory := notification_repository.NewDirectory(c.db)
	preferences := notification_repository.NewPreferenceStore(c.db)
	queue := notification_usecase.NewQueue(c.services.notificationsQueuePub)
	images := notification_service.NewImageStore(c.services.fileStorage)

	publisher, err := notification_usecase.NewPublisher(notification_usecase.PublisherDeps{Outbox: store, Queue: queue, Images: images})
	if err != nil {
		log.Fatalf("[notifications] publisher: %v", err)
	}
	broadcasts, err := notification_usecase.NewBroadcasts(notification_usecase.BroadcastsDeps{
		Store: broadcastStore, Directory: directory, Publisher: publisher, Queue: queue, Images: images,
		Preferences: preferences, Permissions: c.services.conversationAuth,
	})
	if err != nil {
		log.Fatalf("[notifications] broadcasts: %v", err)
	}
	sweeper, err := notification_usecase.NewSweeper(notification_usecase.SweeperDeps{Outbox: store, Broadcasts: broadcastStore, Queue: queue})
	if err != nil {
		log.Fatalf("[notifications] sweeper: %v", err)
	}
	c.notificationCenter = &notificationWiring{
		Publisher:   publisher,
		Inbox:       notification_usecase.NewInboxService(store, preferences, nil),
		Broadcasts:  broadcasts,
		Images:      notification_usecase.NewImages(images),
		Sweeper:     sweeper,
		Retention:   notification_usecase.NewRetention(store, notification_usecase.DefaultPurgeSize, nil),
		store:       store,
		directory:   directory,
		preferences: preferences,
	}
	return c.notificationCenter
}

func (c *Container) initNotifications() {
	wiring := c.notifications()
	if c.services.conversationHub == nil || c.services.conversationAuth == nil || c.useCases.notifier == nil {
		log.Fatalf("[notifications] delivery needs the conversation hub, the permission checker and the email notifier")
	}
	delivery, err := notification_usecase.NewDelivery(notification_usecase.DeliveryDeps{
		Outbox:       wiring.store,
		Directory:    wiring.directory,
		Permissions:  c.services.conversationAuth,
		Preferences:  wiring.preferences,
		Digests:      wiring.store,
		Pusher:       c.services.conversationHub,
		Email:        c.useCases.notifier,
		DashboardURL: c.useCases.dashboardURL,
	})
	if err != nil {
		log.Fatalf("[notifications] delivery: %v", err)
	}
	wiring.Delivery = delivery

	digests, err := notification_usecase.NewDigests(notification_usecase.DigestsDeps{
		Store: wiring.store, Directory: wiring.directory, Email: c.useCases.notifier, DashboardURL: c.useCases.dashboardURL,
	})
	if err != nil {
		log.Fatalf("[notifications] digests: %v", err)
	}
	wiring.Digests = digests

	fanout := webhook_usecase.NewConsumerRunner(webhook_usecase.ConsumerConfig[notification.FanoutMessage]{
		Name: "notification_fanout", Topic: notification.FanoutTopic,
		QueueSub: c.services.notificationQueueSub, QueuePub: c.services.notificationsQueuePub,
		SharedState: c.redisProvider.SharedState(), Concurrency: notificationFanoutConcurrency, Timeout: notificationFanoutTimeout,
		Handle: func(ctx context.Context, m *notification.FanoutMessage) error {
			return delivery.Deliver(ctx, m.NotificationID)
		},
		Classify: classifyNotificationError,
	})
	expand := webhook_usecase.NewConsumerRunner(webhook_usecase.ConsumerConfig[notification.BroadcastMessage]{
		Name: "notification_broadcast", Topic: notification.BroadcastTopic,
		QueueSub: c.services.notificationQueueSub, QueuePub: c.services.notificationsQueuePub,
		SharedState: c.redisProvider.SharedState(), Concurrency: notificationBroadcastConcurrency, Timeout: notificationBroadcastTimeout,
		Handle: func(ctx context.Context, m *notification.BroadcastMessage) error {
			return wiring.Broadcasts.Expand(ctx, m.BroadcastID)
		},
		Classify: classifyNotificationError,
	})
	for _, consumer := range []interface{ Start() error }{fanout, expand} {
		if err := consumer.Start(); err != nil {
			log.Fatalf("[notifications] consumer failed to start: %v", err)
		}
	}
}

func classifyNotificationError(err error) webhook_usecase.Disposition {
	if notification.IsRefusal(err) {
		return webhook_usecase.DispositionDrop
	}
	return webhook_usecase.DispositionRetry
}

func (c *Container) notificationJobs() (sweep, purge, digest cronPackage.CtxJobFunc) {
	wiring := c.notifications()
	if wiring.Digests == nil {
		log.Fatalf("[notifications] the digest job needs initNotifications to run first")
	}
	return cronPackage.CtxJobFunc(wiring.Sweeper.Sweep), cronPackage.CtxJobFunc(wiring.Retention.Purge), cronPackage.CtxJobFunc(wiring.Digests.Send)
}

func (c *Container) notificationHandler() *notificationhttp.Handler {
	return notificationhttp.NewHandler(c.notifications().Inbox)
}

func (c *Container) notificationAdminHandler() *notificationhttp.AdminHandler {
	wiring := c.notifications()
	return notificationhttp.NewAdminHandler(wiring.Broadcasts, wiring.Images)
}

func (c *Container) phoneNotices(dashboardURL string) *businessphone_usecase.PhoneNotices {
	return businessphone_usecase.NewPhoneNotices(c.notifications().Publisher, dashboardURL)
}
