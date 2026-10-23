package route53fake

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
)

const (
	apiPrefix       = "/2013-04-01/"
	xmlns           = "https://route53.amazonaws.com/doc/2013-04-01/"
	defaultMaxItems = 300
)

var mxValue = regexp.MustCompile(`^\d+ \S+$`)

type RecordSet struct {
	Name   string
	Type   string
	TTL    int64
	Values []string
}

type Request struct {
	Method string
	Path   string
	Query  string
	Body   string
}

type refusal struct {
	status      int
	code        string
	message     string
	changesOnly bool
}

type zone struct {
	id   string
	name string
	sets []RecordSet
}

type Server struct {
	mu       sync.Mutex
	zones    map[string]*zone
	requests []Request
	refusals []refusal
	changes  int
}

func New() *Server {
	return &Server{zones: map[string]*zone{}}
}

func (s *Server) AddZone(id, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.zones[id] = &zone{id: id, name: canonicalName(name)}
}

func (s *Server) Put(zoneID string, set RecordSet) {
	s.mu.Lock()
	defer s.mu.Unlock()
	z := s.zones[zoneID]
	set.Name = canonicalName(set.Name)
	z.sets = append(slices.DeleteFunc(z.sets, func(r RecordSet) bool { return r.Name == set.Name && r.Type == set.Type }), set)
}

func (s *Server) RecordSets(zoneID string) []RecordSet {
	s.mu.Lock()
	defer s.mu.Unlock()
	z, ok := s.zones[zoneID]
	if !ok {
		return nil
	}
	out := make([]RecordSet, 0, len(z.sets))
	for _, set := range z.sets {
		set.Values = slices.Clone(set.Values)
		out = append(out, set)
	}
	return out
}

func (s *Server) RecordSetsUnder(zoneID, name string) []RecordSet {
	suffix := canonicalName(name)
	return slices.DeleteFunc(s.RecordSets(zoneID), func(set RecordSet) bool {
		return set.Name != suffix && !strings.HasSuffix(set.Name, "."+suffix)
	})
}

func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.requests)
}

func (s *Server) Changes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.changes
}

func (s *Server) RefuseNext(status int, code, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refusals = append(s.refusals, refusal{status: status, code: code, message: message})
}

func (s *Server) RefuseNextChange(status int, code, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refusals = append(s.refusals, refusal{status: status, code: code, message: message, changesOnly: true})
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body := readBody(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, Request{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Body: body})
	if len(s.refusals) > 0 && (!s.refusals[0].changesOnly || r.Method == http.MethodPost) {
		next := s.refusals[0]
		s.refusals = s.refusals[1:]
		writeError(w, next.status, next.code, next.message)
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, apiPrefix), "/"), "/")
	if len(parts) < 2 || parts[0] != "hostedzone" {
		writeError(w, http.StatusNotFound, "NoSuchHostedZone", "unknown path "+r.URL.Path)
		return
	}
	z, ok := s.zones[parts[1]]
	if !ok {
		writeError(w, http.StatusNotFound, "NoSuchHostedZone", "No hosted zone found with ID: "+parts[1])
		return
	}
	switch {
	case r.Method == http.MethodGet && len(parts) == 2:
		s.getZone(w, z)
	case r.Method == http.MethodGet && len(parts) == 3 && parts[2] == "rrset":
		s.list(w, r, z)
	case r.Method == http.MethodPost && len(parts) == 3 && parts[2] == "rrset":
		s.change(w, body, z)
	default:
		writeError(w, http.StatusBadRequest, "InvalidInput", "unsupported request "+r.Method+" "+r.URL.Path)
	}
}

type xmlHostedZone struct {
	ID              string `xml:"Id"`
	Name            string `xml:"Name"`
	CallerReference string `xml:"CallerReference"`
}

type getZoneResponse struct {
	XMLName     xml.Name      `xml:"GetHostedZoneResponse"`
	Xmlns       string        `xml:"xmlns,attr"`
	HostedZone  xmlHostedZone `xml:"HostedZone"`
	NameServers []string      `xml:"DelegationSet>NameServers>NameServer"`
}

func (s *Server) getZone(w http.ResponseWriter, z *zone) {
	writeXML(w, http.StatusOK, getZoneResponse{
		Xmlns:       xmlns,
		HostedZone:  xmlHostedZone{ID: "/hostedzone/" + z.id, Name: z.name, CallerReference: "route53fake-" + z.id},
		NameServers: []string{"ns-1.route53fake.test"},
	})
}

type xmlRecord struct {
	Value string `xml:"Value"`
}

type xmlRecordSet struct {
	Name    string      `xml:"Name"`
	Type    string      `xml:"Type"`
	TTL     *int64      `xml:"TTL,omitempty"`
	Records []xmlRecord `xml:"ResourceRecords>ResourceRecord"`
}

type listResponse struct {
	XMLName        xml.Name       `xml:"ListResourceRecordSetsResponse"`
	Xmlns          string         `xml:"xmlns,attr"`
	Sets           []xmlRecordSet `xml:"ResourceRecordSets>ResourceRecordSet"`
	IsTruncated    bool           `xml:"IsTruncated"`
	NextRecordName string         `xml:"NextRecordName,omitempty"`
	NextRecordType string         `xml:"NextRecordType,omitempty"`
	MaxItems       int            `xml:"MaxItems"`
}

func (s *Server) list(w http.ResponseWriter, r *http.Request, z *zone) {
	maxItems := defaultMaxItems
	if raw := r.URL.Query().Get("maxitems"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "InvalidInput", "maxitems")
			return
		}
		maxItems = min(n, defaultMaxItems)
	}
	startName := r.URL.Query().Get("name")
	startType := r.URL.Query().Get("type")
	if startType != "" && startName == "" {
		writeError(w, http.StatusBadRequest, "InvalidInput", "type requires name")
		return
	}
	ordered := slices.Clone(z.sets)
	slices.SortFunc(ordered, func(a, b RecordSet) int { return strings.Compare(sortKey(a.Name, a.Type), sortKey(b.Name, b.Type)) })
	start := ""
	if startName != "" {
		start = sortKey(canonicalName(startName), startType)
	}
	resp := listResponse{Xmlns: xmlns, MaxItems: maxItems}
	for _, set := range ordered {
		if sortKey(set.Name, set.Type) < start {
			continue
		}
		if len(resp.Sets) == maxItems {
			resp.IsTruncated, resp.NextRecordName, resp.NextRecordType = true, set.Name, set.Type
			break
		}
		resp.Sets = append(resp.Sets, toXML(set))
	}
	writeXML(w, http.StatusOK, resp)
}

type xmlChange struct {
	Action string       `xml:"Action"`
	Set    xmlRecordSet `xml:"ResourceRecordSet"`
}

type changeRequest struct {
	XMLName xml.Name    `xml:"ChangeResourceRecordSetsRequest"`
	Changes []xmlChange `xml:"ChangeBatch>Changes>Change"`
}

type changeResponse struct {
	XMLName    xml.Name `xml:"ChangeResourceRecordSetsResponse"`
	Xmlns      string   `xml:"xmlns,attr"`
	ID         string   `xml:"ChangeInfo>Id"`
	Status     string   `xml:"ChangeInfo>Status"`
	SubmitTime string   `xml:"ChangeInfo>SubmittedAt"`
}

func (s *Server) change(w http.ResponseWriter, body string, z *zone) {
	var req changeRequest
	if err := xml.Unmarshal([]byte(body), &req); err != nil || len(req.Changes) == 0 {
		writeError(w, http.StatusBadRequest, "InvalidInput", "malformed change batch")
		return
	}
	next := slices.Clone(z.sets)
	var problems []string
	for _, c := range req.Changes {
		set, problem := fromXML(c.Set, z.name)
		if problem != "" {
			problems = append(problems, problem)
			continue
		}
		at := slices.IndexFunc(next, func(r RecordSet) bool { return r.Name == set.Name && r.Type == set.Type })
		switch c.Action {
		case "CREATE":
			if at >= 0 {
				problems = append(problems, fmt.Sprintf("Tried to create resource record set [name='%s', type='%s'] but it already exists", set.Name, set.Type))
				continue
			}
			next = append(next, set)
		case "UPSERT":
			if at >= 0 {
				next[at] = set
				continue
			}
			next = append(next, set)
		case "DELETE":
			if at < 0 {
				problems = append(problems, fmt.Sprintf("Tried to delete resource record set [name='%s', type='%s'] but it was not found", set.Name, set.Type))
				continue
			}
			if next[at].TTL != set.TTL || !slices.Equal(sorted(next[at].Values), sorted(set.Values)) {
				problems = append(problems, fmt.Sprintf("Tried to delete resource record set [name='%s', type='%s'] but the values provided do not match the current values", set.Name, set.Type))
				continue
			}
			next = slices.Delete(next, at, at+1)
		default:
			problems = append(problems, "unknown action "+c.Action)
		}
	}
	if len(problems) > 0 {
		writeInvalidChangeBatch(w, problems)
		return
	}
	z.sets = next
	s.changes++
	writeXML(w, http.StatusOK, changeResponse{Xmlns: xmlns, ID: fmt.Sprintf("/change/C%04d", s.changes), Status: "PENDING", SubmitTime: "2026-10-22T12:00:00.000Z"})
}

func fromXML(in xmlRecordSet, zoneName string) (RecordSet, string) {
	set := RecordSet{Name: canonicalName(in.Name), Type: in.Type}
	if set.Name != zoneName && !strings.HasSuffix(set.Name, "."+zoneName) {
		return set, fmt.Sprintf("RRSet with DNS name %s is not permitted in zone %s", set.Name, zoneName)
	}
	if in.TTL == nil {
		return set, fmt.Sprintf("Invalid request: Missing field 'TTL' in RRSet with DNS name %s and type %s", set.Name, set.Type)
	}
	set.TTL = *in.TTL
	if len(in.Records) == 0 {
		return set, fmt.Sprintf("Invalid request: Expected exactly one of [AliasTarget, all of [TTL, and ResourceRecords]] for %s", set.Name)
	}
	for _, rec := range in.Records {
		switch set.Type {
		case "TXT":
			if !strings.HasPrefix(rec.Value, `"`) || !strings.HasSuffix(rec.Value, `"`) {
				return set, "Invalid Resource Record: 'FATAL problem: InvalidCharacterString (Value should be enclosed in quotation marks) encountered with '" + rec.Value + "''"
			}
		case "MX":
			if !mxValue.MatchString(rec.Value) {
				return set, "Invalid Resource Record: 'FATAL problem: MXRRDATANotTwoFields encountered with '" + rec.Value + "''"
			}
		case "CNAME":
			if len(in.Records) > 1 {
				return set, "RRSet of type CNAME with DNS name " + set.Name + " has more than one record"
			}
		default:
			return set, "unsupported type " + set.Type
		}
		if slices.Contains(set.Values, rec.Value) {
			return set, "Duplicate Resource Record: '" + rec.Value + "'"
		}
		set.Values = append(set.Values, rec.Value)
	}
	return set, ""
}

func toXML(set RecordSet) xmlRecordSet {
	ttl := set.TTL
	out := xmlRecordSet{Name: set.Name, Type: set.Type, TTL: &ttl}
	for _, v := range set.Values {
		out.Records = append(out.Records, xmlRecord{Value: v})
	}
	return out
}

func sortKey(name, recordType string) string {
	labels := strings.Split(strings.TrimSuffix(name, "."), ".")
	slices.Reverse(labels)
	return strings.Join(labels, ".") + "\x00" + recordType
}

func canonicalName(name string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), ".")) + "."
}

func sorted(values []string) []string {
	out := slices.Clone(values)
	slices.Sort(out)
	return out
}

type errorResponse struct {
	XMLName   xml.Name `xml:"ErrorResponse"`
	Xmlns     string   `xml:"xmlns,attr"`
	Type      string   `xml:"Error>Type"`
	Code      string   `xml:"Error>Code"`
	Message   string   `xml:"Error>Message"`
	RequestID string   `xml:"RequestId"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	if code == "InvalidChangeBatch" {
		writeInvalidChangeBatch(w, []string{message})
		return
	}
	writeXML(w, status, errorResponse{Xmlns: xmlns, Type: "Sender", Code: code, Message: message, RequestID: "route53fake"})
}

type invalidChangeBatch struct {
	XMLName   xml.Name `xml:"InvalidChangeBatch"`
	Xmlns     string   `xml:"xmlns,attr"`
	Messages  []string `xml:"Messages>Message"`
	RequestID string   `xml:"RequestId"`
}

func writeInvalidChangeBatch(w http.ResponseWriter, messages []string) {
	writeXML(w, http.StatusBadRequest, invalidChangeBatch{Xmlns: xmlns, Messages: messages, RequestID: "route53fake"})
}

func writeXML(w http.ResponseWriter, status int, v any) {
	data, err := xml.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(status)
	_, _ = w.Write(append([]byte(xml.Header), data...))
}

func readBody(r *http.Request) string {
	if r.Body == nil {
		return ""
	}
	defer r.Body.Close()
	data, _ := io.ReadAll(r.Body)
	return string(data)
}
