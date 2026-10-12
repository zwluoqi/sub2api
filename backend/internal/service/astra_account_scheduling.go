package service

import (
	"context"
	"go.uber.org/zap"
	"slices"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// Deprecated: probe state is observational and must never mutate scheduling.
func (s *AccountTestService) startAstraAccountScheduling() func() { return func() {} }

func astraAccountSchedulingReady(settings config.AstraRoutingSettings, snapshot AstraGatewayRuntime, id int64, now time.Time) bool {
	if !settings.CookiePool.Enabled || snapshot.Revision != settings.Revision {
		return false
	}
	if !slices.Contains(settings.CookiePool.TargetAccountIDs, id) {
		return false
	}
	if snapshot.RouteExpiresAt != nil && now.Before(*snapshot.RouteExpiresAt) {
		return true
	}
	for _, row := range snapshot.Targets {
		if row.AccountID == id && row.State == "ready" && row.ExpiresAt != nil && now.Before(*row.ExpiresAt) {
			return true
		}
	}
	return false
}

func (s *AccountTestService) syncAstraAccountScheduling(context.Context) {}

// Only explicit non-secret fields: no account credentials, cookies, proxy URLs
// or upstream errors. unchanged states never call this logger.
func astraSchedulingLogFields(settings config.AstraRoutingSettings, snapshot AstraGatewayRuntime, account *Account, ready bool, now time.Time) []zap.Field {
	reason := "source_route_unavailable"
	gateway := ""
	expiry := snapshot.RouteExpiresAt
	for _, row := range snapshot.Targets {
		if row.AccountID == account.ID {
			if expiry == nil {
				expiry = row.ExpiresAt
			}
			if row.Gateway != "" {
				gateway = row.Gateway
			}
			if row.Reason != "" {
				reason = row.Reason
			}
			break
		}
	}
	for _, row := range snapshot.Sources {
		if row.Active {
			gateway = row.Gateway
			break
		}
	}
	if ready {
		if reason == "source_route_unavailable" {
			reason = "source_route_available"
		}
	} else if !settings.CookiePool.Enabled {
		reason = "cookie_pool_disabled"
	} else if snapshot.Revision != settings.Revision {
		reason = "configuration_changed"
	} else if !account.IsOpenAIOAuthLike() || account.Status != StatusActive || account.ParentAccountID != nil || (account.ExpiresAt != nil && !now.Before(*account.ExpiresAt)) {
		reason = "account_ineligible"
	} else if expiry != nil && !now.Before(*expiry) {
		reason = "route_expired"
	}
	remaining := int64(0)
	fields := []zap.Field{zap.Int64("account_id", account.ID), zap.Bool("previous_schedulable", account.Schedulable), zap.Bool("schedulable", ready), zap.String("reason", reason), zap.String("gateway", gateway), zap.String("revision", settings.Revision), zap.String("trigger", "astra_state_sync")}
	if expiry != nil {
		fields = append(fields, zap.Time("route_expires_at", *expiry))
		if now.Before(*expiry) {
			remaining = int64(expiry.Sub(now).Seconds())
		}
	}
	return append(fields, zap.Int64("route_remaining_seconds", remaining))
}
