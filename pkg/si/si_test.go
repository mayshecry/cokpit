package si

import "testing"

func TestValidStatuses(t *testing.T) {
	if len(AllStatuses) != 9 {
		t.Fatalf("AllStatuses = %d, want 9", len(AllStatuses))
	}
	for _, s := range AllStatuses {
		if !IsValidStatus(s) {
			t.Errorf("expected %q to be valid", s)
		}
	}
	if IsValidStatus("Bogus") || IsValidStatus("") {
		t.Error("Bogus / empty must not be accepted as statuses")
	}
	if got := StatusLabel(StatusLive); got != "Live / actief" {
		t.Errorf("StatusLabel(Live) = %q", got)
	}
}

func TestCanTransition(t *testing.T) {
	tests := []struct {
		from, to Status
		want     bool
	}{
		{StatusRequested, StatusDesign, true},
		{StatusRequested, StatusCancelled, true},
		{StatusDesign, StatusDevelopment, true},
		{StatusDesign, StatusCancelled, true},
		{StatusDevelopment, StatusTesting, true},
		{StatusDevelopment, StatusDesign, true},
		{StatusTesting, StatusLive, true},
		{StatusTesting, StatusDevelopment, true},
		{StatusLive, StatusChange, true},
		{StatusLive, StatusRetired, true},
		{StatusChange, StatusDevelopment, true},
		{StatusChange, StatusRetired, true},
		{StatusRetired, StatusArchived, true},
		{StatusRequested, StatusLive, false},
		{StatusRequested, StatusArchived, false},
		{StatusArchived, StatusLive, false},
		{StatusCancelled, StatusDesign, false},
		{StatusLive, StatusLive, false},
		{StatusRetired, StatusCancelled, false},
		{"Bogus", StatusDesign, false},
		{"", StatusDesign, false},
	}
	for _, tc := range tests {
		if got := CanTransition(tc.from, tc.to); got != tc.want {
			t.Errorf("CanTransition(%q,%q) = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestDetermineTransition(t *testing.T) {
	if got, err := DetermineTransition(StatusTesting, StatusLive); err != nil || got != StatusLive {
		t.Errorf("legal transition = %v, %v; want Live, nil", got, err)
	}
	if _, err := DetermineTransition(StatusRequested, StatusLive); err == nil {
		t.Error("Requested->Live must be rejected")
	}
	if _, err := DetermineTransition("Bogus", StatusDesign); err == nil {
		t.Error("bogus source must be rejected")
	}
	if _, err := DetermineTransition(StatusDesign, "Bogus"); err == nil {
		t.Error("bogus target must be rejected")
	}
	if got, err := DetermineTransition(StatusArchived, StatusLive); err == nil {
		t.Errorf("Archived->Live must be rejected, got %v", got)
	}
}

func TestNextStatuses(t *testing.T) {
	nxt := NextStatuses(StatusLive)
	if len(nxt) != 2 {
		t.Errorf("NextStatuses(Live) = %v, want 2 targets", nxt)
	}
	if got := NextStatuses("Bogus"); got != nil {
		t.Errorf("NextStatuses(Bogus) = %v, want nil", got)
	}
}

func TestEnvironments(t *testing.T) {
	if !IsValidEnvironment(EnvironmentTest) {
		t.Error("TEST must be valid")
	}
	if IsValidEnvironment("STAGING") {
		t.Error("STAGING must not be valid")
	}
}

func TestParseSICode(t *testing.T) {
	tests := []struct {
		code      string
		wantDebit string
		wantDev   string
		wantConf  string
		wantErr   bool
	}{
		{
			code:      "SI-92931-LAP-CFG0003",
			wantDebit: "92931",
			wantDev:   "LAP",
			wantConf:  "CFG0003",
			wantErr:   false,
		},
		{
			code:      "SI-12345-DESK-ABC123",
			wantDebit: "12345",
			wantDev:   "DESK",
			wantConf:  "ABC123",
			wantErr:   false,
		},
		{
			code:    "invalid-code",
			wantErr: true,
		},
		{
			code:    "SI-92931-LAP",
			wantErr: true,
		},
		{
			code:    "",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		debit, dev, conf, err := ParseSICode(tc.code)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseSICode(%q) expected error, got nil", tc.code)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseSICode(%q) returned unexpected error: %v", tc.code, err)
			continue
		}
		if debit != tc.wantDebit || dev != tc.wantDev || conf != tc.wantConf {
			t.Errorf("ParseSICode(%q) = (%q, %q, %q), want (%q, %q, %q)", tc.code, debit, dev, conf, tc.wantDebit, tc.wantDev, tc.wantConf)
		}
	}
}

func TestSIConfigValidation(t *testing.T) {
	validConfig := &SIConfig{
		DebitNumber:    "92931",
		DeviceType:     "LAP",
		ConfigID:       "CFG0003",
		WindowsProfile: "Standard User",
		Software:       "Office 365, VPN Client",
		AssetSticker:   true,
		Sleeve:         true,
		ScreenProtector: false,
		OtherDemands:   "Deliver to floor 3",
	}

	if validConfig.DebitNumber != "92931" {
		t.Errorf("DebitNumber = %q, want 92931", validConfig.DebitNumber)
	}
	if validConfig.DeviceType != "LAP" {
		t.Errorf("DeviceType = %q, want LAP", validConfig.DeviceType)
	}
	if validConfig.ConfigID != "CFG0003" {
		t.Errorf("ConfigID = %q, want CFG0003", validConfig.ConfigID)
	}
	if !validConfig.AssetSticker {
		t.Error("AssetSticker should be true")
	}
	if validConfig.ScreenProtector {
		t.Error("ScreenProtector should be false")
	}
}