// Package wifi makes the code a phone scans to join a Wi-Fi network.
package wifi

import (
	"strings"

	"rsc.io/qr"
)

// Text returns what the code says: the form phones understand for joining a
// network, "WIFI:T:WPA;S:name;P:password;;". In name and password the
// characters with a meaning in that form are marked with a backslash.
func Text(name, password string, hidden bool) string {
	esc := strings.NewReplacer(`\`, `\\`, `;`, `\;`, `,`, `\,`, `:`, `\:`, `"`, `\"`).Replace
	var b strings.Builder
	b.WriteString("WIFI:")
	if password == "" {
		b.WriteString("T:nopass;")
	} else {
		b.WriteString("T:WPA;")
	}
	b.WriteString("S:" + esc(name) + ";")
	if password != "" {
		b.WriteString("P:" + esc(password) + ";")
	}
	if hidden {
		b.WriteString("H:true;")
	}
	b.WriteString(";")
	return b.String()
}

// Code returns the code for a network as rows of '1' (dark) and '0' (light).
// It can lose up to a sixth of itself, to glare on the TV for example, and
// still be read.
func Code(name, password string, hidden bool) ([]string, error) {
	code, err := qr.Encode(Text(name, password, hidden), qr.M)
	if err != nil {
		return nil, err
	}
	rows := make([]string, code.Size)
	for y := 0; y < code.Size; y++ {
		row := make([]byte, code.Size)
		for x := 0; x < code.Size; x++ {
			row[x] = '0'
			if code.Black(x, y) {
				row[x] = '1'
			}
		}
		rows[y] = string(row)
	}
	return rows, nil
}
