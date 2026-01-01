//go:build cgo && libusb

// Package audio provides interfaces to the XVF3800 audio DSP chip
package audio

import (
	"encoding/binary"
	"fmt"
	"math"
	"sync"

	"github.com/google/gousb"
)

// XVF3800 USB identifiers
const (
	vendorID  = 0x38FB
	productID = 0x1001
)

// XVF3800 control parameters
// Format: (resid, cmdid, length, type)
var parameters = map[string]struct {
	resid  uint16
	cmdid  uint16
	length int
}{
	"DOA_VALUE_RADIANS": {20, 19, 2},
	"DOA_VALUE":         {20, 18, 2},
	"AEC_NUM_MICS":      {33, 71, 1},
	"AEC_SPENERGY":      {33, 80, 4},
}

// XVF3800 provides access to the XMOS XVF3800 audio DSP chip
type XVF3800 struct {
	ctx    *gousb.Context
	dev    *gousb.Device
	mu     sync.Mutex
	closed bool
}

// NewXVF3800 opens a connection to the XVF3800 USB device
func NewXVF3800() (*XVF3800, error) {
	ctx := gousb.NewContext()

	dev, err := ctx.OpenDeviceWithVIDPID(vendorID, productID)
	if err != nil {
		ctx.Close()
		return nil, fmt.Errorf("failed to open XVF3800: %w", err)
	}

	if dev == nil {
		ctx.Close()
		return nil, fmt.Errorf("XVF3800 device not found (VID=%04X PID=%04X)", vendorID, productID)
	}

	// Set auto-detach for kernel driver
	if err := dev.SetAutoDetach(true); err != nil {
		if Debug {
			fmt.Printf("🎤 Warning: SetAutoDetach failed: %v\n", err)
		}
	}

	return &XVF3800{
		ctx: ctx,
		dev: dev,
	}, nil
}

// GetDOA returns the Direction of Arrival angle and speech detection status
// Angle is in radians: 0 = left, π/2 = front, π = right
func (x *XVF3800) GetDOA() (angle float64, speaking bool, err error) {
	x.mu.Lock()
	defer x.mu.Unlock()

	if x.closed {
		return 0, false, fmt.Errorf("device closed")
	}

	param := parameters["DOA_VALUE_RADIANS"]

	// USB control transfer to read DOA
	// Request type: IN | Vendor | Device
	// wValue: 0x80 | cmdid (read flag)
	// wIndex: resid
	data := make([]byte, param.length*4+1) // floats are 4 bytes + 1 status byte

	n, err := x.dev.Control(
		gousb.ControlIn|gousb.ControlVendor|gousb.ControlDevice,
		0,                      // bRequest
		0x80|param.cmdid,       // wValue (read flag | cmdid)
		param.resid,            // wIndex (resid)
		data,                   // data buffer
	)

	if err != nil {
		return 0, false, fmt.Errorf("USB control transfer failed: %w", err)
	}

	if n < 9 {
		return 0, false, fmt.Errorf("short read: got %d bytes, expected 9", n)
	}

	// Check status byte
	if data[0] != 0 {
		return 0, false, fmt.Errorf("device returned error status: %d", data[0])
	}

	// Parse two floats (little-endian)
	angleBits := binary.LittleEndian.Uint32(data[1:5])
	speakingBits := binary.LittleEndian.Uint32(data[5:9])

	angle = float64(math.Float32frombits(angleBits))
	speaking = math.Float32frombits(speakingBits) != 0

	if Debug {
		fmt.Printf("🎤 DOA raw: angle=%.3f rad, speaking=%v\n", angle, speaking)
	}

	return angle, speaking, nil
}

// GetSpeechEnergy returns speech energy values for each mic channel
func (x *XVF3800) GetSpeechEnergy() ([]float64, error) {
	x.mu.Lock()
	defer x.mu.Unlock()

	if x.closed {
		return nil, fmt.Errorf("device closed")
	}

	param := parameters["AEC_SPENERGY"]

	data := make([]byte, param.length*4+1)

	n, err := x.dev.Control(
		gousb.ControlIn|gousb.ControlVendor|gousb.ControlDevice,
		0,
		0x80|param.cmdid,
		param.resid,
		data,
	)

	if err != nil {
		return nil, fmt.Errorf("USB control transfer failed: %w", err)
	}

	if n < param.length*4+1 {
		return nil, fmt.Errorf("short read: got %d bytes", n)
	}

	if data[0] != 0 {
		return nil, fmt.Errorf("device returned error status: %d", data[0])
	}

	energies := make([]float64, param.length)
	for i := 0; i < param.length; i++ {
		bits := binary.LittleEndian.Uint32(data[1+i*4 : 5+i*4])
		energies[i] = float64(math.Float32frombits(bits))
	}

	return energies, nil
}

// Close releases the USB device
func (x *XVF3800) Close() error {
	x.mu.Lock()
	defer x.mu.Unlock()

	if x.closed {
		return nil
	}

	x.closed = true

	if x.dev != nil {
		x.dev.Close()
	}
	if x.ctx != nil {
		x.ctx.Close()
	}

	return nil
}


