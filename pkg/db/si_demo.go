package db

import (
	"context"
	"fmt"
	"time"

	"cockpit/pkg/si"
)

// SeedDemoSI idempotently seeds customer 94828 with four projects and four
// SIs showing the four lifecycle stages Requested/Development/Testing/Live. It
// returns the number of seeded top-level entities (the customer plus the SIs;
// projects are supporting fixtures) and mirrors the demo pattern of
// SeedDemoChecklists (no-op when data exists).

func (s *Store) SeedDemoSI(ctx context.Context, now time.Time) (int, error) {
	var existing int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM customers`).Scan(&existing); err != nil {
		return 0, fmt.Errorf("count customers: %w", err)
	}
	if existing > 0 {
		return 0, nil
	}

	klant, err := s.CreateCustomer(ctx, "94828", "Voorbeeldklant 94828", now)
	if err != nil {
		return 0, fmt.Errorf("seed customer: %w", err)
	}

	projects := []struct {
		code, name, description string
	}{
		{"P-001", "Ordering", "Inkoop- en verkooporders"},
		{"P-002", "Facturatie", "Factuuruitgifte en creditfacturen"},
		{"P-003", "Magazijn", "WMS en voorraadbeheer"},
		{"P-004", "Rapportage", "BI- en rapportage-koppelingen"},
	}
	byCode := map[string]int64{}
	for _, p := range projects {
		created, err := s.CreateProject(ctx, klant.ID, p.code, p.name, p.description, now)
		if err != nil {
			return 0, fmt.Errorf("seed project %s: %w", p.code, err)
		}
		byCode[p.code] = created.ID
	}

	demoSIs := []struct {
		code, name, description string
		primary                    string
		links                      []string
		env                        si.Environment
		transitions                []si.Status
	}{
		{"SI-0001", "EDI inkooporders", "Elektronische inkooporder-koppeling", "P-001", []string{"P-001", "P-003"}, si.EnvironmentProductie, []si.Status{si.StatusInValidation, si.StatusAccepted}},
		{"SI-0002", "Factuuruitgifte", "Uitgifte van facturen naar het klantportaal", "P-002", []string{"P-002"}, si.EnvironmentAcceptatie, []si.Status{si.StatusInValidation}},
		{"SI-0003", "WMS-koppeling", "Voorraad en picking-koppeling met het WMS", "P-003", []string{"P-003"}, si.EnvironmentTest, []si.Status{}},
		{"SI-0004", "BI-extract", "Rapportage-extract naar het BI-platform", "P-004", []string{"P-004"}, si.EnvironmentProductie, []si.Status{si.StatusInValidation, si.StatusAccepted, si.StatusOutphased}},
	}
	// The demo customer counts as a seeded entity too: a full seed returns
	// 1 customer + 4 SIs = 5.
	created := 1
	for _, d := range demoSIs {
		reqID := byCode[d.primary]
		siRec, err := s.CreateSI(ctx, si.CreateSIRequest{
			Code:             d.code,
			Name:             d.name,
			Description:       d.description,
			PrimaryProjectID:  reqID,
			Environment:       d.env,
			Config: &si.SIConfig{
				WorkInstructions: "Volg de standaard werkinstructies voor " + d.name,
				SWI:              "SWI-" + d.code,
				Workflow:         "1. Voorbereiding\n2. Uitvoering\n3. Validatie",
				QCProfile:        "Standaard QC profiel",
				Automations:      "Auto-notificatie bij statuswijziging",
				EscalationFlow:   []string{"NPI", "Operations"},
			},
		}, "demo", now)
		if err != nil {
			return created, fmt.Errorf("seed SI %s: %w", d.code, err)
		}
		if len(d.links) > 1 {
			links := []int64{reqID}
			for _, pc := range d.links {
				if pc == d.primary {
					continue
				}
				links = append(links, byCode[pc])
			}
			if _, err := s.SetSIProjects(ctx, siRec.ID, links, "demo", now); err != nil {
				return created, fmt.Errorf("link SI %s: %w", d.code, err)
			}
		}
		for _, st := range d.transitions {
			if _, err := s.TransitionSI(ctx, siRec.ID, st, "demo-seed", "demo", now); err != nil {
				return created, fmt.Errorf("transition SI %s: %w", d.code, err)
			}
		}
		created++
	}
	return created, nil
}