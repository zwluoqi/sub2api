package service

import (
	"github.com/Wei-Shaw/sub2api/internal/config"
	"slices"
	"strings"
)

type AstraSchedulingActionResult struct {
	Changed bool
	Allowed bool
	Action  string
}

func astraSchedulingAppliesToRequest(s config.AstraRoutingSettings, a *Account, model string, group int64) bool {
	switch s.EffectiveSchedulingMode() {
	case "model":
		return strings.EqualFold(model, "gpt-6-astra") || (a != nil && strings.EqualFold(a.GetMappedModel(model), "gpt-6-astra"))
	case "groups":
		return slices.Contains(s.SchedulingGroupIDs, group)
	default:
		return true
	}
}
