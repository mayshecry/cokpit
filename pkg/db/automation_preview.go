package db

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"cockpit/pkg/automation"
)

type AutomationPreviewCandidate struct {
	OrderID     int64    `json:"orderId"`
	OrderNumber string   `json:"orderNumber"`
	Customer    string   `json:"customerName,omitempty"`
	Detail      string   `json:"detail"`
	Recipients  []string `json:"recipients"`
	Assignee    string   `json:"assignee,omitempty"`
	Problem     string   `json:"problem,omitempty"`
}

type AutomationPreview struct {
	Rule       automation.Rule              `json:"rule"`
	Matched    int                          `json:"matched"`
	Candidates []AutomationPreviewCandidate `json:"candidates"`
	Warnings   []string                     `json:"warnings,omitempty"`
}

func (s *Store) PreviewAutomation(ctx context.Context, rule automation.Rule, now time.Time, limit int) (AutomationPreview, error) {
	rule = normalizeAutomationRule(rule)
	if err := automation.Validate(rule); err != nil {
		return AutomationPreview{}, err
	}
	if err := s.validateAutomationTargets(ctx, rule); err != nil {
		return AutomationPreview{}, err
	}
	if limit <= 0 || limit > 25 {
		limit = 10
	}

	candidates, err := s.automationCandidates(ctx, rule, now)
	if err != nil {
		return AutomationPreview{}, err
	}
	preview := AutomationPreview{
		Rule:       rule,
		Matched:    len(candidates),
		Candidates: []AutomationPreviewCandidate{},
	}

	if len(candidates) == 0 {
		preview.Warnings = append(preview.Warnings, "No order matches this rule right now. It will still fire on the next matching event.")
	}

	seenProblems := map[string]bool{}
	addProblem := func(msg string) {
		if seenProblems[msg] {
			return
		}
		seenProblems[msg] = true
		preview.Warnings = append(preview.Warnings, msg)
	}

	for i, c := range candidates {
		if i >= limit {
			break
		}
		users, teams, err := s.resolveRecipients(ctx, rule, c.customer)
		if err != nil {
			addProblem(err.Error())
			continue
		}
		row := AutomationPreviewCandidate{
			OrderID:     c.orderID,
			OrderNumber: c.orderNumber,
			Customer:    c.customer,
			Detail:      c.detail,
			Recipients:  []string{},
		}
		switch rule.Action {
		case automation.ActionNotify:
			members, err := s.expandTeams(ctx, teams)
			if err != nil {
				addProblem(err.Error())
				continue
			}
			for _, team := range teams {
				list, err := s.teamMembers(ctx, team)
				if err != nil {
					addProblem(err.Error())
					continue
				}
				if len(list) == 0 {
					addProblem(fmt.Sprintf("Team %q has no members, so it notifies nobody for %s.", team, c.orderNumber))
				}
			}
			recipients := append([]string{}, users...)
			seen := map[string]bool{}
			for _, u := range recipients {
				seen[u] = true
			}
			for _, member := range members {
				if !seen[member] {
					seen[member] = true
					recipients = append(recipients, member)
				}
			}
			sort.Strings(recipients)
			row.Recipients = recipients
		case automation.ActionAssign:
			if len(users) > 0 {
				row.Assignee = users[0]
				row.Recipients = []string{users[0]}
			} else {
				member, members, err := s.leastBusyMember(ctx, teams[0])
				if err != nil {
					addProblem(err.Error())
					continue
				}
				if member == "" {
					row.Problem = fmt.Sprintf("Team %q has no members, so nobody would get this order.", teams[0])
					addProblem(row.Problem)
				} else {
					row.Assignee = member
					row.Recipients = []string{member}
					if len(members) > 1 {
						addProblem(fmt.Sprintf("Team %q picks its least busy member — currently @%s for %s.",
							teams[0], member, c.orderNumber))
					}
				}
			}
		case automation.ActionEscalate:
			if len(users) > 0 {
				row.Assignee = users[0]
				row.Recipients = []string{users[0]}
			} else if len(teams) > 0 {
				member, _, err := s.leastBusyMember(ctx, teams[0])
				if err != nil {
					addProblem(err.Error())
					continue
				}
				row.Assignee = member
				if member != "" {
					row.Recipients = []string{member}
				}
			}
		}
		preview.Candidates = append(preview.Candidates, row)
	}

	if len(rule.Customers) == 0 && len(rule.ExcludedCustomers) == 0 && !rule.HasCustomerRouting() {
		preview.Warnings = append(preview.Warnings,
			"This rule applies to every customer. Add customers or exclusions to narrow it.")
	}
	if len(rule.ExcludedCustomers) > 0 {
		preview.Warnings = append(preview.Warnings,
			"Excluded customers: "+strings.Join(rule.ExcludedCustomers, ", ")+".")
	}
	return preview, nil
}
