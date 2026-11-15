package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
)

// prefChoices lists every known preference and its allowed values. The first
// value is the default.
var prefChoices = map[string][]string{
	"theme":   {"system", "light", "dark"},
	"density": {"comfortable", "compact"},
}

// Prefs are a user's effective UI preferences, defaults filled in.
type Prefs struct {
	Theme   string `json:"theme"`
	Density string `json:"density"`
}

func effectivePrefs(stored map[string]string) Prefs {
	get := func(k string) string {
		if v, ok := stored[k]; ok {
			return v
		}
		return prefChoices[k][0]
	}
	return Prefs{Theme: get("theme"), Density: get("density")}
}

func readPrefs(ctx context.Context, q querier, userID string) (map[string]string, error) {
	var raw string
	err := q.QueryRowContext(ctx, `SELECT prefs FROM users WHERE id = ?`, userID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	stored := map[string]string{}
	return stored, json.Unmarshal([]byte(raw), &stored)
}

// UserPrefs returns userID's effective preferences.
func (s *Store) UserPrefs(ctx context.Context, userID string) (Prefs, error) {
	stored, err := readPrefs(ctx, s.r, userID)
	return effectivePrefs(stored), err
}

// UpdatePrefs validates patch, merges it into userID's stored preferences in
// one write and returns the effective result. Nothing is saved on error.
func (s *Store) UpdatePrefs(ctx context.Context, userID string, patch map[string]any) (Prefs, error) {
	clean := map[string]string{}
	for _, k := range slices.Sorted(maps.Keys(patch)) {
		choices, ok := prefChoices[k]
		if !ok {
			return Prefs{}, invalid(k, "Unknown preference.")
		}
		v, ok := patch[k].(string)
		if !ok {
			return Prefs{}, invalid(k, "Must be a string.")
		}
		if !slices.Contains(choices, v) {
			return Prefs{}, invalid(k, fmt.Sprintf("%q is not a valid choice.", v))
		}
		clean[k] = v
	}
	var out Prefs
	err := s.tx(ctx, func(tx *sql.Tx) error {
		stored, err := readPrefs(ctx, tx, userID)
		if err != nil {
			return err
		}
		maps.Copy(stored, clean)
		raw, _ := json.Marshal(stored)
		if _, err := tx.ExecContext(ctx, `UPDATE users SET prefs = ? WHERE id = ?`, string(raw), userID); err != nil {
			return err
		}
		out = effectivePrefs(stored)
		return nil
	})
	return out, err
}
