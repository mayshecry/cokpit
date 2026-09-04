package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"cockpit/pkg/order"
)

var (
	ErrDuplicateProductCode = errors.New("product code already exists")

	ErrDuplicateManual = errors.New("this product's manual is already on the order")

	ErrUnknownAssignee = errors.New("assigned user does not exist")

	ErrBadAnswer = errors.New("answer must be YES, NO or CLEAR")

	ErrBadFlagReason = errors.New("a reason is required to flag a block")

	ErrFlagUnanswered = errors.New("only answered blocks can be flagged")

	ErrManualEdge = errors.New("block is already at that end of the manual")
)

const productCols = `p.id, p.code, p.name, p.description, p.created_by, p.created_at, p.updated_at`

func scanProduct(row interface{ Scan(...any) error }) (order.Product, error) {
	var p order.Product
	var created, updated int64
	if err := row.Scan(&p.ID, &p.Code, &p.Name, &p.Description, &p.CreatedBy, &created, &updated); err != nil {
		return order.Product{}, err
	}
	p.CreatedAt = fromMillis(created)
	p.UpdatedAt = fromMillis(updated)
	return p, nil
}

func (s *Store) CreateProduct(ctx context.Context, code, name, description, createdBy string, now time.Time) (order.Product, error) {
	code, name = strings.TrimSpace(code), strings.TrimSpace(name)
	if code == "" || name == "" {
		return order.Product{}, errors.New("code and name are required")
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM products WHERE code = ?`, code).Scan(&exists); err != nil {
		return order.Product{}, fmt.Errorf("check duplicate product: %w", err)
	}
	if exists > 0 {
		return order.Product{}, ErrDuplicateProductCode
	}
	if createdBy == "" {
		createdBy = "system"
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO products (code, name, description, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		code, name, strings.TrimSpace(description), createdBy, millis(now), millis(now))
	if err != nil {
		return order.Product{}, fmt.Errorf("insert product: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return order.Product{}, fmt.Errorf("last insert id: %w", err)
	}
	return s.getProductRow(ctx, id)
}

func (s *Store) Products(ctx context.Context) ([]order.Product, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT `+productCols+`, (SELECT COUNT(*) FROM manual_blocks b WHERE b.product_id = p.id) AS block_count
FROM products p ORDER BY p.code ASC, p.id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}
	defer rows.Close()

	out := []order.Product{}
	for rows.Next() {
		var p order.Product
		var created, updated, blockCount int64
		if err := rows.Scan(&p.ID, &p.Code, &p.Name, &p.Description, &p.CreatedBy, &created, &updated, &blockCount); err != nil {
			return nil, fmt.Errorf("scan product: %w", err)
		}
		p.CreatedAt = fromMillis(created)
		p.UpdatedAt = fromMillis(updated)
		p.BlockCount = int(blockCount)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) getProductRow(ctx context.Context, id int64) (order.Product, error) {
	var p order.Product
	var created, updated, blockCount int64
	err := s.db.QueryRowContext(ctx,
		`SELECT `+productCols+`, (SELECT COUNT(*) FROM manual_blocks b WHERE b.product_id = p.id) AS block_count
		 FROM products p WHERE p.id = ?`, id).
		Scan(&p.ID, &p.Code, &p.Name, &p.Description, &p.CreatedBy, &created, &updated, &blockCount)
	if errors.Is(err, sql.ErrNoRows) {
		return order.Product{}, ErrNotFound
	}
	if err != nil {
		return order.Product{}, fmt.Errorf("get product: %w", err)
	}
	p.CreatedAt = fromMillis(created)
	p.UpdatedAt = fromMillis(updated)
	p.BlockCount = int(blockCount)
	return p, nil
}

func (s *Store) ProductByID(ctx context.Context, id int64) (order.Product, []order.ManualBlock, error) {
	p, err := s.getProductRow(ctx, id)
	if err != nil {
		return order.Product{}, nil, err
	}
	blocks, err := s.manualBlocks(ctx, id)
	if err != nil {
		return order.Product{}, nil, err
	}
	return p, blocks, nil
}

func (s *Store) manualBlocks(ctx context.Context, productID int64) ([]order.ManualBlock, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, product_id, seq, title, body, assignee, created_at, updated_at
		 FROM manual_blocks WHERE product_id = ? ORDER BY seq ASC, id ASC`, productID)
	if err != nil {
		return nil, fmt.Errorf("list manual blocks: %w", err)
	}
	defer rows.Close()

	blocks := []order.ManualBlock{}
	for rows.Next() {
		var b order.ManualBlock
		var created, updated int64
		if err := rows.Scan(&b.ID, &b.ProductID, &b.Seq, &b.Title, &b.Body, &b.Assignee, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan manual block: %w", err)
		}
		b.CreatedAt = fromMillis(created)
		b.UpdatedAt = fromMillis(updated)
		blocks = append(blocks, b)
	}
	return blocks, rows.Err()
}

func (s *Store) UpdateProduct(ctx context.Context, id int64, code, name, description *string, now time.Time) (order.Product, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return order.Product{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	current, err := scanProduct(tx.QueryRowContext(ctx, `SELECT `+productCols+`, 0 AS block_count FROM products p WHERE p.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return order.Product{}, ErrNotFound
	}
	if err != nil {
		return order.Product{}, fmt.Errorf("get product: %w", err)
	}

	if code != nil {
		*code = strings.TrimSpace(*code)
		if *code == "" {
			return order.Product{}, errors.New("code must not be empty")
		}
		if *code != current.Code {
			var exists int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM products WHERE code = ? AND id != ?`, *code, id).Scan(&exists); err != nil {
				return order.Product{}, fmt.Errorf("check duplicate product: %w", err)
			}
			if exists > 0 {
				return order.Product{}, ErrDuplicateProductCode
			}
		}
		current.Code = *code
	}
	if name != nil {
		*name = strings.TrimSpace(*name)
		if *name == "" {
			return order.Product{}, errors.New("name must not be empty")
		}
		current.Name = *name
	}
	if description != nil {
		current.Description = strings.TrimSpace(*description)
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE products SET code = ?, name = ?, description = ?, updated_at = ? WHERE id = ?`,
		current.Code, current.Name, current.Description, millis(now), id); err != nil {
		return order.Product{}, fmt.Errorf("update product: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return order.Product{}, fmt.Errorf("commit: %w", err)
	}
	return s.getProductRow(ctx, id)
}

func (s *Store) DeleteProduct(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM products WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete product: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

func validateAssignee(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, assignee string) error {
	if assignee == "" {
		return nil
	}
	var exists int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE username = ?`, assignee).Scan(&exists); err != nil {
		return fmt.Errorf("check assignee: %w", err)
	}
	if exists == 0 {
		return ErrUnknownAssignee
	}
	return nil
}
