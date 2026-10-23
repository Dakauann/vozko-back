package cmfake

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type ChannelType struct {
	ID         string
	PrettyName string
}

type Category struct {
	ID   string
	Name string
}

type FormElement struct {
	ID         string
	InputKey   string
	Required   bool
	Admin      bool
	Selectable []string
	ShownAfter string
}

type ProfileStatus struct {
	EntityStatus    string
	RejectionReason string
}

func formElementID(n int) string {
	return fmt.Sprintf("0e0f0000-0000-4000-8000-%012d", n)
}

func DefaultForm() []FormElement {
	return []FormElement{
		{ID: formElementID(1), InputKey: "brandName", Required: true},
		{ID: formElementID(2), InputKey: "description", Required: true},
		{ID: formElementID(3), InputKey: "logoImage", Required: true},
		{ID: formElementID(4), InputKey: "heroImage", Required: true},
		{ID: formElementID(5), InputKey: "brandColor", Required: true},
		{ID: formElementID(6), InputKey: "website", Required: true},
		{ID: formElementID(7), InputKey: "privacyPolicyUrl", Required: true},
		{ID: formElementID(8), InputKey: "termsOfServiceUrl", Required: true},
		{ID: formElementID(9), InputKey: "contactName", Required: true},
		{ID: formElementID(10), InputKey: "contactEmail", Required: true},
		{ID: formElementID(11), InputKey: "contactPhone", Required: true},
		{ID: formElementID(12), InputKey: "billingCategory", Required: true, Selectable: []string{"CONVERSATIONAL", "NON_CONVERSATIONAL"}},
		{ID: formElementID(13), InputKey: "useCase", Required: true, Selectable: []string{"OTP", "TRANSACTIONAL", "PROMOTIONAL", "MULTI_USE"}},
		{ID: formElementID(14), InputKey: "countries", Required: true, Selectable: []string{"BR", "NL", "DE", "FR", "ES", "GB"}},
		{ID: formElementID(15), InputKey: "companyRegistrationNumber", Required: true, ShownAfter: formElementID(14)},
		{ID: formElementID(16), InputKey: "accountManagerNotes", Required: true, Admin: true},
	}
}

func (f *Fake) SetForm(elements []FormElement) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.form = append([]FormElement(nil), elements...)
}

func (f *Fake) ForceFormState(state string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.forcedState = state
}

func (f *Fake) SetChannelTypes(types []ChannelType) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.channelTypes = append([]ChannelType(nil), types...)
}

func (f *Fake) SetCategories(categories []Category) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.categories = append([]Category(nil), categories...)
}

func (f *Fake) SeedRequest(requestID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.channelReqs[requestID] = true
}

func (f *Fake) SeedProfile(requestID, profileID string, statuses ...ProfileStatus) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.channelReqs[requestID] = true
	f.profiles[profileID] = newProfile(requestID, statuses)
}

func newProfile(requestID string, statuses []ProfileStatus) *profile {
	if len(statuses) == 0 {
		statuses = []ProfileStatus{{EntityStatus: statePending}}
	}
	return &profile{requestID: requestID, statuses: append([]ProfileStatus(nil), statuses...), inputs: map[string]string{}}
}

func (f *Fake) SetProfileStatuses(profileID string, statuses ...ProfileStatus) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.profiles[profileID]
	if !ok || len(statuses) == 0 {
		panic(fmt.Sprintf("cmfake: cannot set statuses of profile %q", profileID))
	}
	p.statuses = append([]ProfileStatus(nil), statuses...)
}

func (f *Fake) FormInputs(profileID string) map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]string{}
	if p, ok := f.profiles[profileID]; ok {
		for id, v := range p.inputs {
			out[id] = v
		}
	}
	return out
}

func (f *Fake) FormInputsByKey(profileID string) map[string]string {
	inputs := f.FormInputs(profileID)
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]string{}
	for _, el := range f.form {
		if v, ok := inputs[el.ID]; ok {
			out[el.InputKey] = v
		}
	}
	return out
}

func (f *Fake) serveChannels(w http.ResponseWriter, r *http.Request, body []byte) {
	if r.Header.Get(tokenHeader) != f.ProductToken {
		writeJSON(w, http.StatusUnauthorized, problem("Unauthorized", http.StatusUnauthorized))
		return
	}
	if f.rawChannels != nil {
		writeRaw(w, f.rawChannels)
		return
	}
	rest, ok := strings.CutPrefix(r.URL.Path, channelsPrefix+f.AccountID+"/")
	if !ok {
		writeJSON(w, http.StatusNotFound, problem("Account not found", http.StatusNotFound))
		return
	}
	parts := strings.Split(rest, "/")
	route := r.Method + " " + routeShape(parts)
	switch route {
	case "GET channels/types":
		f.listChannelTypes(w)
	case "GET channels/types/*/categories":
		f.listCategories(w, parts[2])
	case "POST channels/requests":
		f.createChannelRequest(w, r.URL.Query().Get("channelTypeId"))
	case "GET channels/types/*/requestform":
		f.requestForm(w, r, parts[2])
	case "POST rcs/*/profiles":
		f.createProfile(w, parts[1])
	case "PUT rcs/*/profiles/*/forminput":
		f.addFormInput(w, parts[1], parts[3], body)
	case "GET rcs/*/profiles/*":
		f.getProfile(w, parts[1], parts[3])
	default:
		writeJSON(w, http.StatusNotFound, problem("Not Found", http.StatusNotFound))
	}
}

func routeShape(parts []string) string {
	fixed := map[string]bool{"channels": true, "types": true, "categories": true, "requests": true, "requestform": true, "rcs": true, "profiles": true, "forminput": true}
	shape := make([]string, len(parts))
	for i, p := range parts {
		if fixed[p] {
			shape[i] = p
		} else {
			shape[i] = "*"
		}
	}
	return strings.Join(shape, "/")
}

func (f *Fake) listChannelTypes(w http.ResponseWriter) {
	out := make([]map[string]any, len(f.channelTypes))
	for i, t := range f.channelTypes {
		out[i] = map[string]any{
			"id": t.ID, "currency": "EUR", "description": t.PrettyName, "prettyName": t.PrettyName,
			"iconClass": "", "startupCost": 0, "monthlyCost": 0, "channelRequestProcessingDays": 5,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (f *Fake) listCategories(w http.ResponseWriter, typeID string) {
	out := []map[string]any{}
	if typeID == RCSChannelTypeID {
		for _, c := range f.categories {
			out = append(out, map[string]any{"id": c.ID, "iconClass": "", "name": c.Name, "channelLimit": 0})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (f *Fake) createChannelRequest(w http.ResponseWriter, typeID string) {
	if typeID != RCSChannelTypeID {
		writeJSON(w, http.StatusBadRequest, problem("Unknown channel type", http.StatusBadRequest))
		return
	}
	id := newID()
	f.channelReqs[id] = true
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": id, "channelTypeId": typeID, "isPending": true, "isActive": false, "isGenerated": false,
		"createdOnUtc": time.Now().UTC().Format(time.RFC3339),
	})
}

func (f *Fake) createProfile(w http.ResponseWriter, requestID string) {
	if !f.channelReqs[requestID] {
		writeJSON(w, http.StatusNotFound, problem("Channel request not found", http.StatusNotFound))
		return
	}
	id := newID()
	f.profiles[id] = newProfile(requestID, f.progression)
	writeJSON(w, http.StatusCreated, f.profileJSON(id, f.profiles[id].statuses[0]))
}

func (f *Fake) visibleElements(p *profile) []FormElement {
	var out []FormElement
	for _, el := range f.form {
		if el.ShownAfter != "" && (p == nil || p.inputs[el.ShownAfter] == "") {
			continue
		}
		out = append(out, el)
	}
	return out
}

func (f *Fake) requestForm(w http.ResponseWriter, r *http.Request, typeID string) {
	query := r.URL.Query()
	if typeID != RCSChannelTypeID || query.Get("channelTypeCategoryId") != ProfileCategoryID {
		writeJSON(w, http.StatusBadRequest, problem("Unknown channel type or category", http.StatusBadRequest))
		return
	}
	p := f.profiles[query.Get("channelCategoryRequestId")]
	visible := f.visibleElements(p)
	state := formComplete
	if p == nil {
		state = formIncomplete
	}
	elements := make([]map[string]any, len(visible))
	for i, el := range visible {
		if el.Required && !el.Admin && (p == nil || p.inputs[el.ID] == "") {
			state = formIncomplete
		}
		elements[i] = map[string]any{
			"id": el.ID, "inputKey": el.InputKey, "inputDescriptionKey": el.InputKey + ".description",
			"inputPosition": fmt.Sprint(i), "inputType": "text", "isRequired": el.Required, "isAdminElement": el.Admin,
			"inputProperties": map[string]any{"selectable": el.Selectable, "lengthMax": 0, "lengthMin": 0},
		}
	}
	if f.forcedState != "" {
		state = f.forcedState
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"channelType":      map[string]any{"id": RCSChannelTypeID, "prettyName": "RCS", "currency": "EUR"},
		"channelCategory":  map[string]any{"id": ProfileCategoryID, "name": "Profile"},
		"formElements":     elements,
		"isDynamic":        true,
		"dynamicFormState": state,
	})
}

func (f *Fake) addFormInput(w http.ResponseWriter, requestID, profileID string, body []byte) {
	p, ok := f.profiles[profileID]
	if !ok || p.requestID != requestID {
		writeJSON(w, http.StatusNotFound, problem("Profile not found", http.StatusNotFound))
		return
	}
	var inputs []struct {
		FormElementID string `json:"formElementId"`
		InputValue    string `json:"inputValue"`
	}
	if err := json.Unmarshal(body, &inputs); err != nil {
		writeJSON(w, http.StatusBadRequest, problem("The form input must be a list", http.StatusBadRequest))
		return
	}
	known := map[string]bool{}
	for _, el := range f.visibleElements(p) {
		known[el.ID] = true
	}
	for _, in := range inputs {
		if !known[in.FormElementID] {
			writeJSON(w, http.StatusBadRequest, problem("Unknown form element "+in.FormElementID, http.StatusBadRequest))
			return
		}
	}
	for _, in := range inputs {
		p.inputs[in.FormElementID] = in.InputValue
	}
	writeJSON(w, http.StatusAccepted, f.profileJSON(profileID, p.statuses[0]))
}

func (f *Fake) getProfile(w http.ResponseWriter, requestID, profileID string) {
	p, ok := f.profiles[profileID]
	if !ok || p.requestID != requestID {
		writeJSON(w, http.StatusNotFound, problem("Profile not found", http.StatusNotFound))
		return
	}
	current := p.statuses[0]
	if len(p.statuses) > 1 {
		p.statuses = p.statuses[1:]
	}
	writeJSON(w, http.StatusOK, f.profileJSON(profileID, current))
}

func (f *Fake) profileJSON(profileID string, status ProfileStatus) map[string]any {
	p := f.profiles[profileID]
	return map[string]any{
		"id": profileID, "channelRequestId": p.requestID, "accountId": f.AccountID,
		"isActive": status.EntityStatus == "ACTIVE", "isPending": status.EntityStatus == statePending,
		"countries": []string{"BR"}, "countriesActive": []string{},
		"billingCategory": "UNKNOWN", "createdOnUtc": time.Now().UTC().Format(time.RFC3339),
		"rejectionReason": status.RejectionReason, "entityStatus": status.EntityStatus,
	}
}
