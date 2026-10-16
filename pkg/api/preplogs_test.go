package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// samplePrepLog mimics the structure parsePrepLog expects. Lines are
// timestamped like the real script output.
const samplePrepLog = `2026-10-12 09:00:00 - === Starting laptop prep on LAPTOP-TEST01 ===
2026-10-12 09:00:01 - CPU: Intel(R) Core(TM) i5-8350U CPU @ 1.70GHz
2026-10-12 09:00:01 - RAM: 16 GB
2026-10-12 09:00:01 - OS: Microsoft Windows 11 Pro
2026-10-12 09:00:02 - Collecting device identification...
2026-10-12 09:00:02 - ----- DEVICE ID -----
2026-10-12 09:00:02 - ComputerName: LAPTOP-TEST01
2026-10-12 09:00:02 - Manufacturer: LENOVO
2026-10-12 09:00:02 - Model: ThinkPad T14
2026-10-12 09:00:02 - System UUID: 11111111-2222-3333-4444-555555555555
2026-10-12 09:00:02 - MAC Address(es): AA:BB:CC:DD:EE:01
2026-10-12 09:00:03 - Collecting system specs...
2026-10-12 09:00:03 - ----- CPU DETAIL -----
2026-10-12 09:00:03 - Name: Intel(R) Core(TM) i5-8350U
2026-10-12 09:00:03 - Cores: 4
2026-10-12 09:00:03 - Threads: 8
2026-10-12 09:00:03 - Status: OK
2026-10-12 09:00:04 - ----- DISK DRIVES -----
2026-10-12 09:00:04 - Model: SAMSUNG MZVLB512HBJQ
2026-10-12 09:00:04 - SizeBytes: 512110190592
2026-10-12 09:00:04 - Status: OK
2026-10-12 09:00:05 - Collecting extended hardware checks...
2026-10-12 09:00:05 - ----- BATTERY WMI -----
2026-10-12 09:00:05 - Name: Internal Battery
2026-10-12 09:00:05 - ChargePct: 87
2026-10-12 09:00:05 - Status: Unknown
2026-10-12 09:00:05 - TPM Activated=-1 Enabled=-1 Spec=2.0, 0, 1.38
2026-10-12 09:00:06 - ----- GPU -----
2026-10-12 09:00:06 - Name: Intel(R) UHD Graphics 620
2026-10-12 09:00:06 - Status: OK
2026-10-12 09:00:07 - Triggering update scan...
2026-10-12 09:00:07 - Run: UsoClient.exe StartScan
2026-10-12 09:00:08 - Exit code: 0
2026-10-12 09:00:08 - Run: UsoClient.exe StartInstall
2026-10-12 09:00:09 - Exit code: 3010
2026-10-12 09:00:09 - Prep phase complete
2026-10-12 09:00:09 - === Prep phase complete ===
`

func parseForTest(t *testing.T, log string) (map[string]any, []prepIssue) {
	t.Helper()
	doc, host, err := parsePrepLog([]byte(log))
	if err != nil {
		t.Fatalf("parsePrepLog: %v", err)
	}
	if host != "LAPTOP-TEST01" {
		t.Fatalf("host = %q", host)
	}
	issues, _ := doc["issues"].([]prepIssue)
	return doc, issues
}

func TestPrepLogParsesWMITrueValuesAndFullTPMSpec(t *testing.T) {
	doc, _ := parseForTest(t, samplePrepLog)
	health := doc["health"].(map[string]any)
	tpm := health["tpm"].(map[string]any)
	if tpm["activated"] != true || tpm["enabled"] != true {
		t.Errorf("TPM booleans = activated:%v enabled:%v, want both true", tpm["activated"], tpm["enabled"])
	}
	if tpm["spec"] != "2.0, 0, 1.38" {
		t.Errorf("TPM spec = %q, want full version string", tpm["spec"])
	}
}

func TestPrepLogStatusUnknownIsNotAnIssue(t *testing.T) {
	doc, issues := parseForTest(t, samplePrepLog)
	for _, is := range issues {
		if strings.Contains(is.Src, "Battery") || strings.Contains(is.Text, "Unknown") {
			t.Errorf("battery 'Unknown' status reported as an issue: %+v", is)
		}
	}
	_ = doc
}

func TestPrepLogRebootRequiredExitCodeIsNotAnIssue(t *testing.T) {
	_, issues := parseForTest(t, samplePrepLog)
	for _, is := range issues {
		if strings.Contains(is.Text, "3010") {
			t.Errorf("exit code 3010 (success, restart required) reported as an issue: %+v", is)
		}
	}
}

func TestPrepLogRealFailuresStillFlagged(t *testing.T) {
	bad := strings.Replace(samplePrepLog, "Status: Unknown", "Status: Pred Fail", 1) // battery fails
	bad = strings.Replace(bad, "Exit code: 3010", "Exit code: 1603", 1)
	bad = strings.Replace(bad, "Status: OK\n2026-10-12 09:00:07 - Triggering", "Status: Degraded\n2026-10-12 09:00:07 - Triggering", 1)
	_, issues := parseForTest(t, bad)
	want := map[string]bool{"Status: Pred Fail": false, "Status: Degraded": false, "UsoClient.exe StartInstall exited with code 1603": false}
	for _, is := range issues {
		if _, ok := want[is.Text]; ok {
			want[is.Text] = true
		}
	}
	for k, found := range want {
		if !found {
			t.Errorf("expected issue %q to be flagged; got %+v", k, issues)
		}
	}
}

func TestPrepLogCountsAfterFix(t *testing.T) {
	doc, _ := parseForTest(t, samplePrepLog)
	stats := doc["stats"].(map[string]any)
	ok, flag := stats["checksOK"].(int), stats["checksFlag"].(int)
	// OK: CPU, Disk, GPU, exit 0, exit 3010  -> 5 ; flagged: none (battery is Unknown)
	if ok != 5 || flag != 0 {
		t.Errorf("checksOK=%d checksFlag=%d, want 5 and 0", ok, flag)
	}
}

func TestPrepLogAcceptsUTF8BOM(t *testing.T) {
	doc, _, err := parsePrepLog([]byte("\ufeff" + samplePrepLog))
	if err != nil {
		t.Fatalf("log with UTF-8 BOM rejected: %v", err)
	}
	if doc["host"] != "LAPTOP-TEST01" {
		t.Errorf("host = %v", doc["host"])
	}
}

// End-to-end through the public ingest endpoint: a BOM-prefixed log with an
// Unknown battery status and a reboot-required exit code must be accepted
// with no flagged checks.
func TestPostPrepLogNoFalseIssues(t *testing.T) {
	ts := newTestServer(t)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/prep-logs", strings.NewReader("\ufeff"+samplePrepLog))
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("X-File-Name", "LAPTOP-TEST01_prep.log")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out["checksFlag"] != float64(0) {
		t.Errorf("checksFlag = %v, want 0", out["checksFlag"])
	}
	if iss, _ := out["issues"].([]any); len(iss) != 0 {
		t.Errorf("issues = %v, want none", iss)
	}
}
