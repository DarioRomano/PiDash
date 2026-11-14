package sound

// Follower moves the sound to Bluetooth headphones or a speaker when they
// connect, and back to where it was when they are gone again. That is what
// putting headphones on and taking them off should do, without a trip into
// the settings each time.
//
// It only reacts to an output appearing or disappearing. Whatever is chosen
// by hand, on the settings page or with the Sound tile, stays as chosen.
type Follower struct {
	started bool
	// known are the outputs seen at the last look.
	known map[string]bool
	// active is the output that was in use at the last look.
	active string
	// home is the output to return to: the last one in use that is not
	// Bluetooth, usually the TV.
	home string
}

// Step looks at the outputs as they are now and returns the one the sound
// should move to, or "" to leave it where it is.
func (f *Follower) Step(st Status) string {
	if !st.Available {
		return ""
	}
	now := map[string]bool{}
	byID := map[string]Output{}
	active := ""
	for _, o := range st.Outputs {
		now[o.ID], byID[o.ID] = true, o
		if o.Active {
			active = o.ID
		}
	}
	defer func() { f.known, f.active, f.started = now, active, true }()

	if !f.started {
		// What is there when PiDash starts was not just switched on.
		if o, ok := byID[active]; ok && o.Kind != "bluetooth" {
			f.home = active
		}
		return ""
	}

	// The output that was in use is gone, which for the TV's own output does
	// not happen: headphones were switched off. The sound goes back to where
	// it was before them, rather than wherever the sound service has fallen,
	// which on a Pi can be the HDMI socket with nothing in it.
	if was := f.active; was != "" && !now[was] {
		if f.home != "" && now[f.home] && f.home != active && byID[f.home].Usable() {
			return f.home
		}
		return ""
	}

	// Whatever is in use and is not Bluetooth is where to return to later:
	// the TV, or another output someone chose by hand.
	if o, ok := byID[active]; ok && o.Kind != "bluetooth" {
		f.home = active
	}

	// Headphones or a speaker connected: the sound goes there. The sound
	// service may have moved it already; then there is nothing to do.
	for _, o := range st.Outputs {
		if o.Kind == "bluetooth" && !f.known[o.ID] {
			if o.ID == active {
				return ""
			}
			return o.ID
		}
	}
	return ""
}
