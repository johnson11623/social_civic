// Package boundary holds Kenya's administrative tree (ward → constituency →
// county → national), loaded from the admin_units table and cached in memory.
//
// Units are identified by (Level, Code) using IEBC codes, which are unique
// only within a level.
package boundary

import (
	"errors"
	"fmt"
	"sort"
)

// Level is an administrative level. Values match groups.level and admin_units.level.
type Level int16

const (
	LevelWard         Level = 1
	LevelConstituency Level = 2
	LevelCounty       Level = 3
	LevelNational     Level = 4
)

func (l Level) String() string {
	switch l {
	case LevelWard:
		return "ward"
	case LevelConstituency:
		return "constituency"
	case LevelCounty:
		return "county"
	case LevelNational:
		return "national"
	}
	return fmt.Sprintf("level(%d)", int(l))
}

// MarshalText renders levels as their names in JSON.
func (l Level) MarshalText() ([]byte, error) { return []byte(l.String()), nil }

// UnmarshalText parses a level name.
func (l *Level) UnmarshalText(b []byte) error {
	v, err := ParseLevel(string(b))
	if err != nil {
		return err
	}
	*l = v
	return nil
}

// ParseLevel parses a level name.
func ParseLevel(s string) (Level, error) {
	for _, l := range []Level{LevelWard, LevelConstituency, LevelCounty, LevelNational} {
		if l.String() == s {
			return l, nil
		}
	}
	return 0, fmt.Errorf("boundary: unknown level %q", s)
}

// NationalCode is the code of the single national unit.
const NationalCode = 1

// ErrUnknownWard is returned when a code is not a known ward.
var ErrUnknownWard = errors.New("boundary: unknown ward")

// Unit is one administrative unit.
type Unit struct {
	Level            Level
	Code             int
	IEBCCode         string // zero-padded official code
	Name             string // IEBC name, uppercase
	DisplayName      string // readable name
	ParentCode       int    // code at Level+1; 0 for national
	RegisteredVoters int    // wards only (2022 register)
}

// Scope is a ward with its ancestors.
type Scope struct {
	Ward         Unit
	Constituency Unit
	County       Unit
	National     Unit
}

type key struct {
	level Level
	code  int
}

// Tree is an immutable, validated administrative tree.
type Tree struct {
	Version  string
	units    map[key]Unit
	children map[key][]Unit
	index    *searchIndex
}

// NewTree validates units and builds the tree and its search index. There must
// be exactly one national unit, and every other unit needs a parent at the
// level directly above it.
func NewTree(version string, units []Unit) (*Tree, error) {
	t := &Tree{Version: version, units: make(map[key]Unit, len(units)), children: make(map[key][]Unit)}
	nationals := 0
	for _, u := range units {
		if u.Level < LevelWard || u.Level > LevelNational {
			return nil, fmt.Errorf("boundary: unit %d has invalid level %d", u.Code, u.Level)
		}
		if u.Code <= 0 || u.Name == "" || u.DisplayName == "" {
			return nil, fmt.Errorf("boundary: %s %d is missing a code or name", u.Level, u.Code)
		}
		k := key{u.Level, u.Code}
		if _, dup := t.units[k]; dup {
			return nil, fmt.Errorf("boundary: duplicate %s %d", u.Level, u.Code)
		}
		if u.Level == LevelNational {
			nationals++
		}
		t.units[k] = u
	}
	if nationals != 1 {
		return nil, fmt.Errorf("boundary: need exactly one national unit, got %d", nationals)
	}
	for _, u := range units {
		if u.Level == LevelNational {
			continue
		}
		pk := key{u.Level + 1, u.ParentCode}
		if _, ok := t.units[pk]; !ok {
			return nil, fmt.Errorf("boundary: %s %d has no %s parent %d", u.Level, u.Code, u.Level+1, u.ParentCode)
		}
		t.children[pk] = append(t.children[pk], u)
	}
	for _, kids := range t.children {
		sort.Slice(kids, func(i, j int) bool { return kids[i].DisplayName < kids[j].DisplayName })
	}
	t.index = newSearchIndex(t)
	return t, nil
}

// Len returns the number of units, including the national unit.
func (t *Tree) Len() int { return len(t.units) }

// Count returns the number of units at a level.
func (t *Tree) Count(level Level) int {
	n := 0
	for k := range t.units {
		if k.level == level {
			n++
		}
	}
	return n
}

// Unit returns the unit at (level, code).
func (t *Tree) Unit(level Level, code int) (Unit, bool) {
	u, ok := t.units[key{level, code}]
	return u, ok
}

// Children returns a unit's direct children sorted by display name.
func (t *Tree) Children(level Level, code int) []Unit {
	return t.children[key{level, code}]
}

// Parent returns a unit's parent; ok is false for the national unit.
func (t *Tree) Parent(u Unit) (Unit, bool) {
	if u.Level == LevelNational {
		return Unit{}, false
	}
	return t.Unit(u.Level+1, u.ParentCode)
}

// Ancestors returns the unit's ancestors from nearest to county (national excluded).
func (t *Tree) Ancestors(u Unit) []Unit {
	var out []Unit
	for p, ok := t.Parent(u); ok && p.Level != LevelNational; p, ok = t.Parent(p) {
		out = append(out, p)
	}
	return out
}

// ResolveWard returns the ward and its ancestors.
func (t *Tree) ResolveWard(code int) (Scope, error) {
	ward, ok := t.Unit(LevelWard, code)
	if !ok {
		return Scope{}, ErrUnknownWard
	}
	constituency, _ := t.Parent(ward)
	county, _ := t.Parent(constituency)
	national, _ := t.Parent(county)
	return Scope{Ward: ward, Constituency: constituency, County: county, National: national}, nil
}
