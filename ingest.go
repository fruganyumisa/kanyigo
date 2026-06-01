package main

import (
	"database/sql"
)

func nullFloat(v *float64) interface{} {
	if v == nil {
		return nil
	}
	return *v
}

func nullInt64(v *int64) interface{} {
	if v == nil {
		return nil
	}
	return *v
}

type ingestState struct {
	OffsetBytes int64
	Inode       int64
}

func getIngestState(db *sql.DB, key string) (ingestState, error) {
	var state ingestState
	row := db.QueryRow(`SELECT offset_bytes, inode FROM ingest_state WHERE key = $1`, key)
	switch err := row.Scan(&state.OffsetBytes, &state.Inode); err {
	case sql.ErrNoRows:
		return ingestState{OffsetBytes: 0, Inode: 0}, nil
	case nil:
		return state, nil
	default:
		return ingestState{}, err
	}
}
