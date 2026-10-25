package taskplugin

import (
	"context"
	"eflow.local/new-api/model"
	"eflow.local/new-api/pkg/jsplugin"
	"eflow.local/new-api/plugins"
	"gorm.io/gorm"
)

// Default owns management and admitted plugin task routing. It is deliberately
// separate from the offline DefaultRegistry; production starts disabled.
var Default = NewManager(model.SQLTaskPluginStore{DB: func() *gorm.DB { return model.DB }}, factorySources())

func init() {
	model.PlanTaskPricingMigration = Default.PlanPricingMigration
	model.ValidateTaskPricingTargets = Default.ValidatePricingTargets
	model.ResolveOfflineTaskPricingUsage = Default.OfflineTaskPricingUsage
	model.ResolveBillingUsageBindings = func(ctx context.Context, channels []model.Channel) (map[string]model.BillingUsageInfo, error) {
		return Default.BillingUsageInfos(ctx, channels)
	}
}

func factorySources() map[string]string {
	sources := make(map[string]string)
	for _, meta := range jsplugin.DefaultRegistry.Snapshot().Factory {
		source, err := plugins.Source(meta.Key)
		if err != nil {
			panic(err)
		} // Embedded build-time assets only.
		sources[meta.Key] = source
	}
	return sources
}
