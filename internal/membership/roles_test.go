package membership

import "testing"

// T-3.1.2.3 — tiered authority.
func TestCanModerate(t *testing.T) {
	const ward, constituency, county = 551, 111, 22
	wardMod := Roles{{Role: RoleWardMod, UnitLevel: 1, UnitCode: ward}}
	countyMod := Roles{{Role: RoleCountyMod, UnitLevel: 3, UnitCode: county}}
	natMod := Roles{{Role: RoleNatMod, UnitLevel: 4, UnitCode: NationalUnit}}
	dpo := Roles{{Role: RoleDPO}}

	type origin struct{ ward, constituency, county int32 }
	home := origin{ward, constituency, county}
	neighbour := origin{552, constituency, county}
	nairobi := origin{1366, 274, 47}

	cases := []struct {
		name  string
		roles Roles
		level int16
		at    origin
		want  bool
	}{
		{"ward mod, own ward post", wardMod, 1, home, true},
		{"ward mod, other ward", wardMod, 1, neighbour, false},
		{"ward mod, post elevated to constituency", wardMod, 2, home, false},
		{"ward mod, national post", wardMod, 4, home, false},
		{"county mod, ward post in county", countyMod, 1, neighbour, true},
		{"county mod, county post", countyMod, 3, home, true},
		{"county mod, other county", countyMod, 1, nairobi, false},
		{"county mod, national post", countyMod, 4, home, false},
		{"national mod, anything", natMod, 4, nairobi, true},
		{"platform role is not moderation", dpo, 1, home, false},
		{"no roles", nil, 1, home, false},
	}
	for _, c := range cases {
		if got := c.roles.CanModerate(c.level, c.at.ward, c.at.constituency, c.at.county); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
	if !(Roles{{Role: RoleSysadmin}}).Has(RoleSysadmin) || wardMod.Has(RoleWardMod) {
		t.Error("Has must match platform roles only")
	}
}
