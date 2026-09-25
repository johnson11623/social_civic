package identity

import "testing"

func TestNormalizeKenyanMobile(t *testing.T) {
	valid := map[string]string{
		"0712345678":        "+254712345678",
		"0712 345 678":      "+254712345678",
		"712345678":         "+254712345678",
		"254712345678":      "+254712345678",
		"+254712345678":     "+254712345678",
		"+254-712-345-678":  "+254712345678",
		"(0712) 345.678":    "+254712345678",
		"0110123456":        "+254110123456", // Safaricom 011x
		"0100 123 456":      "+254100123456", // Airtel 010x
		"  +254 733 000111": "+254733000111",
	}
	for in, want := range valid {
		got, err := NormalizeKenyanMobile(in)
		if err != nil || got != want {
			t.Errorf("NormalizeKenyanMobile(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{
		"", "0712", "07123456789", "020 222 2222", // too short, too long, Nairobi landline
		"+255712345678", // Tanzania
		"0812345678",    // not a mobile prefix
		"07l2345678",    // letter
		"0712345678+",   // plus not leading
		"+1 415 555 0100",
	} {
		if got, err := NormalizeKenyanMobile(in); err == nil {
			t.Errorf("NormalizeKenyanMobile(%q) = %q, want error", in, got)
		}
	}
}
