package adapter

import (
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// pgUUID converts a google/uuid.UUID to a pgtype.UUID.
func pgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: id != uuid.Nil}
}

// parsePGUUID parses a string into a pgtype.UUID.
func parsePGUUID(s string) pgtype.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		return pgtype.UUID{Valid: false}
	}
	return pgUUID(id)
}

// goUUID parses a string into a google/uuid.UUID.
func goUUID(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return id
}

// pgText creates a pgtype.Text from a string (empty → invalid).
func pgText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{Valid: false}
	}
	return pgtype.Text{String: s, Valid: true}
}

// pgTimestamptz creates a pgtype.Timestamptz from a time.Time (zero → invalid).
func pgTimestamptz(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{Valid: false}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// uuidToString formats a [16]byte as a UUID string. Returns empty string for nil.
func uuidToString(b [16]byte) string {
	return uuid.UUID(b).String()
}

// strOrZero returns the string if the pgtype.Text is valid, empty otherwise.
func strOrZero(t pgtype.Text) string {
	if t.Valid {
		return t.String
	}
	return ""
}

// pgTimestamptzPtr returns *time.Time if valid, nil otherwise.
func pgTimestamptzPtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

// pgInt8Ptr returns *int64 if valid, nil otherwise.
func pgInt8Ptr(i pgtype.Int8) *int64 {
	if !i.Valid {
		return nil
	}
	return &i.Int64
}

// numericToFloat64 converts a pgtype.Numeric to float64, returning 0 if invalid.
func numericToFloat64(n pgtype.Numeric) float64 {
	if !n.Valid {
		return 0
	}
	f, _ := n.Float64Value()
	return f.Float64
}

// numericFromFloat64 converts a float64 to pgtype.Numeric via string formatting.
// pgtype.Numeric.Scan does not accept float64 directly ("cannot scan float64"),
// so we use strconv.FormatFloat to produce a decimal string first.
func numericFromFloat64(value float64) (pgtype.Numeric, error) {
	var n pgtype.Numeric
	s := strconv.FormatFloat(value, 'f', -1, 64)
	if err := n.Scan(s); err != nil {
		return pgtype.Numeric{}, err
	}
	return n, nil
}

// numericFromFloat64Ptr converts a non-zero float64 to pgtype.Numeric.
// Returns an invalid (NULL) pgtype.Numeric when value is 0,
// so callers can distinguish "not provided" from "zero".
func numericFromFloat64Ptr(value float64) (pgtype.Numeric, error) {
	if value == 0 {
		return pgtype.Numeric{Valid: false}, nil
	}
	return numericFromFloat64(value)
}
