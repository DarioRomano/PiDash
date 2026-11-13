//go:build linux

package cec

import (
	"context"
	"encoding/binary"
	"errors"
	"log/slog"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// ioctl numbers and modes from linux/cec.h. The struct sizes are part of the
// numbers and are the same on 32- and 64-bit ARM.
const (
	cecAdapGPhysAddr = 0x80026101
	cecAdapGLogAddrs = 0x805c6103
	cecTransmit      = 0xc0386105
	cecReceive       = 0xc0386106
	cecSMode         = 0x40046109

	cecModeInitiator = 0x01
	cecModeFollower  = 0x10
	cecRxStatusOK    = 0x01
)

// cecMsg mirrors struct cec_msg (56 bytes).
type cecMsg struct {
	TxTs, RxTs                    uint64
	Len, Timeout, Sequence, Flags uint32
	Msg                           [16]byte
	Reply, RxStatus, TxStatus     byte
	TxCounters                    [4]byte
	_                             byte
}

// The layout must match the kernel's on every architecture PiDash is built
// for; these fail to compile if it does not.
var (
	_ [0]byte = [unsafe.Sizeof(cecMsg{}) - 56]byte{}
	_ [0]byte = [unsafe.Offsetof(cecMsg{}.Len) - 16]byte{}
	_ [0]byte = [unsafe.Offsetof(cecMsg{}.Timeout) - 20]byte{}
	_ [0]byte = [unsafe.Offsetof(cecMsg{}.Msg) - 32]byte{}
	_ [0]byte = [unsafe.Offsetof(cecMsg{}.RxStatus) - 49]byte{}
)

func ioctl(fd int, req uintptr, arg unsafe.Pointer) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(arg)); errno != 0 {
		return errno
	}
	return nil
}

// Follow answers the TV's questions on every adapter for as long as ctx
// lives, so the TV keeps sending the remote's keys to the Pi. It also
// registers the Pi on an adapter that has a TV but was never set up, for
// example because the TV was off when the dashboard started.
func (c *Controller) Follow(ctx context.Context, log *slog.Logger) {
	var wg sync.WaitGroup
	for _, dev := range c.Devices {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reported := false
			for ctx.Err() == nil {
				err := c.follow(ctx, dev, log)
				// A machine without this HDMI port is normal; say anything else once.
				if err != nil && !errors.Is(err, syscall.ENOENT) && !reported {
					log.Warn("cannot answer the TV over HDMI-CEC; the remote may stop working after switching inputs", "device", dev, "err", err)
					reported = true
				}
				select {
				case <-ctx.Done():
				case <-time.After(30 * time.Second):
				}
			}
		}()
	}
	wg.Wait()
}

func (c *Controller) follow(ctx context.Context, dev string, log *slog.Logger) error {
	fd, err := syscall.Open(dev, syscall.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)
	// Receive what is sent to the Pi, next to anything else that listens.
	mode := uint32(cecModeInitiator | cecModeFollower)
	if err := ioctl(fd, cecSMode, unsafe.Pointer(&mode)); err != nil {
		return err
	}
	log.Debug("answering the TV over HDMI-CEC", "device", dev)

	r := &responder{phys: physInvalid, addr: laInvalid, active: &c.active}
	var lastRegister time.Time
	for ctx.Err() == nil {
		// Where the Pi is on the bus can change at any time: the TV drops and
		// restores the connection when it is switched off or to another input.
		var phys uint16
		var addrs [92]byte // struct cec_log_addrs
		if err := ioctl(fd, cecAdapGPhysAddr, unsafe.Pointer(&phys)); err != nil {
			return err
		}
		if err := ioctl(fd, cecAdapGLogAddrs, unsafe.Pointer(&addrs)); err != nil {
			return err
		}
		registered := addrs[7] > 0 // num_log_addrs: the Pi was set up as a device
		r.phys, r.addr = phys, laInvalid
		if registered && binary.LittleEndian.Uint16(addrs[4:6]) != 0 {
			r.addr = addrs[0]
		}
		if phys != physInvalid && !registered && time.Since(lastRegister) > time.Minute {
			lastRegister = time.Now()
			if _, err := c.run(ctx, "-d", dev, "--playback", "--osd-name", c.OSDName); err != nil {
				log.Debug("registering with the TV", "device", dev, "err", err)
			} else {
				log.Info("registered the Pi with the TV over HDMI-CEC", "device", dev)
			}
			continue
		}

		// Wait up to a second for a message, then look at the state again.
		msg := cecMsg{Timeout: 1000}
		switch err := ioctl(fd, cecReceive, unsafe.Pointer(&msg)); {
		case errors.Is(err, syscall.ETIMEDOUT), errors.Is(err, syscall.EINTR), errors.Is(err, syscall.EAGAIN):
			continue
		case err != nil:
			return err
		}
		if msg.RxStatus&cecRxStatusOK == 0 || msg.Len == 0 || msg.Len > 16 {
			continue
		}
		data := msg.Msg[:msg.Len]
		log.Debug("cec received", "device", dev, "msg", describe(data))
		out, note := r.handle(data)
		for _, reply := range out {
			tx := cecMsg{Len: uint32(len(reply))}
			copy(tx.Msg[:], reply)
			if err := ioctl(fd, cecTransmit, unsafe.Pointer(&tx)); err != nil {
				log.Debug("cec answer not sent", "device", dev, "msg", describe(reply), "err", err)
				note = ""
			}
		}
		if note != "" {
			log.Info("cec: "+note, "device", dev)
		}
	}
	return nil
}
