package db

import (
	"context"
	"errors"
	"time"
)

var ErrPairCodeInvalid = errors.New("invalid, consumed or expired pairing code")

type PairCode struct {
	Code      string    `json:"code"`
	Username  string    `json:"username"`
	CreatedBy string    `json:"createdBy"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func CanonicalPairCode(code string) string {
	out := make([]byte, 0, 10)
	n := 0
	for i := 0; i < len(code); i++ {
		c := code[i]
		switch {
		case c >= 'a' && c <= 'z':
			c -= 'a' - 'A'
		}
		if (c < '0' || c > '9') && (c < 'A' || c > 'Z') {
			continue
		}
		if n > 0 && n%4 == 0 {
			out = append(out, '-')
		}
		out = append(out, c)
		n++
	}
	return string(out)
}

func (s *Store) IssuePairCode(ctx context.Context, pc PairCode) error {
	pc.Code = CanonicalPairCode(pc.Code)

	_, _ = s.db.ExecContext(ctx,
		`DELETE FROM pair_codes WHERE used_at != 0 OR expires_at < ?`, pc.CreatedAt.Add(-7*24*time.Hour).Unix())
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO pair_codes (code, username, created_by, created_at, expires_at, used_at)
		 VALUES (?, ?, ?, ?, ?, 0)`,
		pc.Code, pc.Username, pc.CreatedBy, pc.CreatedAt.Unix(), pc.ExpiresAt.Unix())
	return err
}

func (s *Store) ActivePairCode(ctx context.Context, username string, now time.Time) (PairCode, error) {
	var pc PairCode
	var created, expires int64
	err := s.db.QueryRowContext(ctx,
		`SELECT code, username, created_by, created_at, expires_at FROM pair_codes
		 WHERE username = ? AND used_at = 0 AND expires_at > ?
		 ORDER BY created_at DESC LIMIT 1`, username, now.Unix()).Scan(&pc.Code, &pc.Username, &pc.CreatedBy, &created, &expires)
	if err != nil {
		return PairCode{}, err
	}
	pc.CreatedAt, pc.ExpiresAt = time.Unix(created, 0).UTC(), time.Unix(expires, 0).UTC()
	return pc, nil
}

func (s *Store) ConsumePairCode(ctx context.Context, code string, now time.Time) (PairCode, error) {
	code = CanonicalPairCode(code)
	res, err := s.db.ExecContext(ctx,
		`UPDATE pair_codes SET used_at = ? WHERE code = ? AND used_at = 0`, now.Unix(), code)
	if err != nil {
		return PairCode{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return PairCode{}, ErrPairCodeInvalid
	}
	var pc PairCode
	var created, expires int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT code, username, created_by, created_at, expires_at FROM pair_codes WHERE code = ?`, code).
		Scan(&pc.Code, &pc.Username, &pc.CreatedBy, &created, &expires); err != nil {
		return PairCode{}, ErrPairCodeInvalid
	}
	pc.CreatedAt, pc.ExpiresAt = time.Unix(created, 0).UTC(), time.Unix(expires, 0).UTC()
	if now.Unix() > pc.ExpiresAt.Unix() {
		return PairCode{}, ErrPairCodeInvalid
	}
	return pc, nil
}

func (s *Store) AnyActivePairCode(ctx context.Context, now time.Time) (PairCode, error) {
	var pc PairCode
	var created, expires int64
	err := s.db.QueryRowContext(ctx,
		`SELECT code, username, created_by, created_at, expires_at FROM pair_codes
		 WHERE used_at = 0 AND expires_at > ? ORDER BY created_at DESC LIMIT 1`, now.Unix()).
		Scan(&pc.Code, &pc.Username, &pc.CreatedBy, &created, &expires)
	if err != nil {
		return PairCode{}, err
	}
	pc.CreatedAt, pc.ExpiresAt = time.Unix(created, 0).UTC(), time.Unix(expires, 0).UTC()
	return pc, nil
}
