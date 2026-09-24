package boundary

import (
	"errors"
	"testing"
)

func sampleUnits() []Unit {
	return []Unit{
		{ID: 1, Level: LevelNational, Name: "National"},
		{ID: 12, Level: LevelCounty, Name: "Kiambu", ParentID: 1},
		{ID: 145, Level: LevelConstituency, Name: "Gatundu South", ParentID: 12},
		{ID: 1203, Level: LevelWard, Name: "Kiamwangi", ParentID: 145},
	}
}

func TestResolveWard(t *testing.T) {
	tree, err := NewTree("t", sampleUnits())
	if err != nil {
		t.Fatal(err)
	}
	s, err := tree.ResolveWard(1203)
	if err != nil {
		t.Fatal(err)
	}
	if s.Ward.ID != 1203 || s.Constituency.ID != 145 || s.County.ID != 12 || s.National.ID != 1 {
		t.Errorf("scope = %+v", s)
	}
	for _, id := range []int{0, 9999, 145, 12, 1} {
		if _, err := tree.ResolveWard(id); !errors.Is(err, ErrUnknownWard) {
			t.Errorf("ResolveWard(%d) err = %v, want ErrUnknownWard", id, err)
		}
	}
}

func TestNewTreeRejectsInvalidTrees(t *testing.T) {
	cases := map[string][]Unit{
		"duplicate id":   append(sampleUnits(), Unit{ID: 12, Level: LevelCounty, Name: "Dup", ParentID: 1}),
		"missing parent": append(sampleUnits(), Unit{ID: 2000, Level: LevelWard, Name: "Orphan", ParentID: 999}),
		"skips a level":  append(sampleUnits(), Unit{ID: 2001, Level: LevelWard, Name: "Wrong", ParentID: 12}),
		"unknown level":  append(sampleUnits(), Unit{ID: 2002, Level: "village", Name: "X", ParentID: 1203}),
	}
	for name, units := range cases {
		if _, err := NewTree("t", units); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestLoadDevFixture(t *testing.T) {
	tree, err := LoadFile("../../db/seed/boundary_dev_fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tree.ResolveWard(1203); err != nil {
		t.Errorf("fixture ward 1203: %v", err)
	}
}
