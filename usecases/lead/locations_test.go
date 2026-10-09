package lead_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/address"
	"vozko/domain/conversation"
	"vozko/domain/geo"
	"vozko/domain/lead"
	"vozko/domain/shared"
)

const (
	locLead      = "6f1c2a3b-4d5e-4f60-8a9b-0c1d2e3f4a5b"
	locOtherLead = "7a2b3c4d-5e6f-4071-9b0c-1d2e3f4a5b6c"
	locMessage   = "8b3c4d5e-6f70-4182-8c1d-2e3f4a5b6c7d"
	locEntry     = "9c4d5e6f-7081-4293-9d2e-3f4a5b6c7d8e"
)

var (
	locPoint = geo.Point{Lat: -23.55052, Lng: -46.633308}
	locHome  = address.Postal{ZipCode: "01310100", Street: "Avenida Paulista", Number: "1000", District: "Bela Vista", City: "São Paulo", State: "SP"}
)

type fakeMessages struct {
	byID  map[string]*conversation.Message
	err   error
	asked []string
}

func (m *fakeMessages) GetByID(id string) (*conversation.Message, error) {
	m.asked = append(m.asked, id)
	if m.err != nil {
		return nil, m.err
	}
	if msg, ok := m.byID[id]; ok {
		return msg, nil
	}
	return nil, conversation.ErrMessageNotFound
}

type nilEntryVisibility struct{}

func (nilEntryVisibility) EntryVisibilityFor(string, string, bool) conversation.EntryVisibility {
	return nil
}

type entryLeadsByEntry struct {
	leads map[string]string
	err   error
}

func (e entryLeadsByEntry) LeadOfEntry(_ context.Context, _ string, ref shared.EntryRef) (string, error) {
	if e.err != nil {
		return "", e.err
	}
	if id, ok := e.leads[ref.EntryID+"/"+string(ref.EntryType)]; ok {
		return id, nil
	}
	return "", lead.ErrLeadNotFound
}

func sentLocation(point geo.Point) *conversation.Message {
	place := &conversation.WhatsAppLocation{Latitude: point.Lat, Longitude: point.Lng, Name: "Casa"}
	return &conversation.Message{
		ID: locMessage, EntryID: locEntry, EntryType: shared.EntryTypeWhatsApp, Channel: conversation.MessageChannelWhatsApp,
		MessageType: conversation.MessageTypeUserMessage, SentBy: conversation.SentByContact("5511999990000"),
		Text: place.Text(), Metadata: place.Metadata(),
	}
}

type locationsFixture struct {
	store    *fakeStore
	messages *fakeMessages
	access   *accessResolver
	notifier *fakeNotifier
	entries  entryLeadsByEntry
	perms    fakePermissions
}

func newLocationsFixture(leads ...*lead.Lead) *locationsFixture {
	return &locationsFixture{
		store:    newFakeStore(leads...),
		messages: &fakeMessages{byID: map[string]*conversation.Message{locMessage: sentLocation(locPoint)}},
		access:   &accessResolver{byUser: map[string]map[string]bool{cmdUser: {locEntry: true}}},
		notifier: &fakeNotifier{},
		entries:  entryLeadsByEntry{leads: map[string]string{locEntry + "/whatsapp": locLead}},
		perms:    fakePermissions{"leads:read": true, "leads:update": true, "leads:read_addresses": true},
	}
}

func (f *locationsFixture) locations(t *testing.T, access EntryAccessResolver) *Locations {
	t.Helper()
	if access == nil {
		access = f.access
	}
	l, err := NewLocations(LocationDeps{
		Store: f.store, Permissions: f.perms, Definitions: &fakeDefinitions{defs: leadDefinitions()}, Notifier: f.notifier,
		Messages: f.messages, Access: access, EntryLeads: f.entries, Now: func() time.Time { return cmdNow },
	})
	if err != nil {
		t.Fatalf("NewLocations: %v", err)
	}
	return l
}

func locatedLead(addresses ...lead.Address) *lead.Lead {
	return &lead.Lead{ID: locLead, WorkspaceID: cmdWorkspace, Name: "Maria", Number: "5511999990000", Version: 4,
		Phones: []lead.ContactPhone{}, Addresses: append([]lead.Address{}, addresses...)}
}

func homeAddress(fix *geo.Fix, status lead.GeoStatus) lead.Address {
	return lead.Address{ID: "a-1", Label: lead.AddressHome, Primary: true, Postal: locHome, Fix: fix, GeoStatus: status}
}

var locActor = Actor{UserID: cmdUser, WorkspaceID: cmdWorkspace}

func TestAcceptLocationStoresTheLeadPinOnThePrimaryAddress(t *testing.T) {
	reference := geo.Fix{Point: geo.Point{Lat: -23.56, Lng: -46.65}, Precision: geo.PrecisionPostalCode, Source: geo.SourceReference, FixedAt: cmdNow}
	f := newLocationsFixture(locatedLead(homeAddress(&reference, lead.GeoApproximate)))
	got, err := f.locations(t, nil).AcceptLocation(context.Background(), locActor, locLead, locMessage)
	if err != nil {
		t.Fatalf("AcceptLocation: %v", err)
	}
	stored := f.store.leads[locLead]
	fix := stored.Addresses[0].Fix
	if fix == nil || fix.Point != locPoint || fix.Source != geo.SourceLeadPin || fix.Precision != geo.PrecisionExact || !fix.FixedAt.Equal(cmdNow) {
		t.Fatalf("stored fix = %+v", fix)
	}
	if stored.Addresses[0].GeoStatus != lead.GeoLocated || stored.Version != 5 || got.Version != 5 {
		t.Fatalf("stored = %+v, returned version %d", stored.Addresses[0], got.Version)
	}
	if len(f.store.saves) != 1 || len(f.store.saves[0].events) != 1 || f.store.saves[0].events[0].Kind != lead.EventLocationAccepted || f.store.saves[0].events[0].Actor != cmdUser {
		t.Fatalf("saves = %+v", f.store.saves)
	}
	if len(f.notifier.changes) != 1 || f.notifier.changes[0].Fields[0] != lead.FieldAddresses {
		t.Fatalf("changes = %+v", f.notifier.changes)
	}
	if got.Addresses[0].Fix == nil {
		t.Fatalf("a reader of addresses sees the fix: %+v", got.Addresses)
	}
}

func TestAcceptLocationGivesALeadWithoutAnAddressItsFirstOne(t *testing.T) {
	f := newLocationsFixture(locatedLead())
	delete(f.perms, "leads:read_addresses")
	got, err := f.locations(t, nil).AcceptLocation(context.Background(), locActor, locLead, locMessage)
	if err != nil {
		t.Fatalf("AcceptLocation: %v", err)
	}
	stored := f.store.leads[locLead].Addresses
	if len(stored) != 1 || !stored[0].Primary || stored[0].Fix == nil || stored[0].Fix.Point != locPoint || stored[0].ID == "" {
		t.Fatalf("stored = %+v", stored)
	}
	if len(got.Addresses) != 1 || got.Addresses[0].Fix != nil || got.Addresses[0].GeoStatus != lead.GeoLocated {
		t.Fatalf("a member without leads.read_addresses sees only the status: %+v", got.Addresses)
	}
}

func TestAcceptLocationTwiceWritesOnce(t *testing.T) {
	f := newLocationsFixture(locatedLead(homeAddress(nil, lead.GeoPending)))
	uc := f.locations(t, nil)
	for i := 0; i < 2; i++ {
		if _, err := uc.AcceptLocation(context.Background(), locActor, locLead, locMessage); err != nil {
			t.Fatalf("AcceptLocation #%d: %v", i, err)
		}
	}
	if len(f.store.saves) != 1 {
		t.Fatalf("saves = %d, want 1", len(f.store.saves))
	}
}

func TestAcceptLocationRefusals(t *testing.T) {
	abroad := sentLocation(geo.Point{Lat: 38.7223, Lng: -9.1393})
	fromOperator := sentLocation(locPoint)
	fromOperator.SentBy, fromOperator.MessageType = conversation.SentByPerson(cmdUser), conversation.MessageTypeOperator
	plain := sentLocation(locPoint)
	plain.Text, plain.Metadata = "oi", nil
	cases := []struct {
		name    string
		arrange func(f *locationsFixture)
		actor   Actor
		leadID  string
		message string
		access  EntryAccessResolver
		wantErr error
	}{
		{name: "without leads:update", arrange: func(f *locationsFixture) { delete(f.perms, "leads:update") }, wantErr: lead.ErrLeadForbidden},
		{name: "without a workspace", actor: Actor{UserID: cmdUser}, wantErr: lead.ErrLeadWorkspaceRequired},
		{name: "without a user", actor: Actor{WorkspaceID: cmdWorkspace}, wantErr: lead.ErrLeadForbidden},
		{name: "a message id that is not one", message: "abc", wantErr: lead.ErrLocationNotFound},
		{name: "a message that does not exist", message: "0d5e6f70-8192-43a4-8e3f-4a5b6c7d8e9f", wantErr: lead.ErrLocationNotFound},
		{name: "a text message", arrange: func(f *locationsFixture) { f.messages.byID[locMessage] = plain }, wantErr: lead.ErrLocationNotFound},
		{name: "a location an operator sent", arrange: func(f *locationsFixture) { f.messages.byID[locMessage] = fromOperator }, wantErr: lead.ErrLocationNotFound},
		{name: "a location outside Brazil", arrange: func(f *locationsFixture) { f.messages.byID[locMessage] = abroad }, wantErr: lead.ErrLocationNotFound},
		{name: "a conversation the member cannot open", arrange: func(f *locationsFixture) { f.access.byUser[cmdUser] = map[string]bool{} }, wantErr: lead.ErrLocationNotFound},
		{name: "no access port answer", access: nilEntryVisibility{}, wantErr: errHistoryIncomplete},
		{name: "the conversation of another lead", leadID: locOtherLead, wantErr: lead.ErrLocationNotFound},
		{name: "a conversation without a lead in this workspace", arrange: func(f *locationsFixture) { f.entries.leads = map[string]string{} }, wantErr: lead.ErrLocationNotFound},
		{name: "a lead of another workspace", arrange: func(f *locationsFixture) { f.store.leads[locLead].WorkspaceID = "ws-2" }, wantErr: lead.ErrLeadNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newLocationsFixture(locatedLead(homeAddress(nil, lead.GeoPending)), &lead.Lead{ID: locOtherLead, WorkspaceID: cmdWorkspace, Name: "Ana", Version: 1, Phones: []lead.ContactPhone{}, Addresses: []lead.Address{}})
			if tc.arrange != nil {
				tc.arrange(f)
			}
			a := locActor
			if tc.actor != (Actor{}) {
				a = tc.actor
			}
			leadID, message := locLead, locMessage
			if tc.leadID != "" {
				leadID = tc.leadID
			}
			if tc.message != "" {
				message = tc.message
			}
			_, err := f.locations(t, tc.access).AcceptLocation(context.Background(), a, leadID, message)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("AcceptLocation() error = %v, want %v", err, tc.wantErr)
			}
			if len(f.store.saves) != 0 || len(f.notifier.changes) != 0 {
				t.Fatalf("a refused accept wrote: %+v", f.store.saves)
			}
		})
	}
}

func TestAcceptLocationPassesStorageErrorsThrough(t *testing.T) {
	f := newLocationsFixture(locatedLead())
	f.messages.err = errors.New("database down")
	_, err := f.locations(t, nil).AcceptLocation(context.Background(), locActor, locLead, locMessage)
	if err == nil || errors.Is(err, lead.ErrLocationNotFound) {
		t.Fatalf("a storage error is not a missing location: %v", err)
	}
	f = newLocationsFixture(locatedLead())
	f.entries.err = errors.New("database down")
	_, err = f.locations(t, nil).AcceptLocation(context.Background(), locActor, locLead, locMessage)
	if err == nil || errors.Is(err, lead.ErrLocationNotFound) {
		t.Fatalf("a storage error is not a missing location: %v", err)
	}
}

func TestPinLocationStoresAPersonsPin(t *testing.T) {
	f := newLocationsFixture(locatedLead(homeAddress(nil, lead.GeoPending)))
	got, err := f.locations(t, nil).PinLocation(context.Background(), locActor, locLead, "a-1", locPoint)
	if err != nil {
		t.Fatalf("PinLocation: %v", err)
	}
	fix := f.store.leads[locLead].Addresses[0].Fix
	if fix == nil || fix.Point != locPoint || fix.Source != geo.SourceManual || fix.Precision != geo.PrecisionExact {
		t.Fatalf("stored fix = %+v", fix)
	}
	if got.Addresses[0].Fix == nil || f.store.saves[0].events[0].Kind != lead.EventLocationPinned {
		t.Fatalf("returned = %+v, saves = %+v", got.Addresses, f.store.saves)
	}
}

func TestPinLocationRefusals(t *testing.T) {
	cases := []struct {
		name      string
		arrange   func(f *locationsFixture)
		addressID string
		point     *geo.Point
		wantErr   error
	}{
		{name: "without leads:update", arrange: func(f *locationsFixture) { delete(f.perms, "leads:update") }, wantErr: lead.ErrLeadForbidden},
		{name: "without leads:read_addresses", arrange: func(f *locationsFixture) { delete(f.perms, "leads:read_addresses") }, wantErr: lead.ErrLeadForbidden},
		{name: "an address of another lead", addressID: "a-9", wantErr: lead.ErrAddressNotFound},
		{name: "the zero point", point: &geo.Point{}, wantErr: lead.ErrLocationInvalid},
		{name: "outside Brazil", point: &geo.Point{Lat: 38.7223, Lng: -9.1393}, wantErr: lead.ErrLocationInvalid},
		{name: "a lead of another workspace", arrange: func(f *locationsFixture) { f.store.leads[locLead].WorkspaceID = "ws-2" }, wantErr: lead.ErrLeadNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newLocationsFixture(locatedLead(homeAddress(nil, lead.GeoPending)))
			if tc.arrange != nil {
				tc.arrange(f)
			}
			addressID, point := "a-1", locPoint
			if tc.addressID != "" {
				addressID = tc.addressID
			}
			if tc.point != nil {
				point = *tc.point
			}
			_, err := f.locations(t, nil).PinLocation(context.Background(), locActor, locLead, addressID, point)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("PinLocation() error = %v, want %v", err, tc.wantErr)
			}
			if len(f.store.saves) != 0 {
				t.Fatalf("a refused pin wrote: %+v", f.store.saves)
			}
		})
	}
}

func TestNewLocationsRefusesAMissingDependency(t *testing.T) {
	f := newLocationsFixture()
	complete := LocationDeps{Store: f.store, Permissions: f.perms, Definitions: &fakeDefinitions{}, Notifier: f.notifier,
		Messages: f.messages, Access: f.access, EntryLeads: f.entries}
	for name, strip := range map[string]func(d *LocationDeps){
		"store":       func(d *LocationDeps) { d.Store = nil },
		"permissions": func(d *LocationDeps) { d.Permissions = nil },
		"definitions": func(d *LocationDeps) { d.Definitions = nil },
		"notifier":    func(d *LocationDeps) { d.Notifier = nil },
		"messages":    func(d *LocationDeps) { d.Messages = nil },
		"access":      func(d *LocationDeps) { d.Access = nil },
		"entry leads": func(d *LocationDeps) { d.EntryLeads = nil },
	} {
		deps := complete
		strip(&deps)
		if _, err := NewLocations(deps); err == nil {
			t.Errorf("NewLocations without %s was accepted", name)
		}
	}
	if _, err := NewLocations(complete); err != nil {
		t.Fatalf("NewLocations(complete) = %v", err)
	}
}
