// Cockpit client launcher ("Esprit ICT").
//
// A thin WebView2 shell around the Cockpit UI. In client-only mode
// (COCKPIT_CLIENT_ONLY=1 on the server) only paired clients can load the UI:
//
//   - First launch   — no cockpit-client.json next to the exe: the app opens
//     the pairing page and binds clientMachine()/clientPairSave()
//     so the page can register this device. Pairing consumes the
//     one-time code printed in the server's startup log.
//   - Later launches — the app signs a launch: ?ckid&ts&mach&proof, where
//     proof = HMAC-SHA256(clientKey, "ts|machineHash"), and the
//     server exchanges it for a session cookie (HttpOnly,
//     SameSite=Strict).
//
// Build (Windows):  go build -ldflags="-H windowsgui" -o CockpitClient.exe .
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	webview "github.com/webview/webview_go"
)

const configName = "cockpit-client.json"

var serverURL = "http://z.larping.today"

// detectServer resolves the server address for the whole session:
//  1. COCKPIT_SERVER env var (e.g. https://cockpit.example.com)
//  2. "server" field in cockpit-client.json next to the exe (pre-seed by IT)
//  3. http://localhost (same machine, default port 80)
func detectServer(cfgPath string) string {
	if v := os.Getenv("COCKPIT_SERVER"); v != "" {
		return strings.TrimRight(v, "/")
	}
	if raw, err := os.ReadFile(cfgPath); err == nil {
		var p pairFile
		if json.Unmarshal(raw, &p) == nil && p.Server != "" {
			return strings.TrimRight(p.Server, "/")
		}
	}
	return serverURL
}

// pairFile is the on-disk pairing record, saved with 0600 next to the exe.
// Server is optional: IT can pre-seed a minimal {"server": "https://…"} file
// next to the exe so the client points at the right address before pairing.
type pairFile struct {
	ClientID string `json:"clientId"`
	Key      string `json:"key"` // hex-encoded pairing key, issued by the server
	Machine  string `json:"machine"`
	SavedAt  string `json:"savedAt"`
	Server   string `json:"server,omitempty"`
}

func main() {
	cfgPath := configPath()
	serverURL = detectServer(cfgPath)

	// machineHash identifies this machine: stable, not reversible to the raw
	// names. Salt the two most stable Windows identity strings.
	machine := machineHash()

	pf, err := readPair(cfgPath)
	if err == nil && pf.Machine != machine {
		// Paired on/by another machine image — start over.
		_ = os.Remove(cfgPath)
		pf = nil
		err = errors.New("machine changed, re-pair")
	}

	w := webview.New(false)
	defer w.Destroy()
	w.SetTitle("Esprit ICT")
	w.SetSize(1024, 768, webview.HintNone)

	if err != nil {
		// Unpaired: bind the two functions the pairing page calls, then open it.
		_ = w.Bind("clientMachine", func() string { return machine })
		_ = w.Bind("clientPairSave", func(raw string) (any, error) {
			var p pairFile
			if err := json.Unmarshal([]byte(raw), &p); err != nil {
				return nil, fmt.Errorf("bad pairing data: %w", err)
			}
			if p.ClientID == "" || p.Key == "" {
				return nil, errors.New("incomplete pairing data")
			}
			// The binding always binds to THIS machine — never trust the page
			// payload for the machine field.
			p.Machine = machine
			raw2, _ := json.Marshal(p)
			if p.Server == "" {
				p.Server = serverURL
				raw2, _ = json.Marshal(p)
			}
			if err := savePair(cfgPath, raw2); err != nil {
				return nil, err
			}
			w.Dispatch(func() { w.Navigate(signedURL(p)) })
			return map[string]string{"ok": "1"}, nil
		})
		w.Navigate(serverURL + "/pair-guide.html")
	} else {
		// Paired: prove it and let the server set the session cookie.
		w.Navigate(signedURL(*pf))
	}
	w.Run()
}

// signedURL builds the launch URL carrying the pairing proof.
// (WebView2 can't attach request headers, so the proof rides in the URL;
// the server validates it, strips it via 302, and sets the cookie.)
func signedURL(p pairFile) string {
	key, err := hex.DecodeString(p.Key)
	if err != nil || len(key) == 0 {
		// Corrupt key — treat as unpaired.
		return serverURL + "/pair-guide.html"
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	payload := ts + "|" + p.Machine
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	q := url.Values{
		"ckid":  []string{p.ClientID},
		"ts":    []string{ts},
		"mach":  []string{p.Machine},
		"proof": []string{hex.EncodeToString(mac.Sum(nil))},
	}
	return serverURL + "/?" + q.Encode()
}

func machineHash() string {
	id := os.Getenv("COMPUTERNAME") + "|" + os.Getenv("PROCESSOR_IDENTIFIER")
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

func configPath() string {
	exe, err := os.Executable()
	if err != nil {
		return configName
	}
	return filepath.Join(filepath.Dir(exe), configName)
}

func readPair(path string) (*pairFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p pairFile
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	if p.ClientID == "" || p.Key == "" || p.Machine == "" {
		return nil, errors.New("incomplete pairing file")
	}
	return &p, nil
}

func savePair(path string, raw []byte) error {
	// Atomic-ish: write then rename, permissions 0600.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
