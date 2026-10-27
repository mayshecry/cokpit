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
var serverURL = ""
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
type pairFile struct {
	ClientID string `json:"clientId"`
	Key      string `json:"key"` 
	Machine  string `json:"machine"`
	SavedAt  string `json:"savedAt"`
	Server   string `json:"server,omitempty"`
}
func main() {
	cfgPath := configPath()
	serverURL = detectServer(cfgPath)
	machine := machineHash()
	pf, err := readPair(cfgPath)
	if err == nil && pf.Machine != machine {
		_ = os.Remove(cfgPath)
		pf = nil
		err = errors.New("machine changed, re-pair")
	}
	w := webview.New(false)
	defer w.Destroy()
	w.SetTitle("Esprit ICT")
	w.SetSize(1024, 768, webview.HintNone)
	if err != nil {
		_ = w.Bind("clientMachine", func() string { return machine })
		_ = w.Bind("clientPairSave", func(raw string) (any, error) {
			var p pairFile
			if err := json.Unmarshal([]byte(raw), &p); err != nil {
				return nil, fmt.Errorf("bad pairing data: %w", err)
			}
			if p.ClientID == "" || p.Key == "" {
				return nil, errors.New("incomplete pairing data")
			}
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
		w.Navigate(signedURL(*pf))
	}
	w.Run()
}
func signedURL(p pairFile) string {
	key, err := hex.DecodeString(p.Key)
	if err != nil || len(key) == 0 {
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
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
