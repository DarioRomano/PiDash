package system

import "testing"

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
