package calllisthttp

import (
	"time"

	"vozko/domain/calls/calllist"
	calllist_usecase "vozko/usecases/calls/calllist"
)

type PhoneChoiceResponse struct {
	Source string `json:"source" example:"identity" enums:"identity,contact"`
	Label  string `json:"label,omitempty" example:"landline" enums:"mobile,landline,work,message,other"`
}

type CallListResponse struct {
	ID              string              `json:"id"`
	Name            string              `json:"name" example:"Retorno de outubro"`
	Status          string              `json:"status" example:"active" enums:"building,active,paused,archived,failed"`
	CreatedBy       string              `json:"createdBy"`
	AssigneeIDs     []string            `json:"assigneeIds"`
	Phone           PhoneChoiceResponse `json:"phone"`
	Selected        int                 `json:"selected" example:"1200"`
	ItemCount       int                 `json:"itemCount" example:"1130"`
	ClosedCount     int                 `json:"closedCount" example:"240"`
	OpenCount       int                 `json:"openCount" example:"890"`
	CalledCount     int                 `json:"calledCount" example:"341"`
	CallbackCount   int                 `json:"callbackCount" example:"26"`
	AcceptsOutcomes bool                `json:"acceptsOutcomes" example:"true"`
	StatusMoves     []string            `json:"statusMoves" example:"paused,archived"`
	Skipped         map[string]int      `json:"skipped"`
	FailureCode     string              `json:"failureCode,omitempty" example:"no_callable_lead"`
	BuiltAt         *time.Time          `json:"builtAt,omitempty"`
	CreatedAt       time.Time           `json:"createdAt"`
	UpdatedAt       time.Time           `json:"updatedAt"`
}

type CallListPageResponse struct {
	Items    []CallListResponse `json:"items"`
	Total    int64              `json:"total" example:"3"`
	Page     int                `json:"page" example:"1"`
	PageSize int                `json:"pageSize" example:"20"`
}

type CallListItemResponse struct {
	ID            string     `json:"id"`
	ListID        string     `json:"listId"`
	LeadID        string     `json:"leadId"`
	LeadName      string     `json:"leadName,omitempty"`
	LeadDistrict  string     `json:"leadDistrict,omitempty" example:"Vila Mariana"`
	LeadCity      string     `json:"leadCity,omitempty" example:"São Paulo"`
	Phone         string     `json:"phone" example:"5511987654321"`
	Position      int        `json:"position" example:"12"`
	State         string     `json:"state" example:"pending" enums:"pending,reserved,closed"`
	ReservedBy    string     `json:"reservedBy,omitempty"`
	ReservedUntil *time.Time `json:"reservedUntil,omitempty"`
	Disposition   string     `json:"disposition,omitempty" example:"interessado"`
	Note          string     `json:"note,omitempty"`
	Refusal       string     `json:"refusal,omitempty" example:"blocked"`
	CallbackAt    *time.Time `json:"callbackAt,omitempty"`
	LastCallID    string     `json:"lastCallId,omitempty"`
	Outcome       string     `json:"outcome,omitempty" example:"answered" enums:"answered,missed,no_answer,busy,declined,cancelled,failed,in_progress"`
	Attempts      int        `json:"attempts,omitempty" example:"2"`
	ClosedBy      string     `json:"closedBy,omitempty"`
	ClosedAt      *time.Time `json:"closedAt,omitempty"`
	Closable      bool       `json:"closable" example:"false"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

type CallListItemPageResponse struct {
	Items  []CallListItemResponse `json:"items"`
	Next   int                    `json:"next,omitempty" example:"50"`
	NextAt *time.Time             `json:"nextAt,omitempty"`
	AsOf   *time.Time             `json:"asOf,omitempty"`
}

type CallListLeadResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty" example:"Maria Souza"`
	District    string `json:"district,omitempty" example:"Vila Mariana"`
	City        string `json:"city,omitempty" example:"São Paulo"`
	FamilyCount int    `json:"familyCount" example:"3"`
}

type CallListInteractionResponse struct {
	EntryID   string    `json:"entryId"`
	EntryType string    `json:"entryType" example:"whatsapp"`
	At        time.Time `json:"at"`
}

type CallListTrunkResponse struct {
	ID   string `json:"id"`
	Name string `json:"name" example:"Matriz"`
}

type CallListNextResponse struct {
	List            CallListResponse             `json:"list"`
	Item            *CallListItemResponse        `json:"item,omitempty"`
	Lead            *CallListLeadResponse        `json:"lead,omitempty"`
	LastInteraction *CallListInteractionResponse `json:"lastInteraction,omitempty"`
	Trunks          []CallListTrunkResponse      `json:"trunks"`
	TrunkRefusal    string                       `json:"trunkRefusal,omitempty" example:"no_dialable_trunk" enums:"unauthorized,no_dialable_trunk"`
	Refused         int                          `json:"refused" example:"0"`
	More            bool                         `json:"more" example:"false"`
}

type UpdateCallListRequest struct {
	Name        *string   `json:"name,omitempty" example:"Retorno de novembro"`
	AssigneeIDs *[]string `json:"assigneeIds,omitempty"`
	Status      *string   `json:"status,omitempty" example:"paused" enums:"active,paused,archived"`
}

type CloseCallListItemRequest struct {
	Disposition string     `json:"disposition" example:"interessado"`
	Note        string     `json:"note,omitempty" example:"Pediu retorno depois das 18h"`
	CallbackAt  *time.Time `json:"callbackAt,omitempty"`
}

func ViewResponseOf(v calllist_usecase.ListView) CallListResponse {
	return listResponse(v.List, v.Verdict)
}

func atLeastZero(count int) int {
	if count < 0 {
		return 0
	}
	return count
}

func listResponse(l *calllist.List, verdict calllist.ListVerdict) CallListResponse {
	skipped := make(map[string]int, len(l.Skipped))
	for reason, count := range l.Skipped {
		skipped[string(reason)] = count
	}
	assignees := l.AssigneeIDs
	if assignees == nil {
		assignees = []string{}
	}
	moves := make([]string, 0, len(verdict.StatusMoves))
	for _, status := range verdict.StatusMoves {
		moves = append(moves, string(status))
	}
	return CallListResponse{
		ID: l.ID, Name: l.Name, Status: string(l.Status), CreatedBy: l.CreatedBy, AssigneeIDs: assignees,
		Phone:    PhoneChoiceResponse{Source: string(l.Phone.Source), Label: string(l.Phone.Label)},
		Selected: l.Selected, ItemCount: l.ItemCount,
		ClosedCount: l.ClosedCount, OpenCount: atLeastZero(l.ItemCount - l.ClosedCount), Skipped: skipped, FailureCode: l.FailureCode,
		CalledCount: atLeastZero(l.CalledCount), CallbackCount: atLeastZero(l.CallbackCount), AcceptsOutcomes: verdict.AcceptsOutcomes,
		StatusMoves: moves, BuiltAt: l.BuiltAt, CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt,
	}
}

func itemResponseOf(i *calllist.Item) CallListItemResponse {
	return CallListItemResponse{
		ID: i.ID, ListID: i.ListID, LeadID: i.LeadID, Phone: i.Phone, Position: i.Position, State: string(i.State),
		ReservedBy: i.ReservedBy, ReservedUntil: i.ReservedUntil, Disposition: i.Disposition, Note: i.Note, Refusal: i.Refusal,
		CallbackAt: i.CallbackAt, LastCallID: i.LastCallID, ClosedBy: i.ClosedBy, ClosedAt: i.ClosedAt,
		CreatedAt: i.CreatedAt, UpdatedAt: i.UpdatedAt,
	}
}

func verdictResponseOf(v calllist_usecase.ItemVerdict) CallListItemResponse {
	out := itemResponseOf(v.Item)
	out.Closable = v.Closable
	return out
}

func rowResponseOf(row calllist_usecase.ItemRow) CallListItemResponse {
	out := itemResponseOf(&row.Item)
	out.LeadName, out.Outcome, out.Attempts = row.LeadRealName(), string(row.Outcome), row.Attempts
	out.LeadDistrict, out.LeadCity, out.Closable = row.LeadDistrict, row.LeadCity, row.Closable
	return out
}

func nextResponseOf(n *calllist_usecase.NextResult) CallListNextResponse {
	out := CallListNextResponse{List: listResponse(n.List, n.Verdict), Trunks: []CallListTrunkResponse{}, TrunkRefusal: string(n.TrunkRefusal), Refused: n.Refused, More: n.More}
	if n.Item == nil {
		return out
	}
	item := itemResponseOf(n.Item)
	item.LeadName, item.LeadDistrict, item.LeadCity, item.Closable = n.Lead.Name, n.Lead.District, n.Lead.City, n.Closable
	out.Item = &item
	out.Lead = &CallListLeadResponse{ID: n.Lead.ID, Name: n.Lead.Name, District: n.Lead.District, City: n.Lead.City, FamilyCount: n.Lead.FamilyCount}
	if n.LastInteraction != nil {
		out.LastInteraction = &CallListInteractionResponse{EntryID: n.LastInteraction.EntryID, EntryType: string(n.LastInteraction.EntryType), At: n.LastInteraction.At}
	}
	for _, trunk := range n.Trunks {
		out.Trunks = append(out.Trunks, CallListTrunkResponse{ID: trunk.ID, Name: trunk.Name})
	}
	return out
}
