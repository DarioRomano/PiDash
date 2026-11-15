package wifi

import (
	"strings"
	"testing"
)

func TestTextForPhones(t *testing.T) {
	for _, c := range []struct {
		name, password string
		hidden         bool
		want           string
	}{
		{"Guests", "welcome123", false, "WIFI:T:WPA;S:Guests;P:welcome123;;"},
		{"Open Cafe", "", false, "WIFI:T:nopass;S:Open Cafe;;"},
		{"Hidden", "secretsecret", true, "WIFI:T:WPA;S:Hidden;P:secretsecret;H:true;;"},
		// The characters the form itself uses are marked, so a password with
		// a semicolon does not end early.
		{`Müller's; "Net"`, `p:a;s,s\w"rd`, false, `WIFI:T:WPA;S:Müller's\; \"Net\";P:p\:a\;s\,s\\w\"rd;;`},
	} {
		if got := Text(c.name, c.password, c.hidden); got != c.want {
			t.Errorf("Text(%q, %q, %v) =\n  %s\nwant\n  %s", c.name, c.password, c.hidden, got, c.want)
		}
	}
}

func TestCodeIsASquareWithItsThreeCorners(t *testing.T) {
	rows, err := Code("Guests", "welcome123", false)
	if err != nil {
		t.Fatal(err)
	}
	n := len(rows)
	if n < 21 || (n-17)%4 != 0 {
		t.Fatalf("a code has 21, 25, 29, … rows; got %d", n)
	}
	for i, r := range rows {
		if len(r) != n || strings.Trim(r, "01") != "" {
			t.Fatalf("row %d is %q", i, r)
		}
	}
	// The three squares in the corners by which a camera finds the code: a
	// dark frame of seven, light inside, dark in the middle.
	finder := []string{"1111111", "1000001", "1011101", "1011101", "1011101", "1000001", "1111111"}
	for _, corner := range [][2]int{{0, 0}, {n - 7, 0}, {0, n - 7}} {
		for y, want := range finder {
			if got := rows[corner[1]+y][corner[0] : corner[0]+7]; got != want {
				t.Errorf("corner at %v, row %d = %s, want %s", corner, y, got, want)
			}
		}
	}
	// A longer text needs a larger code.
	long, err := Code(strings.Repeat("n", 32), strings.Repeat("p", 63), true)
	if err != nil || len(long) <= n {
		t.Errorf("the longest name and password: %d rows, err %v", len(long), err)
	}
}
