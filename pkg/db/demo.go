package db

import (
	"context"
	"fmt"
	"time"

	"cockpit/pkg/order"
)

var demoChecklists = []struct {
	number string
	items  []order.ChecklistItemInput
	tick   int
}{
	{
		number: "DEMO-1001",
		items: []order.ChecklistItemInput{
			{Artikel: "ART-1001", Omschrijving: "Wit A4-papier 80gr (5 reams)", Locatie: "PICK-A01", Aantal: 4},
			{Artikel: "ART-1002", Omschrijving: "Sticky notes geel 76x76", Locatie: "PICK-A02", Aantal: 2},
			{Artikel: "ART-1003", Omschrijving: "Balpen blauw 0.7mm (doos 50)", Locatie: "PICK-B01", Aantal: 10},
		},
		tick: -1,
	},
	{
		number: "DEMO-1002",
		items: []order.ChecklistItemInput{
			{Artikel: "ART-2001", Omschrijving: "Stapelbox 400x300x250", Locatie: "PICK-C03", Aantal: 1},
			{Artikel: "ART-2002", Omschrijving: "Opspanband 19mm zwart", Locatie: "PICK-C04", Aantal: 3},
			{Artikel: "ART-2003", Omschrijving: "Luchtkussenfolie rol 50m", Locatie: "PICK-C05", Aantal: 1},
		},

		tick: 1,
	},
	{
		number: "DEMO-1003",
		items: []order.ChecklistItemInput{
			{Artikel: "ART-3001", Omschrijving: "Etiketrol 100x150 wit (500st)", Locatie: "PICK-D01", Aantal: 6},
			{Artikel: "ART-3002", Omschrijving: "Pakkettape bruin 50mm", Locatie: "PICK-D02", Aantal: 2},
		},
		tick: -1,
	},
}

func (s *Store) SeedDemoChecklists(ctx context.Context, now time.Time) (int, error) {
	var existing int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM checklist_items`).Scan(&existing); err != nil {
		return 0, fmt.Errorf("count checklist items: %w", err)
	}
	if existing > 0 {
		return 0, nil
	}

	created := 0
	for _, dc := range demoChecklists {
		o, err := s.CreateOrder(ctx, dc.number, now.Add(24*time.Hour), now, "demo")
		if err != nil {
			return created, fmt.Errorf("create demo order %s: %w", dc.number, err)
		}
		if _, err := s.SetChecklist(ctx, o.ID, dc.items, "demo", now); err != nil {
			return created, fmt.Errorf("seed checklist %s: %w", dc.number, err)
		}
		if dc.tick >= 0 {
			items, err := s.ChecklistItems(ctx, o.ID)
			if err != nil {
				return created, fmt.Errorf("read demo checklist %s: %w", dc.number, err)
			}
			if dc.tick < len(items) {
				if _, err := s.TickChecklistItem(ctx, items[dc.tick].ID, "demo", now); err != nil {
					return created, fmt.Errorf("pre-tick demo line in %s: %w", dc.number, err)
				}
			}
		}
		created++
	}
	return created, nil
}
