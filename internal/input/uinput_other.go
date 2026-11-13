//go:build !linux

package input

import (
	"errors"
	"os"
)

// The virtual mouse and the remote watcher use Linux kernel interfaces. On
// other systems (development machines) they are simply unavailable.

const eventSize = 24

var errUnsupported = errors.New("the phone touchpad needs Linux")

type virtualDevice struct{}

func uinputProblem() error                           { return errUnsupported }
func openVirtualDevice() (*virtualDevice, error)     { return nil, errUnsupported }
func (d *virtualDevice) move(dx, dy int) error       { return errUnsupported }
func (d *virtualDevice) scroll(dx, dy int) error     { return errUnsupported }
func (d *virtualDevice) key(uint16, bool) error      { return errUnsupported }
func (d *virtualDevice) close()                      {}
func openRemote(string, []uint16) (*os.File, error)  { return nil, errNotARemote }
func padAxes(*os.File) (map[uint16][2]int32, bool)   { return nil, false }
func deviceLabel(*os.File) string                    { return "" }
func decodeEvent([]byte) (typ, code uint16, v int32) { return 0, 0, 0 }
