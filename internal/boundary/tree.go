// Package boundary holds the administrative tree (ward → constituency →
// county → national).
//
// This is an in-memory tree loaded from a JSON file. The authoritative IEBC
// dataset, admin_units table and API arrive with EPIC 4.4 (T-4.4.1.x).
package boundary

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// Level is an administrative level.
type Level string

const (
	LevelWard         Level = "ward"
	LevelConstituency Level = "constituency"
	LevelCounty       Level = "county"
	LevelNational     Level = "national"
)

// parentLevel gives the required parent level for each level.
var parentLevel = map[Level]Level{
	LevelWard:         LevelConstituency,
	LevelConstituency: LevelCounty,
	LevelCounty:       LevelNational,
}

// ErrUnknownWard is returned when an id is not a known ward.
var ErrUnknownWard = errors.New("boundary: unknown ward")

// Unit is one administrative unit.
type Unit struct {
	ID       int    `json:"id"`
	Level    Level  `json:"level"`
	Name     string `json:"name"`
	ParentID int    `json:"parent_id,omitempty"`
}

// Scope is a ward with its ancestors.
type Scope struct {
	Ward         Unit
	Constituency Unit
	County       Unit
	National     Unit
}

// Tree is an immutable, validated administrative tree.
type Tree struct {
	Version string
	units   map[int]Unit
}

type treeFile struct {
	Version string `json:"version"`
	Units   []Unit `json:"units"`
}

// NewTree validates units and builds a tree. Every non-national unit must
// have a parent at the level directly above it.
func NewTree(version string, units []Unit) (*Tree, error) {
	byID := make(map[int]Unit, len(units))
	for _, u := range units {
		if _, dup := byID[u.ID]; dup {
			return nil, fmt.Errorf("boundary: duplicate unit id %d", u.ID)
		}
		byID[u.ID] = u
	}
	for _, u := range units {
		if u.Level == LevelNational {
			continue
		}
		want, ok := parentLevel[u.Level]
		if !ok {
			return nil, fmt.Errorf("boundary: unit %d has unknown level %q", u.ID, u.Level)
		}
		parent, ok := byID[u.ParentID]
		if !ok || parent.Level != want {
			return nil, fmt.Errorf("boundary: unit %d (%s) needs a %s parent", u.ID, u.Level, want)
		}
	}
	return &Tree{Version: version, units: byID}, nil
}

// LoadFile reads a tree from a JSON file of the form
// {"version": "...", "units": [{"id":1,"level":"national","name":"Kenya"}, ...]}.
func LoadFile(path string) (*Tree, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("boundary: read %s: %w", path, err)
	}
	var f treeFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("boundary: parse %s: %w", path, err)
	}
	return NewTree(f.Version, f.Units)
}

// ResolveWard returns the ward and its ancestors.
func (t *Tree) ResolveWard(wardID int) (Scope, error) {
	ward, ok := t.units[wardID]
	if !ok || ward.Level != LevelWard {
		return Scope{}, ErrUnknownWard
	}
	constituency := t.units[ward.ParentID]
	county := t.units[constituency.ParentID]
	national := t.units[county.ParentID]
	return Scope{Ward: ward, Constituency: constituency, County: county, National: national}, nil
}
