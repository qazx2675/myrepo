// Package audit records who did what for later review (M8 will build the UI;
// M1~M4 just needs writes to not be forgotten later).
package audit

import "database/sql"

func Log(db *sql.DB, userID int64, action, target, result string) {
	_, _ = db.Exec(`INSERT INTO audit_log (user_id, action, target, result) VALUES (?, ?, ?, ?)`, userID, action, target, result)
}
