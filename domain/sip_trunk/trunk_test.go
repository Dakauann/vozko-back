package sip_trunk

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDialPlanApply(t *testing.T) {
	cases := []struct {
		name   string
		plan   DialPlan
		number string
		want   string
	}{
		{"empty plan keeps the number", DialPlan{}, "+5511999990000", "+5511999990000"},
		{"strips a matching prefix", DialPlan{StripPrefix: "+55"}, "+5511999990000", "11999990000"},
		{"keeps the number when the prefix does not match", DialPlan{StripPrefix: "+1"}, "+5511999990000", "+5511999990000"},
		{"adds a prefix", DialPlan{AddPrefix: "0"}, "11999990000", "011999990000"},
		{"strips before adding", DialPlan{StripPrefix: "+55", AddPrefix: "0"}, "+5511999990000", "011999990000"},
		{"trims surrounding whitespace", DialPlan{StripPrefix: "+55"}, "  +5511999990000 ", "11999990000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.plan.Apply(tc.number); got != tc.want {
				t.Errorf("Apply(%q) = %q, want %q", tc.number, got, tc.want)
			}
		})
	}
}

func TestSignalingDomainPrefersDomainAndDropsPort(t *testing.T) {
	cases := []struct {
		name  string
		trunk SIPTrunk
		want  string
	}{
		{"explicit domain wins", SIPTrunk{Host: "10.0.0.1", Domain: "sip.provider.com"}, "sip.provider.com"},
		{"falls back to host", SIPTrunk{Host: "sip.provider.com"}, "sip.provider.com"},
		{"drops the port from host", SIPTrunk{Host: "sip.provider.com:5080"}, "sip.provider.com"},
		{"drops the port from domain", SIPTrunk{Domain: "sip.provider.com:5080"}, "sip.provider.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.trunk.SignalingDomain(); got != tc.want {
				t.Errorf("SignalingDomain() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAuthUsernameFallsBackToUsername(t *testing.T) {
	trunk := SIPTrunk{Username: "1001"}
	if got := trunk.AuthUsername(); got != "1001" {
		t.Errorf("AuthUsername() = %q, want 1001", got)
	}
	trunk.Settings.AuthUsername = "auth-1001"
	if got := trunk.AuthUsername(); got != "auth-1001" {
		t.Errorf("AuthUsername() = %q, want auth-1001", got)
	}
}

func TestTrunkTypeSupportsDirection(t *testing.T) {
	cases := []struct {
		trunkType TrunkType
		direction CallDirection
		want      bool
	}{
		{TrunkTypeOutbound, CallDirectionOutbound, true},
		{TrunkTypeOutbound, CallDirectionInbound, false},
		{TrunkTypeInbound, CallDirectionInbound, true},
		{TrunkTypeInbound, CallDirectionOutbound, false},
		{TrunkTypeBidirectional, CallDirectionOutbound, true},
		{TrunkTypeBidirectional, CallDirectionInbound, true},
		{TrunkType("unknown"), CallDirectionOutbound, false},
	}
	for _, tc := range cases {
		if got := tc.trunkType.Supports(tc.direction); got != tc.want {
			t.Errorf("%s.Supports(%s) = %v, want %v", tc.trunkType, tc.direction, got, tc.want)
		}
	}
}

func TestUpdateRegistrationStatusStampsTheChange(t *testing.T) {
	trunk := SIPTrunk{RegistrationStatus: RegistrationStatusRegistering}
	before := time.Now()
	trunk.UpdateRegistrationStatus(RegistrationStatusFailed, "401 unauthorized")
	if trunk.RegistrationStatus != RegistrationStatusFailed || trunk.LastError != "401 unauthorized" {
		t.Fatalf("status = %s / %q, want FAILED / 401 unauthorized", trunk.RegistrationStatus, trunk.LastError)
	}
	if trunk.UpdatedAt.Before(before) {
		t.Errorf("UpdatedAt = %v, want at or after %v", trunk.UpdatedAt, before)
	}
}

func TestSettingsValidateRejectsUnknownCodecsAndSRTPModes(t *testing.T) {
	valid := Settings{Codecs: []Codec{CodecPCMU, CodecPCMA, CodecOpus, CodecTelephoneEvent}, SRTPMode: SRTPModeRequired}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
	if err := (Settings{}).Validate(); err != nil {
		t.Fatalf("zero Settings Validate() = %v, want nil", err)
	}
	if err := (Settings{Codecs: []Codec{"G729"}}).Validate(); !errors.Is(err, ErrUnsupportedCodec) {
		t.Errorf("unknown codec Validate() = %v, want ErrUnsupportedCodec", err)
	}
	if err := (Settings{SRTPMode: "sometimes"}).Validate(); !errors.Is(err, ErrUnsupportedSRTPMode) {
		t.Errorf("unknown SRTP mode Validate() = %v, want ErrUnsupportedSRTPMode", err)
	}
}

func TestCodecParseIsCaseInsensitive(t *testing.T) {
	cases := map[string]Codec{"pcmu": CodecPCMU, "PCMA": CodecPCMA, "OPUS": CodecOpus, "Telephone-Event": CodecTelephoneEvent}
	for raw, want := range cases {
		got, err := ParseCodec(raw)
		if err != nil || got != want {
			t.Errorf("ParseCodec(%q) = %q, %v, want %q", raw, got, err, want)
		}
	}
}

func validTrunk() SIPTrunk {
	return SIPTrunk{
		WorkspaceID: "ws-1",
		Name:        "Main line",
		TrunkType:   TrunkTypeBidirectional,
		Host:        "sip.provider.com",
		Transport:   TransportUDP,
		Username:    "1001",
		Password:    "secret",
	}
}

func TestSIPTrunkValidate(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*SIPTrunk)
		want   error
	}{
		{"valid trunk", func(*SIPTrunk) {}, nil},
		{"missing workspace", func(tr *SIPTrunk) { tr.WorkspaceID = "" }, ErrWorkspaceRequired},
		{"blank name", func(tr *SIPTrunk) { tr.Name = "  " }, ErrNameRequired},
		{"missing host", func(tr *SIPTrunk) { tr.Host = "" }, ErrHostRequired},
		{"host with scheme", func(tr *SIPTrunk) { tr.Host = "sip:provider.com" }, ErrInvalidHost},
		{"host with port", func(tr *SIPTrunk) { tr.Host = "provider.com:5060" }, ErrInvalidHost},
		{"negative port", func(tr *SIPTrunk) { tr.Port = -1 }, ErrInvalidPort},
		{"port too large", func(tr *SIPTrunk) { tr.Port = 70000 }, ErrInvalidPort},
		{"unknown transport", func(tr *SIPTrunk) { tr.Transport = "SCTP" }, ErrUnsupportedTransport},
		{"tls needs certificates the engine does not provision", func(tr *SIPTrunk) { tr.Transport = "TLS" }, ErrUnsupportedTransport},
		{"unknown trunk type", func(tr *SIPTrunk) { tr.TrunkType = "PEER" }, ErrUnsupportedTrunkType},
		{"registration without username", func(tr *SIPTrunk) { tr.Username = "" }, ErrCredentialsRequired},
		{"registration without password", func(tr *SIPTrunk) { tr.Password = "" }, ErrCredentialsRequired},
		{"ip-authenticated trunk needs no credentials", func(tr *SIPTrunk) {
			tr.Username, tr.Password = "", ""
			tr.Settings.SkipRegistration = true
		}, nil},
		{"invalid settings", func(tr *SIPTrunk) { tr.Settings.Codecs = []Codec{"G729"} }, ErrUnsupportedCodec},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			trunk := validTrunk()
			tc.mutate(&trunk)
			err := trunk.Validate()
			if tc.want == nil && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("Validate() = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestATrunkCanBeCheckedBeforeItsPasswordIsKnown(t *testing.T) {
	trunk := validTrunk()
	trunk.Password = ""
	if err := trunk.ValidateWithoutPassword(); err != nil {
		t.Fatalf("ValidateWithoutPassword() = %v, want nil", err)
	}
	if err := trunk.Validate(); !errors.Is(err, ErrCredentialsRequired) {
		t.Fatalf("Validate() = %v, want the password to be required", err)
	}
	trunk.Username = ""
	if err := trunk.ValidateWithoutPassword(); !errors.Is(err, ErrCredentialsRequired) {
		t.Fatalf("ValidateWithoutPassword() = %v, a registering trunk still needs its username", err)
	}
	trunk = validTrunk()
	trunk.Host = "sip:provider.com"
	if err := trunk.ValidateWithoutPassword(); !errors.Is(err, ErrInvalidHost) {
		t.Fatalf("ValidateWithoutPassword() = %v, want the rest still checked", err)
	}
}

func TestSettingsValidateInboundSources(t *testing.T) {
	if err := (Settings{InboundAllowedSources: []string{"203.0.113.0/24", "198.51.100.7"}}).Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
	if err := (Settings{InboundAllowedSources: []string{"not-an-ip"}}).Validate(); !errors.Is(err, ErrInvalidInboundSource) {
		t.Fatalf("Validate() = %v, want ErrInvalidInboundSource", err)
	}
}

func TestInboundSourceAllowlist(t *testing.T) {
	allow, err := ParseInboundSources([]string{"203.0.113.0/24", "198.51.100.7"})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]bool{"203.0.113.40": true, "198.51.100.7": true, "198.51.100.8": false, "garbage": false}
	for ip, want := range cases {
		if got := allow.Contains(ip); got != want {
			t.Errorf("Contains(%q) = %v, want %v", ip, got, want)
		}
	}
}

func TestNormalizeDialString(t *testing.T) {
	valid := map[string]string{
		"+55 (11) 99999-0000": "+5511999990000",
		"011 4003-1234":       "01140031234",
		"*100#":               "*100#",
		" 190 ":               "190",
	}
	for raw, want := range valid {
		got, err := NormalizeDialString(raw)
		if err != nil || got != want {
			t.Errorf("NormalizeDialString(%q) = %q, %v, want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{"", "   ", "abc", "+55 11 9999@evil.com", "1234;user=phone", "12+34", strings.Repeat("9", 33)} {
		if _, err := NormalizeDialString(raw); !errors.Is(err, ErrInvalidPhoneNumber) {
			t.Errorf("NormalizeDialString(%q) error = %v, want ErrInvalidPhoneNumber", raw, err)
		}
	}
}

func TestSettingsValidateDialPlanPrefixes(t *testing.T) {
	if err := (Settings{DialPlan: DialPlan{StripPrefix: "+55", AddPrefix: "0"}}).Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
	for _, plan := range []DialPlan{{AddPrefix: "sip:"}, {StripPrefix: "1;"}, {AddPrefix: "0 "}} {
		if err := (Settings{DialPlan: plan}).Validate(); !errors.Is(err, ErrInvalidDialPlan) {
			t.Errorf("Validate(%+v) = %v, want ErrInvalidDialPlan", plan, err)
		}
	}
}

func TestIsInvalidInputSeparatesValidationFromStateErrors(t *testing.T) {
	trunk := validTrunk()
	trunk.Host = ""
	if !IsInvalidInput(trunk.Validate()) {
		t.Error("a validation failure is not reported as invalid input")
	}
	if IsInvalidInput(ErrTrunkNotRegistered) || IsInvalidInput(ErrTrunkNotFound) || IsInvalidInput(nil) {
		t.Error("state errors must not be reported as invalid input")
	}
}

func TestSettingsRequireACodecTheBrowserCanCarry(t *testing.T) {
	if err := (Settings{Codecs: []Codec{CodecOpus, CodecTelephoneEvent}}).Validate(); !errors.Is(err, ErrNoBridgeableCodec) {
		t.Fatalf("opus-only Validate() = %v, want ErrNoBridgeableCodec", err)
	}
	for _, codecs := range [][]Codec{{CodecPCMA}, {CodecOpus, CodecPCMU}, nil} {
		if err := (Settings{Codecs: codecs}).Validate(); err != nil {
			t.Errorf("Validate(%v) = %v, want nil", codecs, err)
		}
	}
}
