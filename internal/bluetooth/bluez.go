package bluetooth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	bluezService = "org.bluez"
	adapterIface = "org.bluez.Adapter1"
	deviceIface  = "org.bluez.Device1"
	propsIface   = "org.freedesktop.DBus.Properties"
	// agentPath is where PiDash answers BlueZ's questions during pairing.
	agentPath = dbus.ObjectPath("/org/pidash/agent")
	// searchFor is how long a search for devices runs before it stops by
	// itself: searching makes the Pi's Wi-Fi and its other Bluetooth
	// connections a little worse while it lasts.
	searchFor = time.Minute
)

// BlueZ manages Bluetooth through the BlueZ service on the system bus.
type BlueZ struct {
	Log *slog.Logger

	mu     sync.Mutex
	conn   *dbus.Conn
	search *time.Timer // stops the running search
}

// New returns a manager for the Pi's real Bluetooth. Nothing is contacted
// until it is first used.
func New(log *slog.Logger) *BlueZ { return &BlueZ{Log: log} }

// Close ends a running search and the connection to BlueZ.
func (b *BlueZ) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.search != nil {
		b.search.Stop()
	}
	if b.conn != nil {
		b.conn.Close()
		b.conn = nil
	}
}

// bus returns the connection to the system bus, making it if needed. One
// connection is kept: a search belongs to the connection that started it and
// ends with it, and so does the agent that answers pairing questions.
func (b *BlueZ) bus() (*dbus.Conn, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.conn != nil && b.conn.Connected() {
		return b.conn, nil
	}
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, err
	}
	// Pairing a controller or a speaker asks nothing of the person. Without
	// an agent of its own PiDash would leave the questions to the desktop,
	// which answers them with a window nobody can see behind the dashboard.
	if err := conn.Export(agent{log: b.Log}, agentPath, "org.bluez.Agent1"); err == nil {
		call := conn.Object(bluezService, "/org/bluez").Call("org.bluez.AgentManager1.RegisterAgent", 0, agentPath, "NoInputNoOutput")
		if call.Err != nil {
			b.Log.Debug("bluetooth: no pairing agent", "err", call.Err)
		}
	}
	b.conn = conn
	return conn, nil
}

// object is one thing BlueZ knows: an adapter or a device, with its values.
type object map[string]dbus.Variant

func (o object) str(name string) string {
	s, _ := o[name].Value().(string)
	return s
}

func (o object) flag(name string) bool {
	v, _ := o[name].Value().(bool)
	return v
}

// world is everything BlueZ knows right now.
type world struct {
	adapter     dbus.ObjectPath
	adapterVals object
	devices     map[dbus.ObjectPath]object
}

func (b *BlueZ) look(ctx context.Context) (*dbus.Conn, world, error) {
	var w world
	conn, err := b.bus()
	if err != nil {
		return nil, w, err
	}
	var all map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	if err := conn.Object(bluezService, "/").CallWithContext(ctx, "org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&all); err != nil {
		return nil, w, err
	}
	// The first adapter, by name: hci0 on a Pi.
	var adapters []string
	for path, ifaces := range all {
		if _, ok := ifaces[adapterIface]; ok {
			adapters = append(adapters, string(path))
		}
	}
	if len(adapters) == 0 {
		return conn, w, nil
	}
	sort.Strings(adapters)
	w.adapter = dbus.ObjectPath(adapters[0])
	w.adapterVals = all[w.adapter][adapterIface]
	w.devices = map[dbus.ObjectPath]object{}
	for path, ifaces := range all {
		if vals, ok := ifaces[deviceIface]; ok && strings.HasPrefix(string(path), adapters[0]+"/") {
			w.devices[path] = vals
		}
	}
	return conn, w, nil
}

// Status implements Manager.
func (b *BlueZ) Status(ctx context.Context) Status {
	st := Status{Devices: []Device{}}
	_, w, err := b.look(ctx)
	switch {
	case err != nil:
		b.Log.Debug("bluetooth: cannot reach BlueZ", "err", err)
		st.Problem = "The Bluetooth service of this Pi is not running."
		return st
	case w.adapter == "":
		st.Problem = "This Pi has no Bluetooth, or it is switched off in its configuration."
		return st
	}
	st.Available = true
	st.On = w.adapterVals.flag("Powered")
	st.Searching = w.adapterVals.flag("Discovering")
	for _, vals := range w.devices {
		d := Device{Address: vals.str("Address"), Name: vals.str("Alias"), Kind: kindOf(vals.str("Icon")),
			Paired: vals.flag("Paired"), Connected: vals.flag("Connected")}
		if d.Address == "" {
			continue
		}
		// Nearby there are many things without a name: other people's
		// phones, beacons, the neighbours' TV. Nobody can pick among those.
		if _, named := vals["Name"]; !named && !d.Paired {
			continue
		}
		if d.Name == "" {
			d.Name = d.Address
		}
		if rssi, ok := vals["RSSI"].Value().(int16); ok && !d.Paired {
			d.Signal = int(rssi)
		}
		st.Devices = append(st.Devices, d)
	}
	arrange(st.Devices)
	return st
}

// errOf keeps the name of a BlueZ error, which says more than its text:
// "org.bluez.Error.AuthenticationFailed: Authentication Failed".
func errOf(err error) error {
	var de dbus.Error
	if errors.As(err, &de) {
		return fmt.Errorf("%s: %s", de.Name, de.Error())
	}
	return err
}

func (b *BlueZ) call(ctx context.Context, conn *dbus.Conn, path dbus.ObjectPath, method string, wait time.Duration, args ...any) error {
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	return errOf(conn.Object(bluezService, path).CallWithContext(ctx, method, 0, args...).Err)
}

func (b *BlueZ) set(ctx context.Context, conn *dbus.Conn, path dbus.ObjectPath, iface, name string, value any) error {
	return b.call(ctx, conn, path, propsIface+".Set", 15*time.Second, iface, name, dbus.MakeVariant(value))
}

// Power implements Manager.
func (b *BlueZ) Power(ctx context.Context, on bool) error {
	conn, w, err := b.look(ctx)
	if err != nil || w.adapter == "" {
		return errors.New("this Pi has no Bluetooth that PiDash can reach")
	}
	return explain(b.set(ctx, conn, w.adapter, adapterIface, "Powered", on))
}

// Search implements Manager.
func (b *BlueZ) Search(ctx context.Context, on bool) error {
	conn, w, err := b.look(ctx)
	if err != nil || w.adapter == "" {
		return errors.New("this Pi has no Bluetooth that PiDash can reach")
	}
	b.mu.Lock()
	if b.search != nil {
		b.search.Stop()
		b.search = nil
	}
	b.mu.Unlock()
	if !on {
		err := b.call(ctx, conn, w.adapter, adapterIface+".StopDiscovery", 15*time.Second)
		// Not searching, or searching for someone else: both are fine.
		if err != nil && (strings.Contains(err.Error(), "No discovery started") || strings.Contains(err.Error(), "NotAuthorized") || strings.Contains(err.Error(), "Failed")) {
			return nil
		}
		return explain(err)
	}
	// Both kinds of Bluetooth: controllers and speakers use the classic one,
	// newer controllers, keyboards and mice the low-energy one.
	filter := map[string]dbus.Variant{"Transport": dbus.MakeVariant("auto")}
	if err := b.call(ctx, conn, w.adapter, adapterIface+".SetDiscoveryFilter", 15*time.Second, filter); err != nil {
		b.Log.Debug("bluetooth: could not say what to search for", "err", err)
	}
	if err := b.call(ctx, conn, w.adapter, adapterIface+".StartDiscovery", 15*time.Second); err != nil && !strings.Contains(err.Error(), "InProgress") {
		return explain(err)
	}
	adapter := w.adapter
	b.mu.Lock()
	b.search = time.AfterFunc(searchFor, func() {
		if err := b.call(context.Background(), conn, adapter, adapterIface+".StopDiscovery", 15*time.Second); err != nil {
			b.Log.Debug("bluetooth: ending the search", "err", err)
		}
	})
	b.mu.Unlock()
	return nil
}

// device finds a device by its address.
func (b *BlueZ) device(ctx context.Context, address string) (*dbus.Conn, world, dbus.ObjectPath, error) {
	conn, w, err := b.look(ctx)
	if err != nil || w.adapter == "" {
		return nil, w, "", errors.New("this Pi has no Bluetooth that PiDash can reach")
	}
	for path, vals := range w.devices {
		if strings.EqualFold(vals.str("Address"), address) {
			return conn, w, path, nil
		}
	}
	return nil, w, "", ErrUnknown
}

// Pair implements Manager.
func (b *BlueZ) Pair(ctx context.Context, address string) error {
	conn, w, path, err := b.device(ctx, address)
	if err != nil {
		return err
	}
	if !w.devices[path].flag("Paired") {
		if err := b.call(ctx, conn, path, deviceIface+".Pair", 45*time.Second); err != nil && !strings.Contains(err.Error(), "AlreadyExists") {
			return explain(err)
		}
	}
	// Trusted devices may connect by themselves from now on, which is what
	// switching a controller on does.
	if err := b.set(ctx, conn, path, deviceIface, "Trusted", true); err != nil {
		b.Log.Debug("bluetooth: could not trust the device", "device", address, "err", err)
	}
	// Most devices connect as part of pairing. One that has not after a few
	// seconds is asked to; if it declines it stays paired, and can be
	// connected later.
	for i := 0; i < 6; i++ {
		if _, now, _, err := b.device(ctx, address); err == nil && now.devices[path].flag("Connected") {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(500 * time.Millisecond):
		}
	}
	if err := b.call(ctx, conn, path, deviceIface+".Connect", 30*time.Second); err != nil {
		b.Log.Info("bluetooth: paired, but the device did not connect", "device", address, "err", err)
	}
	return nil
}

// Connect implements Manager.
func (b *BlueZ) Connect(ctx context.Context, address string) error {
	conn, _, path, err := b.device(ctx, address)
	if err != nil {
		return err
	}
	if err := b.call(ctx, conn, path, deviceIface+".Connect", 30*time.Second); err != nil && !strings.Contains(err.Error(), "AlreadyConnected") {
		return explain(err)
	}
	return nil
}

// Disconnect implements Manager.
func (b *BlueZ) Disconnect(ctx context.Context, address string) error {
	conn, _, path, err := b.device(ctx, address)
	if err != nil {
		return err
	}
	if err := b.call(ctx, conn, path, deviceIface+".Disconnect", 15*time.Second); err != nil && !strings.Contains(err.Error(), "NotConnected") {
		return explain(err)
	}
	return nil
}

// Forget implements Manager.
func (b *BlueZ) Forget(ctx context.Context, address string) error {
	conn, w, path, err := b.device(ctx, address)
	if err != nil {
		return err
	}
	return explain(b.call(ctx, conn, w.adapter, adapterIface+".RemoveDevice", 15*time.Second, path))
}

// agent answers what BlueZ asks while PiDash pairs a device. It is only
// asked about pairings PiDash started itself, on the request of someone at
// the settings page, so it agrees: there is no one at the TV to type a code.
// A device that insists on a code typed on the Pi is paired from the desktop.
type agent struct{ log *slog.Logger }

func (a agent) Release() *dbus.Error { return nil }
func (a agent) Cancel() *dbus.Error  { return nil }

// RequestPinCode is asked by old devices; nearly all of them want 0000.
func (a agent) RequestPinCode(dbus.ObjectPath) (string, *dbus.Error) { return "0000", nil }
func (a agent) DisplayPinCode(dbus.ObjectPath, string) *dbus.Error   { return nil }
func (a agent) RequestPasskey(dbus.ObjectPath) (uint32, *dbus.Error) { return 0, nil }
func (a agent) DisplayPasskey(dbus.ObjectPath, uint32, uint16) *dbus.Error {
	return nil
}
func (a agent) RequestConfirmation(dbus.ObjectPath, uint32) *dbus.Error { return nil }
func (a agent) RequestAuthorization(dbus.ObjectPath) *dbus.Error        { return nil }
func (a agent) AuthorizeService(dbus.ObjectPath, string) *dbus.Error    { return nil }
