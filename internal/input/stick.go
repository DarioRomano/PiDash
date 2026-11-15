package input

// stick turns the direction pad and the left stick of a game controller into
// presses of the arrow keys, so a controller moves through the dashboard like
// a remote.
//
// A direction pad reports -1, 0 or 1 per axis. A stick reports a position in
// a range that differs from pad to pad; it counts as pushed beyond 60% of the
// way and as let go below 35%, so a stick resting slightly off centre, or one
// held at the edge of "pushed", does not flicker between the two.
type stick struct {
	// ranges holds the lowest and highest value of each axis that is used.
	ranges map[uint16][2]int32
	// held is the arrow key each axis currently presses; 0 for none.
	held map[uint16]uint16
}

// newStick returns nil for a device that is no game controller.
func newStick(ranges map[uint16][2]int32) *stick {
	if len(ranges) == 0 {
		return nil
	}
	return &stick{ranges: ranges, held: map[uint16]uint16{}}
}

// move takes a new position of one axis and returns the arrow key to release
// and the one to press; either may be 0.
func (s *stick) move(axis uint16, value int32) (release, press uint16) {
	if s == nil {
		return 0, 0
	}
	r, ok := s.ranges[axis]
	if !ok || r[1] <= r[0] {
		return 0, 0
	}
	// -1 … 1 across the axis.
	pos := (2*float64(value) - float64(r[0]) - float64(r[1])) / float64(r[1]-r[0])
	was := s.held[axis]
	dir := 0
	switch {
	case pos >= 0.6:
		dir = 1
	case pos <= -0.6:
		dir = -1
	case pos > -0.35 && pos < 0.35:
		dir = 0
	default:
		return 0, 0 // between "let go" and "pushed": stay as it was
	}
	var key uint16
	switch {
	case dir == 0:
	case axis == absX || axis == absHat0X:
		key = map[int]uint16{-1: keyLeft, 1: keyRight}[dir]
	default:
		key = map[int]uint16{-1: keyUp, 1: keyDown}[dir]
	}
	if key == was {
		return 0, 0
	}
	s.held[axis] = key
	return was, key
}
