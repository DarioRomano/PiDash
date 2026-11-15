package screen

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// What wlr-randr prints for a 4K TV, as on a Pi 5, and what it prints after
// the TV was set to 1080p.
const tv4K = `HDMI-A-1 "LG Electronics LG TV SSCR2 0x01010101 (HDMI-A-1)"
  Make: LG Electronics
  Model: LG TV SSCR2
  Serial: 0x01010101
  Physical size: 1600x900 mm
  Enabled: yes
  Modes:
    3840x2160 px, 60.000000 Hz (preferred, current)
    3840x2160 px, 50.000000 Hz
    3840x2160 px, 30.000000 Hz
    1920x1080 px, 120.000000 Hz
    1920x1080 px, 60.000000 Hz
    1920x1080 px, 59.940201 Hz
    1920x1080 px, 50.000000 Hz
    1280x720 px, 60.000000 Hz
  Position: 0,0
  Transform: normal
  Scale: 1.000000
  Adaptive Sync: disabled
HDMI-A-2 "(null) (null) (HDMI-A-2)"
  Enabled: no
  Modes:
    1920x1080 px, 60.000000 Hz (preferred)
`

func fake(t *testing.T, screens *string, fail string) (*Control, *[]string) {
	t.Helper()
	var calls []string
	c := &Control{StatePath: filepath.Join(t.TempDir(), "run", "screen.json"), Run: func(_ context.Context, args ...string) ([]byte, error) {
		line := strings.Join(args, " ")
		calls = append(calls, line)
		if len(args) == 0 {
			return []byte(*screens), nil
		}
		if fail != "" && strings.Contains(line, fail) {
			return []byte("failed to apply configuration"), errors.New("exit status 1")
		}
		// The TV does as it is told.
		for _, size := range []string{"3840x2160", "1920x1080"} {
			if strings.Contains(line, "--mode "+size) {
				*screens = strings.Replace(strings.Replace(*screens, " (preferred, current)", " (preferred)", 1), " (current)", "", 1)
				*screens = strings.Replace(*screens, "    "+size+" px, 60.000000 Hz", "    "+size+" px, 60.000000 Hz (current)", 1)
				*screens = strings.Replace(*screens, "Hz (current) (preferred)", "Hz (preferred, current)", 1)
			}
		}
		return nil, nil
	}}
	return c, &calls
}

func TestReadingTheScreens(t *testing.T) {
	outputs := Parse(tv4K + "HEADLESS-1 \"Headless output 1\"\n  Enabled: yes\n  Modes:\n    1280x720 px (current)\n")
	if len(outputs) != 3 || outputs[0].Name != "HDMI-A-1" || !outputs[0].Enabled || outputs[1].Enabled || len(outputs[0].Modes) != 8 {
		t.Fatalf("outputs = %+v", outputs)
	}
	now, ok := outputs[0].Current()
	if !ok || now.Width != 3840 || now.Hz != 60 || now.arg() != "3840x2160@60.000000Hz" {
		t.Errorf("current = %+v", now)
	}
	if now, ok := outputs[2].Current(); !ok || now.arg() != "1280x720" {
		t.Errorf("a screen that states no rate: %+v", now)
	}
	if _, ok := outputs[1].Current(); ok {
		t.Error("a screen that is off has no current mode")
	}
	if got := Parse("nonsense\n\n   more nonsense\n"); len(got) != 1 || len(got[0].Modes) != 0 {
		t.Errorf("nonsense = %+v", got)
	}
}

func TestTheTVGets1080pWhileTheDashboardRunsAndItsOwnSizeAfter(t *testing.T) {
	screens := tv4K
	c, calls := fake(t, &screens, "")
	ctx := context.Background()

	changed, err := c.Lower(ctx)
	if err != nil || len(changed) != 1 || !strings.Contains(changed[0], "1920x1080 instead of 3840x2160") {
		t.Fatalf("changed = %v, err = %v", changed, err)
	}
	// At 60 a second like before, not 120 and not 59.94; the socket with no TV is left alone.
	if want := "--output HDMI-A-1 --mode 1920x1080@60.000000Hz"; (*calls)[1] != want || len(*calls) != 2 {
		t.Errorf("calls = %q, want the second to be %q", *calls, want)
	}
	// Asked again (the backend starts anew): nothing to do, and the size to go back to is not forgotten.
	if changed, err := c.Lower(ctx); err != nil || len(changed) != 0 {
		t.Errorf("a second time: %v, %v", changed, err)
	}
	restored, err := c.Restore(ctx)
	if err != nil || len(restored) != 1 || restored[0] != "HDMI-A-1: 3840x2160@60.000000Hz" {
		t.Fatalf("restored = %v, err = %v", restored, err)
	}
	if now, _ := Parse(screens)[0].Current(); now.Width != 3840 {
		t.Errorf("the TV is at %+v after restoring", now)
	}
	// Nothing was lowered: nothing is restored.
	n := len(*calls)
	if restored, err := c.Restore(ctx); err != nil || len(restored) != 0 || len(*calls) != n {
		t.Errorf("restoring twice: %v, %v", restored, err)
	}
}

func TestScreensThatNeedNothing(t *testing.T) {
	ctx := context.Background()
	for name, screens := range map[string]string{
		"already 1080p":       strings.Replace(strings.Replace(tv4K, " (preferred, current)", " (preferred)", 1), "1920x1080 px, 60.000000 Hz", "1920x1080 px, 60.000000 Hz (current)", 1),
		"no 1080p on offer":   "HDMI-A-1 \"x\"\n  Enabled: yes\n  Modes:\n    3840x2160 px, 60.000000 Hz (current)\n    2560x1440 px, 60.000000 Hz\n",
		"only 1080p too fast": "HDMI-A-1 \"x\"\n  Enabled: yes\n  Modes:\n    3840x2160 px, 60.000000 Hz (current)\n    1920x1080 px, 120.000000 Hz\n",
		"no screen at all":    "",
	} {
		s := screens
		c, calls := fake(t, &s, "")
		if changed, err := c.Lower(ctx); err != nil || len(changed) != 0 || len(*calls) != 1 {
			t.Errorf("%s: changed %v, err %v, calls %q", name, changed, err, *calls)
		}
	}
	// A TV at 4K and 30 a second gets 1080p at 60: there is no 30 on offer, and 60 is better.
	slow := strings.Replace(strings.Replace(tv4K, " (preferred, current)", " (preferred)", 1), "3840x2160 px, 30.000000 Hz", "3840x2160 px, 30.000000 Hz (current)", 1)
	c, calls := fake(t, &slow, "")
	if _, err := c.Lower(ctx); err != nil || (*calls)[1] != "--output HDMI-A-1 --mode 1920x1080@60.000000Hz" {
		t.Errorf("from 4K at 30: %q, %v", *calls, err)
	}
	// The desktop refuses: said, and nothing is remembered that was not done.
	screens := tv4K
	c, _ = fake(t, &screens, "--mode 1920x1080")
	if _, err := c.Lower(ctx); err == nil || !strings.Contains(err.Error(), "failed to apply") {
		t.Errorf("a refusal: %v", err)
	}
	if restored, _ := c.Restore(ctx); len(restored) != 0 {
		t.Errorf("restored %v after nothing was changed", restored)
	}
}
