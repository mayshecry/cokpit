package si

import "testing"

func TestValidStatuses(t *testing.T) {
	if len(AllStatuses) != 5 {
		t.Fatalf("AllStatuses = %d, want 5", len(AllStatuses))
	}
	for _, s := range AllStatuses {
		if !IsValidStatus(s) {
			t.Errorf("expected %q to be valid", s)
		}
	}
	if IsValidStatus("Bogus") || IsValidStatus("") {
		t.Error("Bogus / empty must not be accepted as statuses")
	}
	if got := StatusLabel(StatusAccepted); got != "Geaccepteerd" {
		t.Errorf("StatusLabel(Accepted) = %q", got)
	}
}

func TestCanTransition(t *testing.T) {
	tests := []struct {
		from, to Status
		want     bool
	}{
		{StatusConcept, StatusInValidation, true},
		{StatusConcept, StatusBlocked, true},
		{StatusInValidation, StatusAccepted, true},
		{StatusInValidation, StatusConcept, true},
		{StatusInValidation, StatusBlocked, true},
		{StatusAccepted, StatusOutphased, true},
		{StatusAccepted, StatusBlocked, true},
		{StatusBlocked, StatusConcept, true},
		{StatusBlocked, StatusInValidation, true},
		{StatusBlocked, StatusAccepted, true},
		{StatusConcept, StatusAccepted, false},
		{StatusConcept, StatusOutphased, false},
		{StatusOutphased, StatusConcept, false},
		{StatusAccepted, StatusInValidation, false},
		{StatusAccepted, StatusAccepted, false},
		{"Bogus", StatusConcept, false},
		{"", StatusConcept, false},
	}
	for _, tc := range tests {
		if got := CanTransition(tc.from, tc.to); got != tc.want {
			t.Errorf("CanTransition(%q,%q) = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestDetermineTransition(t *testing.T) {
	if got, err := DetermineTransition(StatusInValidation, StatusAccepted); err != nil || got != StatusAccepted {
		t.Errorf("legal transition = %v, %v; want Accepted, nil", got, err)
	}
	if _, err := DetermineTransition(StatusConcept, StatusAccepted); err == nil {
		t.Error("Concept->Accepted must be rejected")
	}
	if _, err := DetermineTransition("Bogus", StatusConcept); err == nil {
		t.Error("bogus source must be rejected")
	}
	if _, err := DetermineTransition(StatusConcept, "Bogus"); err == nil {
		t.Error("bogus target must be rejected")
	}
	if got, err := DetermineTransition(StatusOutphased, StatusConcept); err == nil {
		t.Errorf("Outphased->Concept must be rejected, got %v", got)
	}
}

func TestNextStatuses(t *testing.T) {
	nxt := NextStatuses(StatusInValidation)
	if len(nxt) != 3 {
		t.Errorf("NextStatuses(InValidation) = %v, want 3 targets", nxt)
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
		DebitNumber:     "92931",
		DeviceType:      "LAP",
		ConfigID:        "CFG0003",
		WindowsProfile:  "Standard User",
		Software:        "Office 365, VPN Client",
		AssetSticker:    true,
		Sleeve:          true,
		ScreenProtector: false,
		OtherDemands:    "Deliver to floor 3",
		WorkInstructions: "Follow standard procedure",
		SWI:              "SWI-001",
		Workflow:         "1. Prep\n2. Execute\n3. Validate",
		QCProfile:        "Standard QC",
		Automations:      "Auto-notify on status change",
		EscalationFlow:   []string{"NPI", "Operations"},
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
	if len(validConfig.EscalationFlow) != 2 {
		t.Errorf("EscalationFlow length = %d, want 2", len(validConfig.EscalationFlow))
	}
}