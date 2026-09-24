package identity

import (
	"errors"
	"strings"
)

// ErrInvalidPhone is returned for numbers that are not Kenyan mobile numbers.
var ErrInvalidPhone = errors.New("identity: not a Kenyan mobile number")

// NormalizeKenyanMobile returns the E.164 form (+2547XXXXXXXX or +2541XXXXXXXX)
// of a Kenyan mobile number written the ways people usually write it:
// 0712 345 678, 712345678, 254712345678, +254-712-345-678, 0110 123 456.
//
// Diaspora numbers and alternative verification come with US-08.
func NormalizeKenyanMobile(s string) (string, error) {
	var b strings.Builder
	for i, r := range strings.TrimSpace(s) {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' && i == 0:
		case r == ' ' || r == '-' || r == '(' || r == ')' || r == '.':
		default:
			return "", ErrInvalidPhone
		}
	}
	d := b.String()
	switch {
	case len(d) == 12 && strings.HasPrefix(d, "254"):
		d = d[3:]
	case len(d) == 10 && d[0] == '0':
		d = d[1:]
	case len(d) == 9:
	default:
		return "", ErrInvalidPhone
	}
	if d[0] != '7' && d[0] != '1' {
		return "", ErrInvalidPhone
	}
	return "+254" + d, nil
}
