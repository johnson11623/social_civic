package boundary

import (
	"errors"
	"strings"
	"testing"
)

const seedCSV = "../../db/seed/iebc_2022_wards.csv"

func realTree(t testing.TB) *Tree {
	t.Helper()
	units, err := LoadIEBCCSVFile(seedCSV)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := NewTree(IEBC2022, units)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func TestSeedCSVMatchesIEBC2022(t *testing.T) {
	units, err := LoadIEBCCSVFile(seedCSV)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckCounts(units, ExpectedIEBC2022); err != nil {
		t.Fatal(err)
	}
	voters := 0
	for _, u := range units {
		voters += u.RegisteredVoters
	}
	if voters != 22_102_532 {
		t.Errorf("registered voters = %d, want 22,102,532 (IEBC 2022 total)", voters)
	}
	tree := realTree(t)
	if tree.Len() != 1788 {
		t.Errorf("tree has %d units, want 1,788", tree.Len())
	}
}

func TestResolveWard(t *testing.T) {
	tree := realTree(t)
	s, err := tree.ResolveWard(17)
	if err != nil {
		t.Fatal(err)
	}
	if s.Ward.DisplayName != "Ziwa la Ng'ombe" || s.Constituency.DisplayName != "Nyali" ||
		s.County.DisplayName != "Mombasa" || s.National.Code != NationalCode {
		t.Errorf("scope = %+v", s)
	}
	if s.Ward.IEBCCode != "0017" || s.Constituency.IEBCCode != "004" || s.County.IEBCCode != "001" {
		t.Errorf("IEBC codes = %s/%s/%s", s.Ward.IEBCCode, s.Constituency.IEBCCode, s.County.IEBCCode)
	}
	for _, code := range []int{0, -1, 1451, 99999} {
		if _, err := tree.ResolveWard(code); !errors.Is(err, ErrUnknownWard) {
			t.Errorf("ResolveWard(%d) err = %v", code, err)
		}
	}
}

func TestChildrenAndAncestors(t *testing.T) {
	tree := realTree(t)
	if n := len(tree.Children(LevelNational, NationalCode)); n != 47 {
		t.Errorf("national has %d counties", n)
	}
	nairobi := tree.Children(LevelCounty, 47)
	if len(nairobi) != 17 {
		t.Errorf("Nairobi City has %d constituencies, want 17", len(nairobi))
	}
	ward, _ := tree.Unit(LevelWard, 1450)
	anc := tree.Ancestors(ward)
	if len(anc) != 2 || anc[0].DisplayName != "Mathare" || anc[1].DisplayName != "Nairobi City" {
		t.Errorf("ancestors of 1450 = %+v", anc)
	}
}

func TestNewTreeRejectsInvalidTrees(t *testing.T) {
	base := func() []Unit {
		return []Unit{
			NationalUnit,
			{Level: LevelCounty, Code: 1, IEBCCode: "001", Name: "MOMBASA", DisplayName: "Mombasa", ParentCode: 1},
			{Level: LevelConstituency, Code: 1, IEBCCode: "001", Name: "CHANGAMWE", DisplayName: "Changamwe", ParentCode: 1},
			{Level: LevelWard, Code: 1, IEBCCode: "0001", Name: "PORT REITZ", DisplayName: "Port Reitz", ParentCode: 1},
		}
	}
	if _, err := NewTree("t", base()); err != nil {
		t.Fatalf("valid tree rejected: %v", err)
	}
	cases := map[string]func([]Unit) []Unit{
		"duplicate":     func(u []Unit) []Unit { return append(u, u[3]) },
		"orphan ward":   func(u []Unit) []Unit { u[3].ParentCode = 99; return u },
		"no national":   func(u []Unit) []Unit { return u[1:] },
		"two nationals": func(u []Unit) []Unit { n := NationalUnit; n.Code = 2; return append(u, n) },
		"bad level":     func(u []Unit) []Unit { u[3].Level = 9; return u },
		"missing name":  func(u []Unit) []Unit { u[3].DisplayName = ""; return u },
		"non-positive":  func(u []Unit) []Unit { u[3].Code = 0; return u },
	}
	for name, mutate := range cases {
		if _, err := NewTree("t", mutate(base())); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestParseIEBCCSVRejectsBadInput(t *testing.T) {
	header := strings.Join(csvHeader, ",") + "\n"
	cases := map[string]string{
		"wrong header": "a,b,c,d,e,f,g,h,i,j\n",
		"bad code":     header + "x01,MOMBASA,Mombasa,001,CHANGAMWE,Changamwe,0001,PORT REITZ,Port Reitz,1\n",
		"bad voters":   header + "001,MOMBASA,Mombasa,001,CHANGAMWE,Changamwe,0001,PORT REITZ,Port Reitz,many\n",
		"short row":    header + "001,MOMBASA\n",
		"constituency in two counties": header +
			"001,MOMBASA,Mombasa,001,CHANGAMWE,Changamwe,0001,PORT REITZ,Port Reitz,1\n" +
			"002,KWALE,Kwale,001,CHANGAMWE,Changamwe,0002,KIPEVU,Kipevu,1\n",
	}
	for name, in := range cases {
		if _, err := ParseIEBCCSV(strings.NewReader(in)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
