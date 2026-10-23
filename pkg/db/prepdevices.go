package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type PrepDevice struct {
	ID           int64     `json:"id"`
	DeviceKey    string    `json:"deviceKey"`
	ComputerName string    `json:"computerName"`
	Summary      string    `json:"-"`
	Raw          string    `json:"-"`
	FileName     string    `json:"fileName"`
	UploaderIP   string    `json:"-"`
	Runs         int64     `json:"runs"`
	FirstSeenAt  time.Time `json:"firstSeenAt"`
	LastSeenAt   time.Time `json:"lastSeenAt"`
}

func (s *Store) UpsertPrepDevice(ctx context.Context, d PrepDevice, now time.Time) (PrepDevice, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO prep_devices (device_key, computer_name, summary, raw, file_name, uploader_ip, runs, first_seen_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?)
		ON CONFLICT(device_key) DO UPDATE SET
			computer_name = excluded.computer_name,
			summary       = excluded.summary,
			raw           = excluded.raw,
			file_name     = excluded.file_name,
			uploader_ip   = excluded.uploader_ip,
			runs          = prep_devices.runs + 1,
			last_seen_at  = excluded.last_seen_at`,
		d.DeviceKey, d.ComputerName, d.Summary, d.Raw, d.FileName, d.UploaderIP,
		now.Unix(), now.Unix())
	if err != nil {
		return PrepDevice{}, fmt.Errorf("upsert prep device: %w", err)
	}
	_, _ = res, err
	row, err := s.PrepDeviceByKey(ctx, d.DeviceKey)
	if err != nil {
		return PrepDevice{}, err
	}
	return row, nil
}

func (s *Store) PrepDeviceByKey(ctx context.Context, key string) (PrepDevice, error) {
	var d PrepDevice
	var fa, la int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, device_key, computer_name, COALESCE(summary,''), COALESCE(raw,''), file_name,
		       COALESCE(uploader_ip,''), runs, first_seen_at, last_seen_at
		  FROM prep_devices WHERE device_key = ?`, key).
		Scan(&d.ID, &d.DeviceKey, &d.ComputerName, &d.Summary, &d.Raw, &d.FileName, &d.UploaderIP, &d.Runs, &fa, &la)
	if errors.Is(err, sql.ErrNoRows) {
		return PrepDevice{}, fmt.Errorf("prep device not found")
	}
	if err != nil {
		return PrepDevice{}, err
	}
	d.FirstSeenAt = time.Unix(fa, 0)
	d.LastSeenAt = time.Unix(la, 0)
	return d, nil
}

func (s *Store) ListPrepDevices(ctx context.Context) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, device_key, computer_name, file_name, runs, first_seen_at, last_seen_at, COALESCE(summary,'')
		  FROM prep_devices ORDER BY last_seen_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var (
			id, fa, la, runs         int64
			key, name, file, summary string
		)
		if err := rows.Scan(&id, &key, &name, &file, &runs, &fa, &la, &summary); err != nil {
			return nil, err
		}
		item := map[string]any{
			"id":           id,
			"deviceKey":    key,
			"computerName": name,
			"fileName":     file,
			"runs":         runs,
			"firstSeenAt":  time.Unix(fa, 0).UTC().Format(time.RFC3339),
			"lastSeenAt":   time.Unix(la, 0).UTC().Format(time.RFC3339),
		}
		var blob struct {
			Overview struct {
				Manufacturer string `json:"manufacturer"`
				Model        string `json:"model"`
			} `json:"overview"`
			CPU struct {
				Name string `json:"name"`
			} `json:"cpu"`
			RAM struct {
				Text string `json:"text"`
			} `json:"ram"`
			Stats struct {
				ChecksOK   int `json:"checksOK"`
				ChecksFlag int `json:"checksFlag"`
			} `json:"stats"`
			Complete bool   `json:"complete"`
			OS       string `json:"os"`
			Disks    []struct {
				SizeGB float64 `json:"sizeGB"`
			} `json:"disks"`
		}
		if err := json.Unmarshal([]byte(summary), &blob); err == nil {
			item["manufacturer"] = blob.Overview.Manufacturer
			item["model"] = blob.Overview.Model
			item["cpuName"] = blob.CPU.Name
			item["ramText"] = blob.RAM.Text
			item["os"] = blob.OS
			item["complete"] = blob.Complete
			item["checksOK"] = blob.Stats.ChecksOK
			item["checksFlag"] = blob.Stats.ChecksFlag
			tot := 0.0
			for _, d := range blob.Disks {
				tot += d.SizeGB
			}
			item["storageGB"] = int64(tot + 0.5)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) GetPrepDevice(ctx context.Context, id int64) (PrepDevice, error) {
	var d PrepDevice
	var fa, la int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, device_key, computer_name, COALESCE(summary,''), COALESCE(raw,''), file_name,
		       COALESCE(uploader_ip,''), runs, first_seen_at, last_seen_at
		  FROM prep_devices WHERE id = ?`, id).
		Scan(&d.ID, &d.DeviceKey, &d.ComputerName, &d.Summary, &d.Raw, &d.FileName, &d.UploaderIP, &d.Runs, &fa, &la)
	if errors.Is(err, sql.ErrNoRows) {
		return PrepDevice{}, fmt.Errorf("prep device not found")
	}
	if err != nil {
		return PrepDevice{}, err
	}
	d.FirstSeenAt = time.Unix(fa, 0)
	d.LastSeenAt = time.Unix(la, 0)
	return d, nil
}

func (s *Store) DeletePrepDevice(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM prep_devices WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("prep device not found")
	}
	return nil
}
