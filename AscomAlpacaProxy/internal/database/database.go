package database

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite" // Pure Go SQLite driver
)

var (
	db *sql.DB
)

// Init opens the database and ensures the schema exists.
func Init(dbPath string) error {
	var err error
	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Performance tuning
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		return fmt.Errorf("failed to set WAL mode: %w", err)
	}

	// Create table
	schema := `
	CREATE TABLE IF NOT EXISTS telemetry_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp INTEGER NOT NULL,
		voltage REAL,
		current REAL,
		power REAL,
		temp_amb REAL,
		hum_amb REAL,
		dew_point REAL,
		temp_lens REAL,
		pwm1 INTEGER,
		pwm2 INTEGER,
		dc1 INTEGER,
		dc2 INTEGER,
		dc3 INTEGER,
		dc4 INTEGER,
		dc5 INTEGER,
		usbc12 INTEGER,
		usb345 INTEGER,
		adj_conv REAL
	);
	CREATE INDEX IF NOT EXISTS idx_timestamp ON telemetry_log(timestamp);
	`
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	return nil
}

// Close closes the database connection.
func Close() {
	if db != nil {
		db.Close()
	}
}

// TelemetryData mirrors the structure used in other parts of the app
// We define it here or import it? Ideally we import from telemetry, but telemetry imports database, causing cycle.
// So we define a struct here or verify if we can pass discrete values.
// Providing a struct here is safer.
type TelemetryRecord struct {
	Timestamp int64
	Voltage   float64
	Current   float64
	Power     float64
	TempAmb   float64
	HumAmb    float64
	DewPoint  float64
	TempLens  float64
	PWM1      int
	PWM2      int
	DC1       int
	DC2       int
	DC3       int
	DC4       int
	DC5       int
	USBC12    int
	USB345    int
	AdjConv   float64
}

// InsertTelemetry writes a record to the DB.
func InsertTelemetry(r TelemetryRecord) error {
	query := `
	INSERT INTO telemetry_log (
		timestamp, voltage, current, power, temp_amb, hum_amb, dew_point, temp_lens, pwm1, pwm2,
		dc1, dc2, dc3, dc4, dc5, usbc12, usb345, adj_conv
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := db.Exec(query,
		r.Timestamp, r.Voltage, r.Current, r.Power, r.TempAmb, r.HumAmb, r.DewPoint, r.TempLens, r.PWM1, r.PWM2,
		r.DC1, r.DC2, r.DC3, r.DC4, r.DC5, r.USBC12, r.USB345, r.AdjConv,
	)
	return err
}

// GetHistory returns records between start and end timestamps.
// limit: for downsampling (e.g. GET every Nth record could be done in SQL with row_number or MOD,
// but simple filtering is easier first).
// Actually, basic query is fine, downsampling can be done by API or SQL modulo if needed.
func GetHistory(start, end int64) ([]TelemetryRecord, error) {
	query := `SELECT timestamp, voltage, current, power, temp_amb, hum_amb, dew_point, temp_lens, pwm1, pwm2,
	                 dc1, dc2, dc3, dc4, dc5, usbc12, usb345, adj_conv
	          FROM telemetry_log 
	          WHERE timestamp BETWEEN ? AND ? 
	          ORDER BY timestamp ASC`

	rows, err := db.Query(query, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []TelemetryRecord
	for rows.Next() {
		var r TelemetryRecord
		if err := rows.Scan(
			&r.Timestamp, &r.Voltage, &r.Current, &r.Power, &r.TempAmb, &r.HumAmb, &r.DewPoint, &r.TempLens, &r.PWM1, &r.PWM2,
			&r.DC1, &r.DC2, &r.DC3, &r.DC4, &r.DC5, &r.USBC12, &r.USB345, &r.AdjConv,
		); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, nil
}

// GetDistinctDates returns a list of YYYY-MM-DD strings present in the DB.
func GetDistinctDates() ([]string, error) {
	// sqlite 'unixepoch' modifier requires a relatively recent version, modernc should support it.
	query := `SELECT DISTINCT date(timestamp, 'unixepoch', 'localtime') as day FROM telemetry_log ORDER BY day DESC`

	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dates []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			continue
		}
		dates = append(dates, d)
	}
	return dates, nil
}

// PruneOldTelemetry keeps at least the specified number of recorded astronomical nights.
// This deletes data older than the oldest of the last N distinct recorded nights.
// Uses noon-to-noon boundaries (subtracts 12 hours before calculating date).
func PruneOldTelemetry(minNights int) error {
	if minNights <= 0 {
		return nil // Keep everything
	}

	// Step 1: Get the last N distinct recorded nights (noon-to-noon)
	// Subtract 43200 seconds (12 hours) to shift boundary from midnight to noon
	queryDates := `
		SELECT DISTINCT date(timestamp - 43200, 'unixepoch', 'localtime') as night 
		FROM telemetry_log 
		ORDER BY night DESC 
		LIMIT ?`

	rows, err := db.Query(queryDates, minNights)
	if err != nil {
		return fmt.Errorf("failed to query distinct nights: %w", err)
	}
	defer rows.Close()

	var nights []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			continue
		}
		nights = append(nights, d)
	}

	// If we have fewer recorded nights than minNights, keep everything
	if len(nights) < minNights {
		return nil
	}

	// Step 2: Get the oldest night we want to keep
	oldestNightToKeep := nights[len(nights)-1] // Last in the DESC list = oldest of the N

	// Step 3: Delete everything before that night (using same noon-to-noon logic)
	queryDelete := `
		DELETE FROM telemetry_log 
		WHERE date(timestamp - 43200, 'unixepoch', 'localtime') < ?`

	if _, err := db.Exec(queryDelete, oldestNightToKeep); err != nil {
		return fmt.Errorf("failed to prune old records: %w", err)
	}

	return nil
}
