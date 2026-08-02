# Adding a channel

How to add a messaging channel (Telegram, Messenger, …) so it behaves like
WhatsApp and Instagram across the whole CRM: inbox list, conversation view,
replies, kanban board, stages, labels and filters.

The shared machinery keys on **`(entry_id, entry_type)`** and never on a channel
table. Anything that follows that rule works for a new channel automatically;
this document lists the places that genuinely need to learn about it.

Instagram and Telegram are the reference implementations — when in doubt, grep
for one of them and mirror it. Telegram is the better model for a channel with
no campaign concept and no publishing surface; Instagram is the better model for
one that also has posts and comments.

**Read §0.5 first.** Registering a channel in the sets below gets it into the
inbox and onto the board. It does NOT get it analysis, analytics, export, the AI
finish/stage tools or webhook-triggered workflows — those used to fail closed and
silently for anything but WhatsApp, and the registries that fixed it are the
things you now have to add one line to.

---

## 0. Before you start: what the channel must provide

| Requirement | Why |
|---|---|
| A conversation table with `workspace_id`, a contact reference, `last_message_at`, `deleted_at` | The read paths select from it |
| An account/container row carrying `workspace_id` and (ideally) `department_id` | Workspace scoping and department scoping |
| An inbound webhook or poll | To create conversations and messages |
| An outbound send API | To reply |
| A stable provider message id | Deduplication and echo reconciliation |

If the provider restricts outbound messaging to a time window (Instagram's 24h
rule), note it now — step 5 is where it is expressed. A channel with no window is
not exempt from step 5: Telegram in bot mode has no clock at all, and its
composer closes only when the customer blocks the bot. That still has to be
expressed, because the UI copy for "closed" differs.

---

## 0.5 The registries: one line each, and the reason they exist

Every one of these replaced an `entryType == "whatsapp"` check whose `default`
branch returned "not supported" with no error and no log line. A channel missing
from one of them does not break — it silently loses a feature, which is how
Instagram shipped for months with conversations that could be transferred and
staged but never closed, analysed, exported or counted.

| Registry | File | What a missing entry costs |
|---|---|---|
| Entry-type predicate sets | [domain/shared/entry_type.go](../domain/shared/entry_type.go) | Everything below; `channel_parity_test.go` fails the build if the sets disagree |
| Inbox/board UNION | [infra/repositories/conversation/entry_sources.go](../infra/repositories/conversation/entry_sources.go) | Conversations invisible in the list and on the board |
| Entry-scoped queries | [infra/repositories/conversation/channel_queries.go](../infra/repositories/conversation/channel_queries.go) | `invalid entry type` on the scoped inbox, no window filter, blank conversation header |
| Attendance/analytics | [infra/repositories/attendance/channel_sources.go](../infra/repositories/attendance/channel_sources.go) | Agents handling the channel look idle; the channel-mix panel reports 100% of something else |
| Workflow ownership | [infra/repositories/workflow/entry_ownership_repository.go](../infra/repositories/workflow/entry_ownership_repository.go) | Webhook-triggered workflows rejected silently |
| Stage counts | [infra/repositories/stage/stage_repository.go](../infra/repositories/stage/stage_repository.go) | Per-channel stage counts silently return every channel's |
| Analysis subject | `SetAnalysisSubjectResolver` in [infra/container/jobs.go](../infra/container/jobs.go) | `EnableAnalysis` is a switch that does nothing |
| Export source | `SetChannelEntryLister` in [infra/container/channel_wiring.go](../infra/container/channel_wiring.go) | `unsupported entry type` — the tenant cannot get their data out |
| Status counts | `SetConversationCounter` (step 8) | The inbox header reads "no work here" while the list below shows work |

---

## 1. Register the entry type — `domain/shared/entry_type.go`

```go
const EntryTypeTelegram EntryType = "telegram"
```

Then add it to the sets that apply:

- `messagingEntryTypes` — channels the shared messaging pipeline persists and
  routes. Gates `Valid()`. Add your channel here.
- `conversationViewableEntryTypes` — conversations the CRM can open, search and
  page. Add your channel here.
- `crmTaggableEntryTypes` — conversations that can carry a kanban stage and
  labels. Gates `SupportsCRMTagging()`, which both `domain/stage` and
  `domain/label` validate against. **If your channel reaches the board (step 4),
  it must be here**, or its cards render but cannot be moved or labelled.
- `conversationClosableEntryTypes` — conversations that can be finished. Gates
  the AI finish tool, the workflow finish node and the manual close.
- `inboxScopableEntryTypes` — valid values of the websocket's `campaignType` and
  of `SearchInboxInput.CampaignType`. Missing here is a 400 on connect.
- `containerScopedInboxEntryTypes` — channels whose inbox can be narrowed to one
  container (a campaign, or an account). Narrower than the previous set: voice and
  support are selectable but have no container query behind them.
- `knownEntryTypes` — the union of every set above. Gates `IsKnown()`, which nine
  HTTP conversation endpoints use. `channel_parity_test.go` asserts it stays the
  union, so you cannot add to one set and forget this one.

These are six independent questions (voice is viewable and taggable but is not a
messaging channel; support is a messaging channel and taggable but is not opened
through the conversation view, and has no container query), so decide each on its
own. Answering by copying another channel's memberships is how a hole gets in.

`TestEveryBoardChannelCanCarryStagesAndLabels` pins the board registry and the
tagging set together, and
[domain/shared/channel_parity_test.go](../domain/shared/channel_parity_test.go)
pins every set against every other, so forgetting one fails the build rather than
shipping a card that cannot be moved or a conversation that cannot be closed.

Nothing else in the delivery or usecase layers needs editing for the
conversation view: the websocket handlers ask
`shared.EntryType(x).SupportsConversationView()`, and the "invalid entry type"
message is generated from the set.

**Also add the message channel** in `domain/conversation/message.go`:

```go
MessageChannelTelegram MessageChannel = "telegram"
```
and include it in the `Valid()` switch, or messages will be rejected on write.

---

## 2. Domain package — `domain/telegram/`

Mirror `domain/instagram/`:

- `entity.go` — Account, Contact, Conversation, plus any channel-specific entity
- `repository.go` — repository interfaces (ports)
- `webhook.go` + normalizer — turn the provider payload into a flat, ordered list
  of events
- `service.go` — the outbound provider port

Keep provider HTTP details out of this package; it holds contracts and rules.

---

## 3. Schema and migration

- `infra/database/schema/telegram_schema.go` — GORM models
- `infra/database/migrate.go` — add the models to AutoMigrate, plus any partial
  or unique indexes

Conversation rows must carry the denormalized clocks the read paths rely on:
`last_message_at`, and (if the channel has a window) a customer-side clock such
as `last_customer_message_at`.

Reuse the shared `conversation_messages` table for the transcript — do **not**
create a per-channel message table. Set `external_message_id` to the provider's
id; the partial unique index on `(entry_type, external_message_id)` is what makes
duplicate webhook deliveries safe.

---

## 4. Repositories — `infra/repositories/telegram/`

Implement the domain ports. Include a batch contact reader:

```go
FindByIDs(ctx context.Context, ids []string) ([]*Contact, error)
```

The inbox hydrates one page of senders with a single query; a per-row lookup
would make the inbox N+1.

---

## 5. Send-side adapter — `usecases/telegram/channel_adapter.go`

Implement `domain/conversation.ChannelAdapter`:

```go
EntryType() shared.EntryType
ResolveEntry(ctx, entryID) (*EntryContext, error)
WindowState(ctx, ec) (open bool, expiresAt *time.Time, err error)
SendText(ctx, ec, req) (*SendOutcome, error)
SendMedia(ctx, ec, req) (*SendOutcome, error)
```

- `ResolveEntry` returns workspace, account, contact id and the provider-facing
  contact ref.
- `WindowState` returns `(true, nil, nil)` when the channel has no window.
  It is consulted by **both** the send path and the composer's UI state, so
  there is exactly one definition of "can I reply right now".
- `SendOutcome.ProviderMessageID` must be populated — it is stored as
  `external_message_id` so the later echo webhook reconciles against the row
  instead of inserting a duplicate.

Optional capabilities are discovered by type assertion: implement
`ReactingAdapter` and/or `PresenceAdapter` only if the provider supports them.

Once registered (step 8), `MessageSenderService` routes sends through the
adapter automatically — there is no channel switch to edit.

---

## 6. Inbound pipeline — `usecases/telegram/`

- `handle_webhook_usecase.go` — turn one normalized event into CRM state.
  Persist **through `conversation.MessageHistoryManager`** (`history.Record`),
  which owns dedup, persistence and websocket fan-out. Do not write
  `conversation_messages` directly.
- `consume_webhook_usecase.go` — subscribe queue topics using the generic
  `webhook_usecase.ConsumerRunner`, supplying only:
  - `DedupKey` — the idempotency key for one event
  - `Handle` — the handler above
  - `Classify` — retry / drop / dead-letter per failure kind

Call `assignments.EnsureAssignment(conversationID, entryType, accountID)` so the
conversation enters the round-robin pool, and `conversations.RecordInbound(...)`
so the window clock (and inbox ordering) advances.

---

## 7. Read paths — `infra/repositories/conversation/entry_sources.go`

Add **one descriptor** to `entrySources`:

```go
{
    EntryType:     shared.EntryTypeTelegram,
    From:          "telegram_conversations tgc",
    WorkspaceJoin: "JOIN telegram_accounts tga ON tga.id = tgc.account_id AND tga.workspace_id = ?",

    EntryID: "tgc.id",
    LeadID:  "tgc.contact_id",   // contact id when contacts are not leads
    Account: "COALESCE(tgc.account_id::text, '')",

    ConversationStatus: "tgc.conversation_status", // "" if the channel has none
    CampaignID:         "",                        // "" if no campaigns

    CreatedAt:     "tgc.created_at",
    UpdatedAt:     "tgc.updated_at",
    LastMessageAt: "tgc.last_message_at",
    Deleted:       "tgc.deleted_at IS NULL",

    Department: "tga.department_id", // "" if not department-scoped
}
```

This one entry feeds **both** the inbox list and the CRM board, and therefore
stages, labels and compiled filters — all of which key on
`(entry_id, entry_type)`.

Rules the registry enforces for you: workspace scoping, soft-delete, "has
messages", conversation-status filtering, department scope (**fails closed** when
`Department` is empty and the operator is department-restricted), and the
assignment scope.

`WorkspaceJoin` must contain exactly one `?`. Never interpolate values into these
strings — every value is bound. `entry_sources_test.go` asserts both properties.

Also add the type to the per-type hydration switches in
`GetEntriesWithMessages` callers (`searchEntriesByWorkspace`,
`SearchEntriesByFilter`) if your channel needs its rows hydrated there, and give
`GetEntriesWithMessages` a `case` describing its joins.

---

## 8. Container wiring — `infra/container/telegram.go`

Mirror `wireInstagramConversationStack()`:

```go
c.registerChannelAdapter(adapter)                                       // send + window
c.services.conversationAuthImpl.SetEntryAccessRepo(entryType, convRepo) // access control
c.services.conversationStatusService.SetConversationStatusStore(entryType, store)
c.services.conversationStatusService.SetConversationCounter(entryType, convRepo.CountByStatus)
resolverSetter.SetEntryOwnerResolver(entryType, convRepo)               // workspace/department
historySetter.SetContactIdentityLookup(entryType, lookup)               // sender names
```

Every one of these is keyed by `shared.EntryType` and ACCUMULATES. They were
originally Instagram-shaped single fields (`SetInstagramEntryRepo`,
`SetInstagramContacts`); the deprecated aliases still exist, but a second channel
calling them would have overwritten the first. Use the keyed form.

> **Use `c.registerChannelAdapter`.** Adapters accumulate; calling
> `SetChannelAdapters(NewAdapterRegistry(adapter))` directly would replace the
> registry and silently disable every previously wired channel's send path.

### Sender identity (channels whose contacts are not leads)

The inbox resolves display names through the lead repository. A channel whose
contacts are not leads needs its own lookup, or rows render with no name.

The port is `conversation_usecase.ContactIdentityLookup`, registered with
`HistoryProviderService.SetContactIdentityLookup(entryType, lookup)`. Hydration
iterates every registered lookup and is applied in three places — the
container-scoped inbox list, the workspace inbox list, and `GetInboxEntry` (the
`entry_update` broadcast path).

**All three are covered by `hydrateContactSenders`**, and they must stay that
way: missing the third is what caused the production bug where an Instagram
conversation's name vanished every time a new message arrived.

Supply the mapping through `contactIdentityFuncs` in
[infra/container/channel_wiring.go](../infra/container/channel_wiring.go) rather
than writing a new adapter type — only the field mapping is genuinely per
channel.

---

## 9. Delivery — `delivery/http/telegram/`

- Public webhook route (the provider calls it) — verify the signature over the
  **raw body bytes**, before any re-marshalling
- Authenticated management routes (connect account, list accounts, …)
- Register in `delivery/http/router.go` and `infra/container/router.go`

The websocket hub needs no changes: subscribe, history, search and send are all
channel-agnostic once step 1 is done.

---

## 10. Frontend — `vozko-front`

1. **`src/lib/conversations/types.ts`**
   - add `'telegram'` to the `EntryType` union
   - keep `normalizeEntryType` correct
   - declare what the channel can do in `channelCapabilities`:
     - `supportsCalling` — false unless the channel has phone numbers
     - `supportsAiHandling` — **false unless AI agents actually run on it**;
       showing the AI chip for a channel with no agent invocation promises
       automation that never happens
2. **Conversation header** (`src/components/crm/CrmLayout.tsx`) — channel avatar
   and colour
3. **i18n** — add keys to all four locales (`pt`, `en`, `de`, `es`); pt is the
   default. A missing key falls back to a humanized key path, which is a bug, not
   a design
4. **Channel management pages + sidebar entry** if the channel needs them

---

## 11. Tests to write

Mirror the existing suites — they encode the failures this system has actually
had:

| Test | Guards against |
|---|---|
| `entry_sources_test.go` — projection shape | A malformed branch breaks the UNION **for every channel**, WhatsApp included |
| placeholder/arg parity | A mismatch shifts later bound values and corrupts other channels' filters |
| workspace scoping + no interpolation | Cross-tenant leakage; SQL injection |
| department scope fails closed | A restricted operator seeing conversations they must not |
| WhatsApp-only page untouched | The new channel changing behaviour for existing tenants |
| sender hydration incl. `GetInboxEntry` | Names vanishing on `entry_update` |
| `SupportsConversationView` | Conversations that cannot be opened |
| `SupportsCRMTagging` | Board cards that cannot be staged or labelled |

Before merging, capture a **test baseline** on the untouched branch and compare:

```bash
git stash -u
go test -count=1 $(for d in cmd delivery domain infra usecases pkg; do go list ./$d/...; done) 2>&1 | grep '^FAIL' | awk '{print $2}' | sort -u > /tmp/before.txt
git stash pop
go test -count=1 $(for d in cmd delivery domain infra usecases pkg; do go list ./$d/...; done) 2>&1 | grep '^FAIL' | awk '{print $2}' | sort -u > /tmp/after.txt
comm -13 /tmp/before.txt /tmp/after.txt   # must be empty
```

`go test ./...` fails at setup (a permissions error under `data/`), which is why
the package list is enumerated explicitly. A few packages are flaky under full
parallel load — re-run a suspect package in isolation before treating it as a
regression.

---

## 12. Environment

Required variables abort boot when missing (`mustGetEnvTrimmed`), which is
correct — a half-configured channel should not start. Document every new variable
in the deploy notes, because adding one makes the new binary refuse to start
until it is set.

---

## Checklist

- [ ] `EntryType` constant + ALL SIX domain sets (§1)
- [ ] `MessageChannel` constant + `Valid()`
- [ ] Domain package (entities, ports, webhook normalizer)
- [ ] Schema + migration + indexes
- [ ] Repositories, including batch `FindByIDs`
- [ ] `ChannelAdapter` (send + window)
- [ ] Webhook handler via `MessageHistoryManager` + `ConsumerRunner`
- [ ] `entrySources` descriptor + `channelQueries` declaration
- [ ] `channelSources` (attendance), ownership query, stage subquery
- [ ] Analysis subject resolver + export lister + status counter
- [ ] Container wiring via `registerChannelAdapter` + auth/status/resolver
- [ ] Contact identity lookup wired into **all three** hydration points
- [ ] HTTP routes (public webhook + management)
- [ ] Frontend types, `channelCapabilities`, header, i18n ×4
- [ ] Tests + baseline regression comparison
- [ ] Env vars documented
