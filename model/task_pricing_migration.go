package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"eflow.local/new-api/pkg/billingexpr"
	"eflow.local/new-api/setting/billing_setting"
	"gorm.io/gorm"
)

const TaskPricingMigrationOption = "ModelPricingTaskMigrationV1"

type TaskPricingMigrationEntry struct {
	Model     string                         `json:"model"`
	Plugin    string                         `json:"plugin,omitempty"`
	ChannelID int                            `json:"channel_id,omitempty"`
	Status    string                         `json:"status"`
	Reason    string                         `json:"reason,omitempty"`
	Rule      *billing_setting.TaskPriceRule `json:"rule,omitempty"`
}

type TaskPricingMigrationReport struct {
	SourceHash     string                      `json:"source_hash"`
	Candidate      string                      `json:"-"`
	Entries        []TaskPricingMigrationEntry `json:"entries"`
	Blocked        bool                        `json:"blocked"`
	AlreadyApplied bool                        `json:"already_applied"`
}

// Resolver receives a single locked database snapshot, including disabled rows.
// It must not read live runtime views or perform HTTP/provider operations.
var PlanTaskPricingMigration func(context.Context, ModelPricingConfig, []Channel, TaskPluginSnapshot, []TaskPluginSourceSnapshot, bool) (TaskPricingMigrationReport, error)

func taskPricingMigrationWithTx(ctx context.Context, tx *gorm.DB, expected string, apply bool) (TaskPricingMigrationReport, error) {
	var report TaskPricingMigrationReport
	values, err := optionValuesForUpdate(tx)
	if err != nil {
		return report, err
	}
	raw, ok := values[ModelPricingOption]
	if !ok {
		return report, errors.New("ModelPricing must be initialized before task migration")
	}
	var document ModelPricingConfig
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		return report, err
	}
	if document.TaskPriorityVersion == 1 {
		var record Option
		if err := tx.Where("`key` = ?", TaskPricingMigrationOption).First(&record).Error; err != nil {
			return report, fmt.Errorf("task migration provenance missing: %w", err)
		}
		var saved struct {
			SourceHash string `json:"source_hash"`
		}
		if err := json.Unmarshal([]byte(record.Value), &saved); err != nil || saved.SourceHash == "" {
			return report, errors.New("invalid task migration provenance")
		}
		if apply && expected != saved.SourceHash {
			return report, errors.New("task migration preview is stale")
		}
		report.SourceHash, report.AlreadyApplied = saved.SourceHash, true
		return report, nil // Never overwrite post-migration administrator edits.
	}
	if PlanTaskPricingMigration == nil {
		return report, errors.New("offline task price resolver unavailable")
	}
	// No credentials, channel settings or task data are selected.
	var channels []Channel
	if err := lockForUpdate(tx).Select("id", "type", "status", "models", "model_mapping").Order("id").Find(&channels).Error; err != nil {
		return report, err
	}
	snapshot, err := readTaskPluginSnapshot(tx, true)
	if err != nil {
		return report, err
	}
	hashes := make([]string, 0, len(snapshot.Production))
	for _, binding := range snapshot.Production {
		hashes = append(hashes, binding.SourceHash)
	}
	var sources []TaskPluginSourceSnapshot
	if len(hashes) > 0 {
		if err := lockForUpdate(tx).Where("source_hash IN ?", hashes).Find(&sources).Error; err != nil {
			return report, err
		}
	}
	report, err = PlanTaskPricingMigration(ctx, document, channels, snapshot, sources, values["SelfUseModeEnabled"] == "true")
	if err != nil {
		return report, err
	}
	sort.SliceStable(report.Entries, func(i, j int) bool {
		a, b := report.Entries[i], report.Entries[j]
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		if a.Plugin != b.Plugin {
			return a.Plugin < b.Plugin
		}
		return a.ChannelID < b.ChannelID
	})
	// Include routing, fixed identities and all pricing/group inputs in the CAS.
	// Script source is represented by its verified hash in the binding snapshot.
	source, err := json.Marshal(struct {
		Pricing  string
		Channels []Channel
		Revision uint64
		Bindings []TaskPluginProductionBinding
		Groups   map[string]string
		Entries  []TaskPricingMigrationEntry
	}{raw, channels, snapshot.Revision, snapshot.Production, map[string]string{"GroupRatio": values["GroupRatio"], "GroupGroupRatio": values["GroupGroupRatio"], "QuotaPerUnit": values["QuotaPerUnit"], "SelfUseModeEnabled": values["SelfUseModeEnabled"]}, report.Entries})
	if err != nil {
		return report, err
	}
	report.SourceHash = billingexpr.ExprHashString(string(source))
	if !apply {
		return report, nil
	}
	if report.Blocked {
		return report, errors.New("task price migration contains unresolved entries")
	}
	if expected == "" || expected != report.SourceHash {
		return report, errors.New("task migration preview is stale")
	}
	if _, err := unpackModelPricing(report.Candidate); err != nil {
		return report, err
	}
	record, err := json.Marshal(map[string]any{"version": 1, "source_hash": report.SourceHash, "target_hash": billingexpr.ExprHashString(report.Candidate), "original": raw, "entries": report.Entries})
	if err != nil {
		return report, err
	}
	// All-or-nothing with provenance; no individual model is switched early.
	if err := saveOptionValuesWithTx(tx, map[string]string{ModelPricingOption: report.Candidate, TaskPricingMigrationOption: string(record)}); err != nil {
		return report, err
	}
	return report, nil
}

func TaskPricingMigration(ctx context.Context, expected string, apply bool) (TaskPricingMigrationReport, error) {
	optionMutationMutex.Lock()
	defer optionMutationMutex.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var report TaskPricingMigrationReport
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		report, err = taskPricingMigrationWithTx(ctx, tx, expected, apply)
		return err
	})
	if err == nil && apply && !report.AlreadyApplied {
		if publishErr := loadOptionsFromDatabaseLocked(); publishErr != nil {
			err = fmt.Errorf("%w: %w", ErrOptionSavedNotPublished, publishErr)
		}
	}
	return report, err
}
