package system

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestHotHasAGapSoEffectsDoNotFlicker(t *testing.T) {
	p := New(0)
	p.CoolAbove = 75
	for i, step := range []struct {
		temp float64
		hot  bool
	}{
		{60, false},
		{74.9, false},
		{75, true},   // reached the limit
		{72, true},   // a little cooler is not cool yet
		{68.1, true}, // …
		{68, false},  // seven degrees below: cool again
		{74, false},  // and it stays cool until the limit is reached again
		{81, true},
	} {
		if got := p.judge(step.temp); got != step.hot {
			t.Errorf("step %d: at %.1f° hot = %v, want %v", i, step.temp, got, step.hot)
		}
	}
}

func TestCoolingSwitchedOffNeverCallsThePiHot(t *testing.T) {
	p := New(0)
	if p.judge(95) {
		t.Error("without a limit the Pi must never count as hot")
	}
	// Switched off while hot: not hot any more.
	p.CoolAbove = 70
	p.judge(80)
	p.CoolAbove = 0
	if p.judge(80) {
		t.Error("still hot after cooling was switched off")
	}
}

func TestTheProcessorsTemperatureOnDifferentMachines(t *testing.T) {
	zones := func(sensors ...[2]string) string {
		dir := t.TempDir()
		for i, s := range sensors {
			z := filepath.Join(dir, "thermal_zone"+strconv.Itoa(i))
			if err := os.MkdirAll(z, 0o755); err != nil {
				t.Fatal(err)
			}
			_ = os.WriteFile(filepath.Join(z, "type"), []byte(s[0]+"\n"), 0o644)
			_ = os.WriteFile(filepath.Join(z, "temp"), []byte(s[1]+"\n"), 0o644)
		}
		return dir
	}
	for name, c := range map[string]struct {
		dir  string
		want float64
		ok   bool
	}{
		"a Raspberry Pi":                {zones([2]string{"cpu-thermal", "48312"}), 48.312, true},
		"a PC: not the Wi-Fi's sensor":  {zones([2]string{"iwlwifi_1", "39000"}, [2]string{"acpitz", "27800"}, [2]string{"x86_pkg_temp", "52000"}), 52, true},
		"a board with several":          {zones([2]string{"gpu-thermal", "41000"}, [2]string{"soc-thermal", "44500"}), 44.5, true},
		"a PC with only the board's":    {zones([2]string{"acpitz", "31000"}), 31, true},
		"only sensors of other things":  {zones([2]string{"iwlwifi_1", "39000"}, [2]string{"BAT0", "30000"}), 0, false},
		"a sensor that is not there":    {zones([2]string{"cpu-thermal", "-274000"}), 0, false},
		"no sensors at all (a machine)": {t.TempDir(), 0, false},
	} {
		got, ok := cpuTemp(c.dir)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: %v %v, want %v %v", name, got, ok, c.want, c.ok)
		}
	}
}

// How busy the processor was is told by its counters: the time all cores
// spent busy against the time that went by, between two readings.
func TestHowBusyTheProcessorWas(t *testing.T) {
	// user nice system idle iowait irq softirq steal, then four cores.
	first := "cpu  100 0 50 800 50 0 0 0 0 0\ncpu0 1 0 0 0 0 0 0 0\ncpu1 1 0 0 0 0 0 0 0\ncpu2 1 0 0 0 0 0 0 0\ncpu3 1 0 0 0 0 0 0 0\nintr 5\n"
	busy, total, cores := cpuTimes(first)
	if busy != 150 || total != 1000 || cores != 4 {
		t.Fatalf("busy %d, total %d, cores %d", busy, total, cores)
	}
	p := New(0)
	if pct := p.busySince(busy, total); pct != nil {
		t.Fatalf("the first reading has nothing to compare with: %v", *pct)
	}
	// 400 ticks later, 100 of them busy and 50 waiting for the disk: 25 %.
	busy2, total2, _ := cpuTimes("cpu  180 0 70 1050 100 0 0 0 0 0\n")
	if pct := p.busySince(busy2, total2); pct == nil || *pct != 25 {
		t.Fatalf("want 25 %%, got %v", pct)
	}
	// Counters that went backwards (a restart): nothing is said.
	if pct := p.busySince(10, 20); pct != nil {
		t.Fatalf("after counters went back: %v", *pct)
	}
}
