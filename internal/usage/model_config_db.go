package usage

import (
	"database/sql"
	"strings"
	"time"

	sqlmodelconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/storage/sqlite/modelconfig"
)

// Compatibility bridge contract:
// - Owner: model catalog / model settings boundary.
// - Real implementation: internal/storage/sqlite/modelconfig and internal/management/settings/modelconfig.
// - Allowed callers: legacy adapters still being migrated; new management/runtime code should use modelconfig settings first.
// - Exit condition: remaining callers move to modelconfig settings or narrower bridges; do not add new imports here.
type ModelConfigRow = sqlmodelconfig.ModelConfigRow
type ModelOwnerPresetRow = sqlmodelconfig.ModelOwnerPresetRow
type AuthGroupOwnerMappingRow = sqlmodelconfig.AuthGroupOwnerMappingRow

func initModelConfigTables(db *sql.DB) {
	sqlmodelconfig.InitTables(db)
}

func modelConfigStore() sqlmodelconfig.Store {
	return sqlmodelconfig.NewStore(getDB())
}

func modelConfigStoreForTenant(tenantID string) sqlmodelconfig.Store {
	return sqlmodelconfig.NewTenantStore(getDB(), tenantID)
}

func normalizeModelOwnerValue(value string) string {
	return sqlmodelconfig.NormalizeModelOwnerValue(value)
}

func normalizeAuthGroupKey(value string) string {
	return sqlmodelconfig.NormalizeAuthGroupKey(value)
}

func normalizePricingMode(mode string) string {
	return sqlmodelconfig.NormalizePricingMode(mode)
}

func normalizeModelModalities(values []string) []string {
	return sqlmodelconfig.NormalizeModelModalities(values)
}

func ownerLabelForValue(value string) string {
	return sqlmodelconfig.OwnerLabelForValue(value)
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func intToBool(value int) bool {
	return value != 0
}

func upsertLegacyPricingIntoModelConfig(db *sql.DB, modelID string, input, output, cached float64, updatedAt string) {
	upsertLegacyPricingIntoModelConfigForTenant(db, systemTenantID, modelID, input, output, cached, updatedAt)
}

func upsertLegacyPricingIntoModelConfigForTenant(db *sql.DB, tenantID, modelID string, input, output, cached float64, updatedAt string) {
	sqlmodelconfig.UpsertLegacyPricingIntoModelConfigForTenant(db, tenantID, modelID, input, output, cached, updatedAt)
}

func ListModelConfigs() []ModelConfigRow { return ListModelConfigsForTenant(systemTenantID) }
func ListModelConfigsForTenant(tenantID string) []ModelConfigRow {
	return modelConfigStoreForTenant(tenantID).ListModelConfigs()
}
func GetModelConfig(modelID string) (ModelConfigRow, bool) {
	return GetModelConfigForTenant(systemTenantID, modelID)
}
func GetModelConfigForTenant(tenantID, modelID string) (ModelConfigRow, bool) {
	return modelConfigStoreForTenant(tenantID).GetModelConfig(modelID)
}

func UpsertModelConfig(row ModelConfigRow) error {
	return UpsertModelConfigForTenant(systemTenantID, row)
}
func UpsertModelConfigForTenant(tenantID string, row ModelConfigRow) error {
	store := modelConfigStoreForTenant(tenantID)
	modelID := strings.TrimSpace(row.ModelID)
	if modelID == "" {
		return store.UpsertModelConfig(row)
	}
	row.ModelID = modelID
	if err := store.UpsertModelConfig(row); err != nil {
		return err
	}

	saved, ok := store.GetModelConfig(modelID)
	if !ok {
		return nil
	}

	if saved.PricingMode == "token" {
		if err := UpsertModelPricingV2ForTenant(tenantID,
			saved.ModelID,
			saved.InputPricePerMillion,
			saved.OutputPricePerMillion,
			saved.CachedPricePerMillion,
			saved.CacheReadPricePerMillion,
			saved.CacheWritePricePerMillion,
		); err != nil {
			return err
		}
	} else if err := DeleteModelPricingForTenant(tenantID, saved.ModelID); err != nil {
		return err
	}

	if saved.OwnedBy != "" {
		if err := UpsertModelOwnerPresetForTenant(tenantID, ModelOwnerPresetRow{
			Value:   saved.OwnedBy,
			Label:   ownerLabelForValue(saved.OwnedBy),
			Enabled: true,
		}); err != nil {
			return err
		}
	}

	return nil
}

func DeleteModelConfig(modelID string) error {
	return DeleteModelConfigForTenant(systemTenantID, modelID)
}
func DeleteModelConfigForTenant(tenantID, modelID string) error {
	modelID = strings.TrimSpace(modelID)
	if err := modelConfigStoreForTenant(tenantID).DeleteModelConfig(modelID); err != nil {
		return err
	}
	return DeleteModelPricingForTenant(tenantID, modelID)
}

func ListModelOwnerPresets() []ModelOwnerPresetRow {
	return ListModelOwnerPresetsForTenant(systemTenantID)
}
func ListModelOwnerPresetsForTenant(tenantID string) []ModelOwnerPresetRow {
	return modelConfigStoreForTenant(tenantID).ListModelOwnerPresets()
}

func GetModelOwnerPreset(value string) (ModelOwnerPresetRow, bool) {
	return GetModelOwnerPresetForTenant(systemTenantID, value)
}
func GetModelOwnerPresetForTenant(tenantID, value string) (ModelOwnerPresetRow, bool) {
	return modelConfigStoreForTenant(tenantID).GetModelOwnerPreset(value)
}

func UpsertModelOwnerPreset(row ModelOwnerPresetRow) error {
	return UpsertModelOwnerPresetForTenant(systemTenantID, row)
}
func UpsertModelOwnerPresetForTenant(tenantID string, row ModelOwnerPresetRow) error {
	return modelConfigStoreForTenant(tenantID).UpsertModelOwnerPreset(row)
}

func ReplaceModelOwnerPresets(rows []ModelOwnerPresetRow) error {
	return ReplaceModelOwnerPresetsForTenant(systemTenantID, rows)
}
func ReplaceModelOwnerPresetsForTenant(tenantID string, rows []ModelOwnerPresetRow) error {
	return modelConfigStoreForTenant(tenantID).ReplaceModelOwnerPresets(rows)
}

func ListAuthGroupOwnerMappings() []AuthGroupOwnerMappingRow {
	return ListAuthGroupOwnerMappingsForTenant(systemTenantID)
}
func ListAuthGroupOwnerMappingsForTenant(tenantID string) []AuthGroupOwnerMappingRow {
	return modelConfigStoreForTenant(tenantID).ListAuthGroupOwnerMappings()
}

func GetAuthGroupOwnerMapping(authGroup string) (AuthGroupOwnerMappingRow, bool) {
	return GetAuthGroupOwnerMappingForTenant(systemTenantID, authGroup)
}
func GetAuthGroupOwnerMappingForTenant(tenantID, authGroup string) (AuthGroupOwnerMappingRow, bool) {
	return modelConfigStoreForTenant(tenantID).GetAuthGroupOwnerMapping(authGroup)
}

func UpsertAuthGroupOwnerMapping(row AuthGroupOwnerMappingRow) error {
	return UpsertAuthGroupOwnerMappingForTenant(systemTenantID, row)
}
func UpsertAuthGroupOwnerMappingForTenant(tenantID string, row AuthGroupOwnerMappingRow) error {
	return modelConfigStoreForTenant(tenantID).UpsertAuthGroupOwnerMapping(row)
}

func DeleteAuthGroupOwnerMapping(authGroup string) error {
	return DeleteAuthGroupOwnerMappingForTenant(systemTenantID, authGroup)
}
func DeleteAuthGroupOwnerMappingForTenant(tenantID, authGroup string) error {
	return modelConfigStoreForTenant(tenantID).DeleteAuthGroupOwnerMapping(authGroup)
}
