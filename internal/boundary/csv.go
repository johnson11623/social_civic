package boundary

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
)

// IEBC2022 is the source version of db/seed/iebc_2022_wards.csv.
const IEBC2022 = "iebc-2022"

// ExpectedIEBC2022 is the number of units per level in the 2012 delimitation.
var ExpectedIEBC2022 = map[Level]int{
	LevelNational:     1,
	LevelCounty:       47,
	LevelConstituency: 290,
	LevelWard:         1450,
}

var csvHeader = []string{
	"county_code", "county_name", "county_display",
	"constituency_code", "constituency_name", "constituency_display",
	"ward_code", "ward_name", "ward_display", "registered_voters_2022",
}

// NationalUnit is the root of the tree.
var NationalUnit = Unit{Level: LevelNational, Code: NationalCode, IEBCCode: "0", Name: "NATIONAL", DisplayName: "National"}

// LoadIEBCCSVFile reads the seed CSV (see db/seed/README.md).
func LoadIEBCCSVFile(path string) ([]Unit, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("boundary: open %s: %w", path, err)
	}
	defer f.Close()
	return ParseIEBCCSV(f)
}

// ParseIEBCCSV turns one-row-per-ward CSV into units for every level, checking
// that each county and constituency code always carries the same name and parent.
func ParseIEBCCSV(r io.Reader) ([]Unit, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = len(csvHeader)
	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("boundary: read header: %w", err)
	}
	for i, h := range csvHeader {
		if header[i] != h {
			return nil, fmt.Errorf("boundary: column %d is %q, want %q", i+1, header[i], h)
		}
	}

	units := []Unit{NationalUnit}
	seen := map[key]Unit{}
	add := func(u Unit) error {
		k := key{u.Level, u.Code}
		if prev, ok := seen[k]; ok {
			if prev != u {
				return fmt.Errorf("boundary: %s %s has conflicting rows (%q under %d vs %q under %d)",
					u.Level, u.IEBCCode, prev.Name, prev.ParentCode, u.Name, u.ParentCode)
			}
			return nil
		}
		seen[k] = u
		units = append(units, u)
		return nil
	}

	for line := 2; ; line++ {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("boundary: line %d: %w", line, err)
		}
		county, err1 := parseCode(rec[0])
		constituency, err2 := parseCode(rec[3])
		ward, err3 := parseCode(rec[6])
		voters, err4 := strconv.Atoi(rec[9])
		if err := errors.Join(err1, err2, err3, err4); err != nil {
			return nil, fmt.Errorf("boundary: line %d: %w", line, err)
		}
		if err := errors.Join(
			add(Unit{Level: LevelCounty, Code: county, IEBCCode: rec[0], Name: rec[1], DisplayName: rec[2], ParentCode: NationalCode}),
			add(Unit{Level: LevelConstituency, Code: constituency, IEBCCode: rec[3], Name: rec[4], DisplayName: rec[5], ParentCode: county}),
			add(Unit{Level: LevelWard, Code: ward, IEBCCode: rec[6], Name: rec[7], DisplayName: rec[8], ParentCode: constituency, RegisteredVoters: voters}),
		); err != nil {
			return nil, fmt.Errorf("boundary: line %d: %w", line, err)
		}
	}
	return units, nil
}

func parseCode(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid code %q", s)
	}
	return n, nil
}

// CheckCounts verifies the number of units per level.
func CheckCounts(units []Unit, want map[Level]int) error {
	got := map[Level]int{}
	for _, u := range units {
		got[u.Level]++
	}
	var errs []error
	for _, l := range []Level{LevelNational, LevelCounty, LevelConstituency, LevelWard} {
		if got[l] != want[l] {
			errs = append(errs, fmt.Errorf("%s: got %d, want %d", l, got[l], want[l]))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("boundary: unexpected unit counts: %w", errors.Join(errs...))
	}
	return nil
}
