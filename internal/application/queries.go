package application

import "icecoreacclimationgate/internal/domain"

func StateOf(c domain.AcclimationCase) map[string]any {
	return map[string]any{"case_id": c.CaseID, "state": c.State, "revision": c.Revision}
}
