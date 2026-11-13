package bluetooth

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// fakeBlueZ starts a message bus of its own with a stand-in for the BlueZ
// service on it (python-dbusmock's), and points the system bus address at it.
// The test is skipped where those are not installed:
//
//	sudo apt install dbus-daemon python3-dbusmock
func fakeBlueZ(t *testing.T) *dbus.Conn {
	t.Helper()
	daemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon is not installed")
	}
	python := ""
	for _, name := range []string{"python3", "python3.12", "python3.11", "python3.13"} {
		if p, err := exec.LookPath(name); err == nil && exec.Command(p, "-c", "import dbusmock").Run() == nil {
			python = p
			break
		}
	}
	if python == "" {
		t.Skip("python3-dbusmock is not installed")
	}

	bus := exec.Command(daemon, "--session", "--nofork", "--print-address")
	out, err := bus.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := bus.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { bus.Process.Kill(); bus.Wait() })
	address, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", strings.TrimSpace(address))

	mock := exec.Command(python, "-m", "dbusmock", "--system", "--template", "bluez5")
	mock.Env = append(os.Environ(), "DBUS_SYSTEM_BUS_ADDRESS="+strings.TrimSpace(address))
	if err := mock.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mock.Process.Kill(); mock.Wait() })

	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	for i := 0; ; i++ {
		var owner string
		if conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, bluezService).Store(&owner) == nil {
			break
		}
		if i > 100 {
			t.Fatal("the stand-in for BlueZ did not start")
		}
		time.Sleep(50 * time.Millisecond)
	}
	return conn
}

// tell calls one of the stand-in's own methods, such as "add a device".
func tell(t *testing.T, conn *dbus.Conn, method string, args ...any) {
	t.Helper()
	if err := conn.Object(bluezService, "/").Call("org.bluez.Mock."+method, 0, args...).Err; err != nil {
		t.Fatalf("%s: %v", method, err)
	}
}

func names(st Status) string {
	var out []string
	for _, d := range st.Devices {
		s := d.Name
		if d.Paired {
			s += " paired"
		}
		if d.Connected {
			s += " connected"
		}
		out = append(out, s)
	}
	return strings.Join(out, " | ")
}

func TestAgainstBlueZ(t *testing.T) {
	conn := fakeBlueZ(t)
	ctx := context.Background()
	b := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer b.Close()

	// A Pi without Bluetooth says so instead of failing.
	if st := b.Status(ctx); st.Available || st.Problem == "" {
		t.Fatalf("without an adapter: %+v", st)
	}
	if err := b.Search(ctx, true); err == nil {
		t.Error("searching without Bluetooth should say why it cannot")
	}

	tell(t, conn, "AddAdapter", "hci0", "raspberrypi")
	tell(t, conn, "AddDevice", "hci0", "AA:BB:CC:00:00:01", "Xbox Wireless Controller")
	tell(t, conn, "AddDevice", "hci0", "AA:BB:CC:00:00:02", "Living room speaker")
	tell(t, conn, "PairDevice", "hci0", "AA:BB:CC:00:00:02")

	st := b.Status(ctx)
	if !st.Available || !st.On || st.Searching {
		t.Fatalf("status = %+v", st)
	}
	// Paired first, then what is nearby.
	if got := names(st); got != "Living room speaker paired | Xbox Wireless Controller" {
		t.Errorf("devices = %s", got)
	}
	if st.Devices[1].Signal == 0 || st.Devices[0].Signal != 0 {
		t.Errorf("signal belongs to devices nearby only: %+v", st.Devices)
	}

	// Searching, and stopping again.
	if err := b.Search(ctx, true); err != nil {
		t.Fatal(err)
	}
	if !b.Status(ctx).Searching {
		t.Error("the search did not start")
	}
	if err := b.Search(ctx, true); err != nil {
		t.Errorf("asking twice must not be an error: %v", err)
	}
	if err := b.Search(ctx, false); err != nil {
		t.Fatal(err)
	}
	if b.Status(ctx).Searching {
		t.Error("the search did not stop")
	}

	// Pairing makes the device the Pi's own and trusted, so that it may
	// connect by itself later, which is what switching a controller on does.
	if err := b.Pair(ctx, "aa:bb:cc:00:00:01"); err != nil { // addresses in either case
		t.Fatal(err)
	}
	if got := names(b.Status(ctx)); got != "Living room speaker paired | Xbox Wireless Controller paired" {
		t.Errorf("after pairing: %s", got)
	}
	var trusted dbus.Variant
	if err := conn.Object(bluezService, "/org/bluez/hci0/dev_AA_BB_CC_00_00_01").Call(propsIface+".Get", 0, deviceIface, "Trusted").Store(&trusted); err != nil || trusted.Value() != true {
		t.Errorf("the paired device should be trusted: %v, %v", trusted, err)
	}
	if err := b.Pair(ctx, "AA:BB:CC:00:00:01"); err != nil {
		t.Errorf("pairing what is paired already must be fine: %v", err)
	}

	// Connecting and disconnecting are accepted, also when the device is that
	// way already. (The stand-in keeps "connected" to itself after these
	// calls, so what the list shows is checked separately below.)
	for i, do := range []func(context.Context, string) error{b.Disconnect, b.Disconnect, b.Connect, b.Connect} {
		if err := do(ctx, "AA:BB:CC:00:00:01"); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	tell(t, conn, "ConnectDevice", "hci0", "AA:BB:CC:00:00:01")
	if got := names(b.Status(ctx)); got != "Living room speaker paired | Xbox Wireless Controller paired connected" {
		t.Errorf("a connected device is not shown as connected: %s", got)
	}

	// A controller that reports its battery has it shown; the speaker, which
	// does not, has none.
	pad := conn.Object(bluezService, "/org/bluez/hci0/dev_AA_BB_CC_00_00_01")
	if err := pad.Call("org.freedesktop.DBus.Mock.AddProperties", 0, batteryIface, map[string]dbus.Variant{"Percentage": dbus.MakeVariant(uint8(35))}).Err; err != nil {
		t.Fatal(err)
	}
	st = b.Status(ctx)
	if st.Devices[0].Battery != nil || st.Devices[1].Battery == nil || *st.Devices[1].Battery != 35 {
		t.Errorf("battery: speaker %v, controller %v; want none and 35", st.Devices[0].Battery, st.Devices[1].Battery)
	}

	// Forgetting.
	if err := b.Forget(ctx, "AA:BB:CC:00:00:02"); err != nil {
		t.Fatal(err)
	}
	if got := names(b.Status(ctx)); got != "Xbox Wireless Controller paired connected" {
		t.Errorf("after forgetting the speaker: %s", got)
	}
	for name, do := range map[string]func(context.Context, string) error{"pair": b.Pair, "connect": b.Connect, "disconnect": b.Disconnect, "forget": b.Forget} {
		if err := do(ctx, "AA:BB:CC:00:00:02"); err != ErrUnknown {
			t.Errorf("%s of a device that is gone: %v, want ErrUnknown", name, err)
		}
	}

	// Switching Bluetooth off and on.
	if err := b.Power(ctx, false); err != nil {
		t.Fatal(err)
	}
	if st := b.Status(ctx); st.On || !st.Available {
		t.Errorf("after switching off: %+v", st)
	}
	if err := b.Power(ctx, true); err != nil {
		t.Fatal(err)
	}
	if !b.Status(ctx).On {
		t.Error("Bluetooth did not come back on")
	}
}

func TestThingsWithoutANameAreLeftOut(t *testing.T) {
	// Covered without BlueZ: the rule that decides what is listed.
	devices := []Device{
		{Address: "3", Name: "Zeta", Signal: -80},
		{Address: "1", Name: "beta", Paired: true},
		{Address: "4", Name: "Near", Signal: -40},
		{Address: "2", Name: "Alpha", Paired: true},
		{Address: "5", Name: "Unknown strength"},
	}
	arrange(devices)
	var got []string
	for _, d := range devices {
		got = append(got, d.Name)
	}
	if want := "Alpha beta Near Zeta Unknown strength"; strings.Join(got, " ") != want {
		t.Errorf("order = %v, want %s", got, want)
	}
	for icon, want := range map[string]string{"input-gaming": "controller", "audio-headset": "audio", "audio-card": "audio", "input-keyboard": "keyboard", "input-mouse": "mouse", "phone": "phone", "": "other"} {
		if got := kindOf(icon); got != want {
			t.Errorf("kindOf(%q) = %s, want %s", icon, got, want)
		}
	}
}

func TestBlueZErrorsAreExplained(t *testing.T) {
	for raw, want := range map[string]string{
		"org.bluez.Error.AuthenticationFailed: Authentication Failed": "pairing mode",
		"org.bluez.Error.Failed: br-connection-page-timeout":          "did not answer",
		"org.bluez.Error.NotReady: Resource Not Ready":                "switched off",
		"org.bluez.Error.InProgress: Operation already in progress":   "still busy",
		"org.freedesktop.DBus.Error.AccessDenied: Rejected send":      "not allowed",
		"org.bluez.Error.Failed: Blocked through rfkill":              "rfkill unblock",
		"org.freedesktop.DBus.Error.UnknownObject: no such object":    "not known any more",
	} {
		if e := explain(errOf(dbus.Error{Name: strings.SplitN(raw, ":", 2)[0], Body: []any{strings.TrimSpace(strings.SplitN(raw, ":", 2)[1])}})); e == nil || !strings.Contains(e.Error(), want) {
			t.Errorf("explain(%s) = %v, want something about %q", raw, e, want)
		}
	}
	if explain(nil) != nil {
		t.Error("no error must stay no error")
	}
}

func TestBatteriesKnownToLinuxItself(t *testing.T) {
	dir := t.TempDir()
	add := func(name, scope, capacity string) {
		os.MkdirAll(dir+"/"+name, 0o755)
		os.WriteFile(dir+"/"+name+"/scope", []byte(scope+"\n"), 0o644)
		os.WriteFile(dir+"/"+name+"/capacity", []byte(capacity+"\n"), 0o644)
	}
	add("ps-controller-battery-a4:53:85:01:02:03", "Device", "62")
	add("hid-AA:BB:CC:00:00:09-battery", "Device", "15")
	add("BAT0", "System", "90")                           // the computer's own battery
	add("hid-11:22:33:44:55:66-battery", "Device", "n/a") // a driver that does not know
	got := kernelBatteries(dir)
	if len(got) != 2 || got["a4:53:85:01:02:03"] != 62 || got["aa:bb:cc:00:00:09"] != 15 {
		t.Errorf("batteries = %v", got)
	}
	if len(kernelBatteries(dir+"/missing")) != 0 {
		t.Error("a system without this directory has no batteries, and no error")
	}
}
