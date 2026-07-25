package sip_trunk

import (
	"net"
	"strconv"
	"strings"
	"time"
)

type TrunkType string

const (
	TrunkTypeMobile TrunkType = "mobile"
	TrunkTypeFixed  TrunkType = "fixed"
)

func (t TrunkType) IsValid() bool {
	switch t {
	case TrunkTypeMobile, TrunkTypeFixed:
		return true
	default:
		return false
	}
}

type RegistrationStatus string

const (
	RegistrationStatusPending      RegistrationStatus = "pending"
	RegistrationStatusRegistering  RegistrationStatus = "registering"
	RegistrationStatusRegistered   RegistrationStatus = "registered"
	RegistrationStatusFailed       RegistrationStatus = "failed"
	RegistrationStatusUnregistered RegistrationStatus = "unregistered"
)

type TransportType string

const (
	TransportUDP TransportType = "udp"
	TransportTCP TransportType = "tcp"
	TransportTLS TransportType = "tls"
)

func (t TransportType) IsValid() bool {
	switch t {
	case TransportUDP, TransportTCP, TransportTLS:
		return true
	default:
		return false
	}
}

type CodecID string

const (
	// Deprecated: Opus and G722 are no longer implemented in the media plane and
	// are never offered/selected. The constants remain only so legacy persisted
	// trunk configs still validate; they are silently ignored at runtime.
	// See docs/SIP_AUDIO_PIPELINE.md §9.1.
	CodecIDOpus CodecID = "opus"
	CodecIDG722 CodecID = "g722"

	CodecIDPCMU           CodecID = "pcmu"
	CodecIDPCMA           CodecID = "pcma"
	CodecIDTelephoneEvent CodecID = "telephone-event"
)

func (c CodecID) IsValid() bool {
	switch c {
	case CodecIDOpus, CodecIDG722, CodecIDPCMU, CodecIDPCMA, CodecIDTelephoneEvent:
		return true
	default:
		return false
	}
}

// CodecInfo describes a selectable audio codec for the trunk UI.
type CodecInfo struct {
	ID          CodecID `json:"id"`
	Label       string  `json:"label"`
	Description string  `json:"description"`
	// Recommended highlights the codec the UI should suggest by default.
	Recommended bool `json:"recommended,omitempty"`
	// Default marks codecs that make up the system's default offer profile
	// (used when a trunk has no explicit per-trunk codec list).
	Default bool `json:"default,omitempty"`
}

// OfferableCodecs returns the audio codecs a trunk may select, in the system's
// default preference order (PCMA-first for Brazil's A-law-native PSTN).
//
// This is the single source of truth the API exposes to clients so the trunk
// UI never drifts from what the media plane can actually honor. It MUST stay in
// sync with infra/voip.codecByID (the resolver that maps these IDs onto the
// wire) — a consistency test guards this. The removed G.722/Opus are
// deliberately absent (see docs/SIP_AUDIO_PIPELINE.md §9.1), and
// telephone-event is excluded because it is DTMF (RFC 4733), always appended
// automatically rather than chosen as a voice codec.
func OfferableCodecs() []CodecInfo {
	return []CodecInfo{
		{
			ID:          CodecIDPCMA,
			Label:       "G.711 A-law (PCMA)",
			Description: "Narrowband padrão. Recomendado no Brasil — a PSTN é nativamente A-law, evitando uma transcodificação na operadora.",
			Recommended: true,
			Default:     true,
		},
		{
			ID:          CodecIDPCMU,
			Label:       "G.711 µ-law (PCMU)",
			Description: "Narrowband padrão — América do Norte/Japão. Use apenas se a operadora exigir µ-law.",
			Default:     true,
		},
	}
}

type SRTPMode string

const (
	SRTPModeDisabled SRTPMode = "disabled"
	SRTPModeOptional SRTPMode = "optional"
	SRTPModeRequired SRTPMode = "required"
)

func (s SRTPMode) IsValid() bool {
	switch s {
	case SRTPModeDisabled, SRTPModeOptional, SRTPModeRequired:
		return true
	default:
		return false
	}
}

type NumberFormat string

const (
	NumberFormatPassthrough NumberFormat = "passthrough"
	NumberFormatE164        NumberFormat = "e164"
	NumberFormatNational    NumberFormat = "national"
	NumberFormatLocal       NumberFormat = "local"
)

func (n NumberFormat) IsValid() bool {
	switch n {
	case NumberFormatPassthrough, NumberFormatE164, NumberFormatNational, NumberFormatLocal:
		return true
	default:
		return false
	}
}

type SessionRefresher string

const (
	SessionRefresherUnset SessionRefresher = ""
	SessionRefresherUAC   SessionRefresher = "uac"
	SessionRefresherUAS   SessionRefresher = "uas"
)

func (s SessionRefresher) IsValid() bool {
	switch s {
	case SessionRefresherUnset, SessionRefresherUAC, SessionRefresherUAS:
		return true
	default:
		return false
	}
}

type SIPTrunk struct {
	ID string `json:"id"`

	WorkspaceID        string             `json:"workspaceId"`
	OwnerAssignedBy    string             `json:"ownerAssignedBy,omitempty"`
	OwnerAssignedAt    *time.Time         `json:"ownerAssignedAt,omitempty"`
	Name               string             `json:"name"`
	Description        string             `json:"description,omitempty"`
	Host               string             `json:"host"`
	Port               int                `json:"port"`
	Username           string             `json:"username"`
	Password           string             `json:"-"`
	Domain             string             `json:"domain"`
	Transport          TransportType      `json:"transport"`
	TrunkType          TrunkType          `json:"trunkType"`
	IsRotational       bool               `json:"isRotational"`
	PhoneNumber        string             `json:"phoneNumber,omitempty"`
	Enabled            bool               `json:"enabled"`
	IsGloballyVisible  bool               `json:"isGloballyVisible"`
	RegistrationStatus RegistrationStatus `json:"registrationStatus"`
	LastError          string             `json:"lastError,omitempty"`
	LastRegisteredAt   *time.Time         `json:"lastRegisteredAt,omitempty"`
	CreatedAt          time.Time          `json:"createdAt"`
	UpdatedAt          time.Time          `json:"updatedAt"`

	OutboundProxy *string `json:"outboundProxy,omitempty"`
	AuthUsername  *string `json:"authUsername,omitempty"`
	UserAgent     *string `json:"userAgent,omitempty"`

	RegisterEnabled       *bool             `json:"registerEnabled,omitempty"`
	RegisterExpirySeconds *int              `json:"registerExpirySeconds,omitempty"`
	RegisterRetrySeconds  *int              `json:"registerRetrySeconds,omitempty"`
	SessionTimerEnabled   *bool             `json:"sessionTimerEnabled,omitempty"`
	SessionTimerSeconds   *int              `json:"sessionTimerSeconds,omitempty"`
	MinSESeconds          *int              `json:"minSESeconds,omitempty"`
	SessionRefresher      *SessionRefresher `json:"sessionRefresher,omitempty"`

	BindHost      *string  `json:"bindHost,omitempty"`
	BindPort      *int     `json:"bindPort,omitempty"`
	PublicAddress *string  `json:"publicAddress,omitempty"`
	StunServers   []string `json:"stunServers,omitempty"`
	StunEnabled   *bool    `json:"stunEnabled,omitempty"`

	Codecs          []CodecID `json:"codecs,omitempty"`
	PTimeMs         *int      `json:"ptimeMs,omitempty"`
	DTMFPayloadType *int      `json:"dtmfPayloadType,omitempty"`
	SRTPMode        *SRTPMode `json:"srtpMode,omitempty"`

	DialPrefix      *string       `json:"dialPrefix,omitempty"`
	DialStripDigits *int          `json:"dialStripDigits,omitempty"`
	NumberFormat    *NumberFormat `json:"numberFormat,omitempty"`
	HideCallerID    *bool         `json:"hideCallerId,omitempty"`

	RingingTimeoutSeconds *int `json:"ringingTimeoutSeconds,omitempty"`
}

func (t *SIPTrunk) IsOwnedBy(workspaceID string) bool {
	if workspaceID == "" || t.WorkspaceID == "" {
		return false
	}
	return t.WorkspaceID == workspaceID
}

func (t *SIPTrunk) CanAcceptInbound(workspaceID string) error {
	if !t.IsOwnedBy(workspaceID) {
		return ErrInboundNotOwned
	}
	if t.IsGloballyVisible {
		return ErrInboundGlobalTrunk
	}
	return nil
}

// Actor identifies who is performing a mutating operation on a trunk. It is
// populated server-side by the delivery layer (never decoded from client JSON)
// and carried into the use cases so the authorization invariant lives at the
// domain/use-case boundary instead of being re-derived in HTTP handlers.
type Actor struct {
	WorkspaceID string
	IsAdmin     bool
}

// CanBeModifiedBy reports whether the actor may mutate this trunk. Platform
// admins may modify any trunk. A workspace actor may modify only a trunk it
// owns, and never a globally-visible (shared) trunk — a shared trunk that other
// workspaces depend on must only be changed by an admin.
func (t *SIPTrunk) CanBeModifiedBy(a Actor) error {
	if a.IsAdmin {
		return nil
	}
	if !t.IsOwnedBy(a.WorkspaceID) {
		return ErrTrunkForbidden
	}
	if t.IsGloballyVisible {
		return ErrTrunkForbidden
	}
	return nil
}

// ApplyGlobalVisibility sets the globally-visible flag, enforcing that only
// platform admins may publish or unpublish a trunk globally. A non-admin may
// submit the unchanged value (no-op); any actual change requires admin.
func (t *SIPTrunk) ApplyGlobalVisibility(want bool, a Actor) error {
	if want == t.IsGloballyVisible {
		return nil
	}
	if !a.IsAdmin {
		return ErrTrunkGlobalForbidden
	}
	t.IsGloballyVisible = want
	return nil
}

func (t *SIPTrunk) AssignOwner(workspaceID, userID string, now time.Time) error {
	workspaceID = strings.TrimSpace(workspaceID)
	userID = strings.TrimSpace(userID)
	if workspaceID == "" {
		return ErrTrunkOwnerWorkspaceRequired
	}
	t.WorkspaceID = workspaceID
	t.OwnerAssignedBy = userID
	assignedAt := now
	if assignedAt.IsZero() {
		assignedAt = time.Now()
	}
	t.OwnerAssignedAt = &assignedAt
	t.UpdatedAt = assignedAt
	return nil
}

func NewSIPTrunk(
	id, workspaceID, name, description string,
	host string, port int,
	username, password, domain string,
	transport TransportType,
	trunkType TrunkType,
	isRotational bool,
	phoneNumber string,
	isGloballyVisible bool,
) *SIPTrunk {
	now := time.Now()
	return &SIPTrunk{
		ID:                 id,
		WorkspaceID:        workspaceID,
		Name:               name,
		Description:        description,
		Host:               host,
		Port:               port,
		Username:           username,
		Password:           password,
		Domain:             domain,
		Transport:          transport,
		TrunkType:          trunkType,
		IsRotational:       isRotational,
		PhoneNumber:        phoneNumber,
		Enabled:            true,
		IsGloballyVisible:  isGloballyVisible,
		RegistrationStatus: RegistrationStatusPending,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

func (t *SIPTrunk) Normalize() {
	t.ID = strings.TrimSpace(t.ID)
	t.WorkspaceID = strings.TrimSpace(t.WorkspaceID)
	t.Name = strings.TrimSpace(t.Name)
	t.Description = strings.TrimSpace(t.Description)
	t.Host = strings.TrimSpace(strings.ToLower(t.Host))
	t.Username = strings.TrimSpace(t.Username)
	t.Password = strings.TrimSpace(t.Password)
	t.Domain = strings.TrimSpace(strings.ToLower(t.Domain))
	t.PhoneNumber = strings.TrimSpace(t.PhoneNumber)

	if t.Transport == "" {
		t.Transport = TransportUDP
	}
	if t.Port <= 0 {
		t.Port = 5060
	}
	if t.Domain == "" {
		t.Domain = t.Host
	}

	t.normalizeAdvanced()
}

func (t *SIPTrunk) normalizeAdvanced() {
	trimPtr := func(p *string) *string {
		if p == nil {
			return nil
		}
		v := strings.TrimSpace(*p)
		if v == "" {
			return nil
		}
		*p = v
		return p
	}
	lowerPtr := func(p *string) *string {
		if p == nil {
			return nil
		}
		v := strings.ToLower(strings.TrimSpace(*p))
		if v == "" {
			return nil
		}
		*p = v
		return p
	}

	t.OutboundProxy = trimPtr(t.OutboundProxy)
	t.AuthUsername = trimPtr(t.AuthUsername)
	t.UserAgent = trimPtr(t.UserAgent)
	t.BindHost = lowerPtr(t.BindHost)
	t.PublicAddress = trimPtr(t.PublicAddress)
	t.DialPrefix = trimPtr(t.DialPrefix)

	if t.SRTPMode != nil {
		v := SRTPMode(strings.ToLower(strings.TrimSpace(string(*t.SRTPMode))))
		if v == "" {
			t.SRTPMode = nil
		} else {
			*t.SRTPMode = v
		}
	}
	if t.NumberFormat != nil {
		v := NumberFormat(strings.ToLower(strings.TrimSpace(string(*t.NumberFormat))))
		if v == "" {
			t.NumberFormat = nil
		} else {
			*t.NumberFormat = v
		}
	}
	if t.SessionRefresher != nil {
		v := SessionRefresher(strings.ToLower(strings.TrimSpace(string(*t.SessionRefresher))))
		*t.SessionRefresher = v
	}

	t.StunServers = cleanStringSlice(t.StunServers, false)
	if len(t.Codecs) > 0 {
		out := make([]CodecID, 0, len(t.Codecs))
		seen := make(map[CodecID]struct{}, len(t.Codecs))
		for _, c := range t.Codecs {
			v := CodecID(strings.ToLower(strings.TrimSpace(string(c))))
			if v == "" {
				continue
			}
			if _, dup := seen[v]; dup {
				continue
			}
			seen[v] = struct{}{}
			out = append(out, v)
		}
		t.Codecs = out
	}
}

func cleanStringSlice(in []string, lower bool) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, s := range in {
		v := strings.TrimSpace(s)
		if lower {
			v = strings.ToLower(v)
		}
		if v == "" {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

const (
	maxNameLen          = 120
	maxDescriptionLen   = 1000
	maxHostLen          = 253
	maxUsernameLen      = 128
	maxPasswordLen      = 256
	maxDomainLen        = 253
	maxPhoneLen         = 32
	maxIdentityFieldLen = 128
	maxStunServers      = 8
	maxCodecsList       = 8
)

func containsCtrlChar(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == 0x7f {
			return true
		}
	}
	return false
}

func (t *SIPTrunk) Validate() error {
	t.Normalize()
	if t.WorkspaceID == "" {
		return ErrTrunkOwnerWorkspaceRequired
	}

	if t.Name == "" {
		return ErrTrunkNameRequired
	}
	if len(t.Name) > maxNameLen {
		return ErrTrunkNameTooLong
	}
	if len(t.Description) > maxDescriptionLen {
		return ErrTrunkDescriptionTooLong
	}
	if t.Host == "" {
		return ErrTrunkHostRequired
	}
	if len(t.Host) > maxHostLen || len(t.Domain) > maxDomainLen {
		return ErrTrunkHostInvalid
	}
	if t.Username == "" {
		return ErrTrunkUsernameRequired
	}
	if len(t.Username) > maxUsernameLen || containsCtrlChar(t.Username) {
		return ErrTrunkUsernameInvalid
	}
	if t.Password == "" {
		return ErrTrunkPasswordRequired
	}
	if len(t.Password) > maxPasswordLen || containsCtrlChar(t.Password) {
		return ErrTrunkPasswordInvalid
	}
	if t.Port < 1 || t.Port > 65535 {
		return ErrTrunkPortInvalid
	}
	if !t.Transport.IsValid() {
		return ErrTrunkTransportInvalid
	}
	if !t.TrunkType.IsValid() {
		return ErrTrunkTypeInvalid
	}
	if !t.IsRotational && t.PhoneNumber == "" {
		return ErrTrunkPhoneRequired
	}
	if len(t.PhoneNumber) > maxPhoneLen {
		return ErrTrunkPhoneInvalid
	}
	if err := validateRegistrarHost(t.Host); err != nil {
		return err
	}
	if t.Domain != "" && t.Domain != t.Host {
		if err := validateRegistrarHost(t.Domain); err != nil {
			return err
		}
	}

	if err := t.validateAdvanced(); err != nil {
		return err
	}

	return nil
}

func validateRegistrarHost(host string) error {
	if host == "" {
		return ErrTrunkHostRequired
	}
	lower := strings.ToLower(host)
	switch lower {
	case "localhost", "broadcasthost", "ip6-localhost", "ip6-loopback":
		return ErrTrunkHostBlocked
	}
	if strings.ContainsAny(host, " \t\r\n\"'\\/`<>(){}[]|;,") {
		return ErrTrunkHostInvalid
	}

	ip := net.ParseIP(host)
	if ip == nil {
		if strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
			return ErrTrunkHostInvalid
		}
		return nil
	}
	if IsBlockedIP(ip) {
		return ErrTrunkHostBlocked
	}
	return nil
}

// IsBlockedIP reports whether an IP is in a range a trunk must never target:
// loopback, RFC1918 private, link-local (incl. the 169.254.169.254 metadata
// address), multicast and the unspecified address. It is the single source of
// truth shared by the create-time literal check (validateRegistrarHost) and the
// infra HostGuard's resolve-time check (DNS-rebinding / SSRF defense).
func IsBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified() ||
		ip.IsInterfaceLocalMulticast()
}

// OutboundHosts returns the distinct hostnames/IPs this trunk will connect to at
// registration or call time (registrar host, SIP domain, outbound proxy and STUN
// servers), with any port stripped. The use case feeds these to a HostGuard so a
// name that resolves to an internal address is rejected even though it passed the
// IP-literal validation (basic SSRF-by-DNS defense).
func (t *SIPTrunk) OutboundHosts() []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(h string) {
		h = strings.TrimSpace(h)
		if h == "" {
			return
		}
		if _, dup := seen[h]; dup {
			return
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	add(t.Host)
	add(t.Domain)
	if t.OutboundProxy != nil {
		add(stripPort(*t.OutboundProxy))
	}
	for _, s := range t.StunServers {
		if host, _, err := net.SplitHostPort(strings.TrimSpace(s)); err == nil {
			add(host)
		}
	}
	return out
}

func (t *SIPTrunk) UpdateRegistrationStatus(status RegistrationStatus, errMsg string) {
	t.RegistrationStatus = status
	t.LastError = errMsg

	if status == RegistrationStatusRegistered {
		now := time.Now()
		t.LastRegisteredAt = &now
		t.LastError = ""
	}
	t.UpdatedAt = time.Now()
}

type SIPTrunkStatusUpdate struct {
	TrunkID   string
	Status    RegistrationStatus
	Error     string
	Timestamp time.Time
}

func (t *SIPTrunk) validateAdvanced() error {

	if t.UserAgent != nil {
		if len(*t.UserAgent) > maxIdentityFieldLen {
			return ErrTrunkUserAgentInvalid
		}
		if containsCtrlChar(*t.UserAgent) {
			return ErrTrunkIdentityFieldInvalid
		}
	}
	if t.AuthUsername != nil {
		if len(*t.AuthUsername) > maxIdentityFieldLen || containsCtrlChar(*t.AuthUsername) {
			return ErrTrunkIdentityFieldInvalid
		}
	}
	if t.OutboundProxy != nil {
		if len(*t.OutboundProxy) > 255 || containsCtrlChar(*t.OutboundProxy) {
			return ErrTrunkOutboundProxyInvalid
		}
		if err := validateRegistrarHost(stripPort(*t.OutboundProxy)); err != nil {
			return ErrTrunkOutboundProxyInvalid
		}
	}

	if t.RegisterExpirySeconds != nil {
		if *t.RegisterExpirySeconds < 1 || *t.RegisterExpirySeconds > 86400 {
			return ErrTrunkRegisterExpiryInvalid
		}
	}
	if t.RegisterRetrySeconds != nil {
		if *t.RegisterRetrySeconds < 1 || *t.RegisterRetrySeconds > 3600 {
			return ErrTrunkRegisterRetryInvalid
		}
	}
	if t.MinSESeconds != nil && *t.MinSESeconds < 90 {

		return ErrTrunkMinSEInvalid
	}
	if t.SessionTimerSeconds != nil {
		if *t.SessionTimerSeconds < 90 {
			return ErrTrunkSessionTimerInvalid
		}
		if t.MinSESeconds != nil && *t.SessionTimerSeconds < *t.MinSESeconds {
			return ErrTrunkSessionTimerInvalid
		}
	}
	if t.SessionRefresher != nil && !t.SessionRefresher.IsValid() {
		return ErrTrunkSessionRefresherInvalid
	}

	if t.BindPort != nil && (*t.BindPort < 1 || *t.BindPort > 65535) {
		return ErrTrunkBindPortInvalid
	}
	if t.PublicAddress != nil {
		if len(*t.PublicAddress) > 255 || containsCtrlChar(*t.PublicAddress) {
			return ErrTrunkPublicAddressInvalid
		}
	}
	if len(t.StunServers) > maxStunServers {
		return ErrTrunkTooManyEntries
	}
	for _, s := range t.StunServers {
		if !looksLikeHostPort(s) {
			return ErrTrunkStunServerInvalid
		}
		host, _, _ := net.SplitHostPort(strings.TrimSpace(s))
		if err := validateRegistrarHost(host); err != nil {
			return ErrTrunkStunServerBlocked
		}
	}

	if len(t.Codecs) > maxCodecsList {
		return ErrTrunkTooManyEntries
	}
	for _, c := range t.Codecs {
		if !c.IsValid() {
			return ErrTrunkCodecInvalid
		}
	}
	if t.PTimeMs != nil && (*t.PTimeMs < 10 || *t.PTimeMs > 120) {
		return ErrTrunkPTimeInvalid
	}
	if t.DTMFPayloadType != nil {

		if *t.DTMFPayloadType < 96 || *t.DTMFPayloadType > 127 {
			return ErrTrunkDTMFPayloadInvalid
		}
	}
	if t.SRTPMode != nil {
		if !t.SRTPMode.IsValid() {
			return ErrTrunkSRTPModeInvalid
		}
		if *t.SRTPMode == SRTPModeRequired && t.Transport == TransportUDP {

			return ErrTrunkSRTPRequiresSecureTransport
		}
	}

	if t.DialPrefix != nil {
		if len(*t.DialPrefix) > 16 || containsCtrlChar(*t.DialPrefix) {
			return ErrTrunkDialPrefixInvalid
		}
	}
	if t.DialStripDigits != nil && (*t.DialStripDigits < 0 || *t.DialStripDigits > 16) {
		return ErrTrunkDialStripInvalid
	}
	if t.NumberFormat != nil && !t.NumberFormat.IsValid() {
		return ErrTrunkNumberFormatInvalid
	}

	if t.RingingTimeoutSeconds != nil {
		if *t.RingingTimeoutSeconds < 5 || *t.RingingTimeoutSeconds > 600 {
			return ErrTrunkRingingTimeoutInvalid
		}
	}

	return nil
}

func stripPort(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, ";?"); i >= 0 {
		s = s[:i]
	}
	if strings.HasPrefix(s, "[") {
		if end := strings.Index(s, "]"); end > 0 {
			return s[1:end]
		}
	}
	if i := strings.LastIndex(s, ":"); i >= 0 {
		return s[:i]
	}
	return s
}

func looksLikeHostPort(s string) bool {
	host, port, err := net.SplitHostPort(strings.TrimSpace(s))
	if err != nil {
		return false
	}
	if host == "" {
		return false
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return false
	}
	return true
}
