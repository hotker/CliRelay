package usage

import (
	"database/sql"

	log "github.com/sirupsen/logrus"
)

const createRuntimeSettingsTableSQL = `
CREATE TABLE IF NOT EXISTS runtime_settings (
  setting_key TEXT PRIMARY KEY NOT NULL,
  payload     TEXT NOT NULL DEFAULT '{}',
  updated_at  TEXT NOT NULL DEFAULT ''
);
`

func initRuntimeSettingsTable(db *sql.DB) {
	if db == nil {
		return
	}
	if _, err := db.Exec(createRuntimeSettingsTableSQL); err != nil {
		log.Errorf("usage: create runtime_settings table: %v", err)
	}
}
