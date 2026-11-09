package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"

	"cockpit/pkg/db"
)

const maxPrepLogBytes = 512 << 10

var (
	prepTSLine   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} - (.*)$`)
	prepHostLine = regexp.MustCompile(`^=== Starting laptop prep on ([A-Za-z0-9._-]{1,64}) ===$`)
	prepDoneLine = regexp.MustCompile(`^=== Prep phase complete ===$`)
	prepSection  = regexp.MustCompile(`^----- ([A-Z][A-Z0-9 /_()&+-]{2,60}) -----$`)
	prepSectionN = regexp.MustCompile(`^--- ([A-Z][A-Z0-9 /_()&+-]{2,60}) \[(\d{1,3})\] ---$`)
	prepKV       = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9 /_().()-]{0,48}):\s(.*)$`)
	prepRunCmd   = regexp.MustCompile(`^Run: (\S.*)$`)
	prepExitCode = regexp.MustCompile(`^Exit code: (-?\d+)$`)
	prepTPMLine  = regexp.MustCompile(`^TPM Activated=(\S+) Enabled=(\S+) Spec=(.*)$`)
)

type prepKVEntry struct {
	K string `json:"k"`
	V string `json:"v"`
}

type prepSectionData struct {
	Name  string
	Items [][]prepKVEntry
}

type prepEvent struct {
	T string `json:"t"`
	M string `json:"m"`
}

type prepIssue struct {
	Src  string `json:"src"`
	Text string `json:"text"`
}

func parsePrepLog(data []byte) (map[string]any, string, error) {
	if len(data) < 300 {
		return nil, "", errors.New("file too small to be a prep log")
	}
	if len(data) > maxPrepLogBytes {
		return nil, "", errors.New("file too large (max 512 KB)")
	}
	text := string(data)
	if !utf8.Valid(data) {
		decoded, derr := charmap.Windows1252.NewDecoder().Bytes(data)
		if derr != nil {
			return nil, "", errors.New("file is not valid UTF-8 or Windows-1252 text")
		}
		text = string(decoded)
	}

	text = strings.TrimPrefix(text, "\uFEFF")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	lines := strings.Split(text, "\n")
	tsCount := 0
	sectionCount := 0
	host := ""
	complete := false

	sections := []*prepSectionData{}
	var cur *prepSectionData
	var curItem *[]prepKVEntry

	events := []prepEvent{}
	loose := map[string]string{}
	firstTS, lastTS := "", ""
	updates := []map[string]any{}
	lastRunCmd := ""
	var power, tpmAct, tpmEn, tpmSpec string

	loseSection := func() { cur = nil; curItem = nil }
	pushItem := func() {
		if cur == nil {
			return
		}
		it := []prepKVEntry{}
		cur.Items = append(cur.Items, it)
		curItem = &cur.Items[len(cur.Items)-1]
	}

	for _, ln := range lines {
		ln = strings.TrimRight(ln, " \t")
		if ln == "" {
			continue
		}
		parts := prepTSLine.FindStringSubmatch(ln)
		if parts == nil {
			continue
		}
		tsCount++
		body := strings.TrimSpace(parts[1])
		stamp := ln[:19]
		if firstTS == "" {
			firstTS = stamp
		}
		lastTS = stamp

		if m := prepHostLine.FindStringSubmatch(body); m != nil {
			host = m[1]
			continue
		}
		if prepDoneLine.MatchString(body) {
			complete = true
			events = append(events, prepEvent{T: stamp, M: body})
			loseSection()
			continue
		}
		if m := prepSection.FindStringSubmatch(body); m != nil {
			sectionCount++
			cur = &prepSectionData{Name: m[1], Items: [][]prepKVEntry{}}
			sections = append(sections, cur)
			pushItem()
			continue
		}
		if m := prepSectionN.FindStringSubmatch(body); m != nil {
			n, _ := strconv.Atoi(m[2])
			sectionCount++
			if cur != nil && cur.Name == m[1] {
				for len(cur.Items) < n {
					pushItem()
				}
			} else {
				cur = &prepSectionData{Name: m[1], Items: [][]prepKVEntry{}}
				sections = append(sections, cur)
				for len(cur.Items) < n {
					pushItem()
				}
			}
			continue
		}

		if m := prepTPMLine.FindStringSubmatch(body); m != nil {
			tpmAct, tpmEn, tpmSpec = m[1], m[2], strings.TrimSpace(m[3])
			events = append(events, prepEvent{T: stamp, M: body})
			continue
		}
		if strings.HasPrefix(body, "Power: ") {
			power = strings.TrimPrefix(body, "Power: ")
			events = append(events, prepEvent{T: stamp, M: body})
			continue
		}
		if m := prepRunCmd.FindStringSubmatch(body); m != nil {
			lastRunCmd = m[1]
			events = append(events, prepEvent{T: stamp, M: body})
			continue
		}
		if m := prepExitCode.FindStringSubmatch(body); m != nil {
			if lastRunCmd != "" {
				updates = append(updates, map[string]any{"cmd": lastRunCmd, "code": strings.TrimSpace(m[1])})
				lastRunCmd = ""
			} else {
				events = append(events, prepEvent{T: stamp, M: body})
			}
			continue
		}

		if cur != nil {
			if m := prepKV.FindStringSubmatch(body); m != nil {
				k := strings.TrimSpace(m[1])
				v := strings.TrimSpace(m[2])
				if curItem != nil {
					*curItem = append(*curItem, prepKVEntry{K: k, V: v})
				}
				continue
			}
		} else if m := prepKV.FindStringSubmatch(body); m != nil {
			k := m[1]
			if k == "CPU" || k == "RAM" || k == "OS" {
				loose[k] = strings.TrimSpace(m[2])
				continue
			}
		}
		events = append(events, prepEvent{T: stamp, M: body})
	}

	if host == "" {
		return nil, "", errors.New("missing '=== Starting laptop prep on <HOST> ===' header")
	}
	if tsCount < 10 {
		return nil, "", errors.New("not enough timestamped lines — not a prep log")
	}
	if sectionCount < 3 {
		return nil, "", errors.New("missing hardware sections — not a complete prep log")
	}

	kvOf := func(secName string, n int) map[string]string {
		for _, sec := range sections {
			if sec.Name == secName && len(sec.Items) > n {
				out := map[string]string{}
				for _, e := range sec.Items[n] {
					out[e.K] = e.V
				}
				return out
			}
		}
		return map[string]string{}
	}
	bySection := func(secName string) []map[string]string {
		out := []map[string]string{}
		for _, sec := range sections {
			if sec.Name != secName {
				continue
			}
			for _, item := range sec.Items {
				m := map[string]string{}
				for _, e := range item {
					m[e.K] = e.V
				}
				if len(m) > 0 {
					out = append(out, m)
				}
			}
		}
		return out
	}

	devID := kvOf("DEVICE ID", 0)
	overview := map[string]any{
		"computerName":      pick(devID, "ComputerName", host),
		"manufacturer":      devID["Manufacturer"],
		"model":             devID["Model"],
		"biosSerial":        devID["BIOS Serial"],
		"motherboardSerial": devID["Motherboard SN"],
		"systemUUID":        devID["System UUID"],
		"assetTag":          devID["Asset Tag"],
		"enclosureSerial":   devID["Enclosure SN"],
	}
	macs := []string{}
	for _, m := range strings.Split(devID["MAC Address(es)"], ",") {
		if m = strings.TrimSpace(m); m != "" {
			macs = append(macs, m)
		}
	}
	overview["macs"] = macs

	cpu := map[string]any{"name": loose["CPU"]}
	if cd := kvOf("CPU DETAIL", 0); len(cd) > 0 {
		cpu = map[string]any{
			"name": pick(cd, "Name", loose["CPU"]), "mfr": cd["Mfr"],
			"maxMHz": cd["MaxMHz"], "cores": cd["Cores"], "threads": cd["Threads"],
			"socket": cd["Socket"], "status": cd["Status"],
		}
	}

	mods := []map[string]any{}
	for _, m := range bySection("MEMORY MODULES") {
		mods = append(mods, map[string]any{
			"bank": m["Bank"], "bytes": m["Bytes"], "mhz": m["MHz"],
			"mfr": m["Mfr"], "part": m["Part"], "serial": m["Serial"],
		})
	}
	ram := map[string]any{"text": loose["RAM"], "modules": mods}

	disks := []map[string]any{}
	for _, d := range bySection("DISK DRIVES") {
		disks = append(disks, map[string]any{
			"model": d["Model"], "sizeBytes": d["SizeBytes"],
			"sizeGB": bytesToGB(d["SizeBytes"]), "status": d["Status"],
			"iface": d["Interface"], "serial": d["Serial"],
		})
	}
	logical := []map[string]any{}
	for _, d := range bySection("LOGICAL DISKS") {
		logical = append(logical, map[string]any{
			"drive": d["Drive"], "sizeBytes": d["SizeBytes"],
			"sizeGB": bytesToGB(d["SizeBytes"]), "freeGB": bytesToGB(d["FreeBytes"]),
			"fs": d["FS"], "label": d["Label"],
		})
	}

	var gpu any
	if g := kvOf("GPU", 0); len(g) > 0 {
		gpu = map[string]any{
			"name": g["Name"], "driver": g["Driver"], "vramBytes": g["VRAM"],
			"resX": g["ResX"], "resY": g["ResY"], "status": g["Status"],
		}
	}
	var batt any
	if b := kvOf("BATTERY WMI", 0); len(b) > 0 {
		batt = map[string]any{
			"name": b["Name"], "designMWh": b["Design_mWh"], "fullMWh": b["Full_mWh"],
			"chargePct": b["ChargePct"], "battStatus": b["BattStatus"], "status": b["Status"],
		}
	}
	var bios any
	if b := kvOf("BIOS", 0); len(b) > 0 {
		bios = map[string]any{"mfr": b["Mfr"], "smbiosVer": b["SMBIOSVer"], "version": b["Version"], "releaseDate": b["ReleaseDate"]}
	}
	var mobo any
	if m := kvOf("MOTHERBOARD", 0); len(m) > 0 {
		mobo = map[string]any{"mfr": m["Mfr"], "product": m["Product"], "version": m["Version"], "serial": m["Serial"]}
	}
	netw := []map[string]any{}
	for _, n := range bySection("NETWORK ADAPTERS") {
		netw = append(netw, map[string]any{
			"name": n["Name"], "speed": n["Speed"], "mac": n["MAC"], "connStatus": n["ConnStatus"],
		})
	}

	checksOK, checksFlag := 0, 0
	issues := []prepIssue{}
	for _, sec := range sections {
		for i, item := range sec.Items {
			label := sec.Name
			if len(sec.Items) > 1 {
				label = fmt.Sprintf("%s [%d]", sec.Name, i+1)
			}
			var statusVal, nameVal string
			hasStatus := false
			for _, e := range item {
				if e.K == "Name" || e.K == "Model" || e.K == "Slot" {
					nameVal = e.V
				}
				if e.K == "Status" {
					statusVal = e.V
					hasStatus = true
				}
			}
			if hasStatus {
				switch prepStatusState(statusVal) {
				case prepStatusPass:
					checksOK++
				case prepStatusFail:
					checksFlag++
					if nameVal != "" {
						label += " — " + nameVal
					}
					issues = append(issues, prepIssue{Src: label, Text: "Status: " + statusVal})
				default:

				}
			}
		}
	}
	for _, u := range updates {
		code, _ := u["code"].(string)
		if prepExitCodeOK(code) {
			checksOK++
		} else {
			checksFlag++
			issues = append(issues, prepIssue{
				Src:  "Windows Update",
				Text: fmt.Sprintf("%s exited with code %s", u["cmd"], code),
			})
		}
	}
	secOut := []map[string]any{}
	for _, sec := range sections {
		secOut = append(secOut, map[string]any{"name": sec.Name, "items": sec.Items})
	}

	doc := map[string]any{
		"host":       host,
		"complete":   complete,
		"startedAt":  firstTS,
		"finishedAt": lastTS,
		"overview":   overview,
		"cpu":        cpu,
		"ram":        ram,
		"os":         loose["OS"],
		"disks":      disks,
		"logical":    logical,
		"gpu":        gpu,
		"battery":    batt,
		"bios":       bios,
		"mobo":       mobo,
		"network":    netw,
		"health": map[string]any{
			"power": power,
			"tpm":   map[string]any{"activated": prepBool(tpmAct), "enabled": prepBool(tpmEn), "spec": tpmSpec},
		},
		"updates":  updates,
		"sections": secOut,
		"events":   events,
		"stats": map[string]any{
			"checksOK": checksOK, "checksFlag": checksFlag,
			"sections": len(sections), "events": len(events), "lines": tsCount,
		},
		"issues": issues,
	}
	return doc, host, nil
}

type prepStatus int

const (
	prepStatusNeutral prepStatus = iota
	prepStatusPass
	prepStatusFail
)

func prepStatusState(v string) prepStatus {
	s := strings.TrimSpace(v)
	switch {
	case strings.EqualFold(s, "OK"):
		return prepStatusPass
	case s == "", strings.EqualFold(s, "Unknown"), strings.EqualFold(s, "N/A"):
		return prepStatusNeutral
	default:
		return prepStatusFail
	}
}

func prepExitCodeOK(code string) bool {
	return code == "0" || code == "3010" || code == "2147942487"
}

func prepBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "-1", "yes":
		return true
	default:
		return false
	}
}

func pick(m map[string]string, k, fallback string) string {
	if v, ok := m[k]; ok && v != "" {
		return v
	}
	return fallback
}

func bytesToGB(s string) float64 {
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || n <= 0 {
		return 0
	}
	return float64(int64(n/(1<<30)*10+0.5)) / 10
}

func jsonString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func (s *Server) handlePostPrepLog(w http.ResponseWriter, r *http.Request) {
	ok, _ := s.loginLimiter.Allow("prep:"+clientIP(r), 30, time.Minute)
	if !ok {
		s.writeError(w, http.StatusTooManyRequests, "rate_limited", "Slow down — max 30 submissions per minute.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPrepLogBytes+4096)
	var payload []byte
	var fileName string

	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		f, hdr, err := r.FormFile("file")
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "no_file", "multipart/form-data expected with a 'file' field.")
			return
		}
		defer f.Close()
		if hdr != nil {
			fileName = hdr.Filename
		}
		buf, err := io.ReadAll(io.LimitReader(f, maxPrepLogBytes+1))
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "read_failed", "Could not read the uploaded log.")
			return
		}
		payload = buf
	} else {
		buf, err := io.ReadAll(io.LimitReader(r.Body, maxPrepLogBytes+1))
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "read_failed", "Could not read the body.")
			return
		}
		payload = buf
		if fn := r.Header.Get("X-File-Name"); fn != "" {
			fileName = fn
		}
	}
	if len(bytes.TrimSpace(payload)) == 0 {
		s.writeError(w, http.StatusBadRequest, "empty", "The upload is empty.")
		return
	}

	doc, host, err := parsePrepLog(payload)
	if err != nil {
		s.writeError(w, http.StatusUnprocessableEntity, "not_a_prep_log",
			"Rejected: this does not look like an Esprit prep log ("+err.Error()+").")
		return
	}

	ov, _ := doc["overview"].(map[string]any)
	computerName, _ := ov["computerName"].(string)
	if computerName == "" {
		computerName = host
	}
	deviceKey := ""
	if uuid, _ := ov["systemUUID"].(string); uuid != "" {
		deviceKey = "UUID:" + strings.ToUpper(strings.TrimSpace(uuid))
	} else {
		deviceKey = "NAME:" + strings.ToUpper(computerName)
	}
	if fileName == "" {
		fileName = host + "_prep.log"
	}

	row, err := s.store.UpsertPrepDevice(r.Context(), db.PrepDevice{
		DeviceKey:    deviceKey,
		ComputerName: computerName,
		Summary:      jsonString(doc),
		Raw:          string(normalizeText(payload)),
		FileName:     fileName,
		UploaderIP:   clientIP(r),
	}, s.now())
	if err != nil {
		s.writeInternalError(w, r, err)
		return
	}
	stats, _ := doc["stats"].(map[string]any)
	s.writeJSON(w, http.StatusCreated, map[string]any{
		"ok":           true,
		"id":           row.ID,
		"host":         host,
		"computerName": computerName,
		"complete":     doc["complete"],
		"sections":     stats["sections"],
		"checksOK":     stats["checksOK"],
		"checksFlag":   stats["checksFlag"],
		"issues":       doc["issues"],
		"runs":         row.Runs,
	})
}

func normalizeText(b []byte) []byte {
	if utf8.Valid(b) {
		return b
	}
	decoded, err := charmap.Windows1252.NewDecoder().Bytes(b)
	if err != nil {
		return b
	}
	return decoded
}

func (s *Server) handleListPrepDevices(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListPrepDevices(r.Context())
	if err != nil {
		s.writeInternalError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"devices": items})
}

func (s *Server) handleGetPrepDevice(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.writeError(w, http.StatusBadRequest, "bad_id", "Invalid device id.")
		return
	}
	row, err := s.store.GetPrepDevice(r.Context(), id)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "not_found", "Device not found.")
		return
	}
	var doc map[string]any
	_ = json.Unmarshal([]byte(row.Summary), &doc)
	s.writeJSON(w, http.StatusOK, map[string]any{
		"id":        row.ID,
		"deviceKey": row.DeviceKey,
		"fileName":  row.FileName,
		"runs":      row.Runs,
		"firstSeen": row.FirstSeenAt.UTC().Format(time.RFC3339),
		"lastSeen":  row.LastSeenAt.UTC().Format(time.RFC3339),
		"doc":       doc,
		"raw":       row.Raw,
	})
}

func (s *Server) handleDeletePrepDevice(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.writeError(w, http.StatusBadRequest, "bad_id", "Invalid device id.")
		return
	}
	if err := s.store.DeletePrepDevice(r.Context(), id); err != nil {
		s.writeError(w, http.StatusNotFound, "not_found", "Device not found.")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
