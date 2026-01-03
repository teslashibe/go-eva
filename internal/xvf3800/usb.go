// Package xvf3800 provides access to the XMOS XVF3800 audio DSP chip
package xvf3800

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/google/gousb"
	"github.com/teslashibe/go-eva/internal/doa"
)

// XVF3800 USB identifiers
const (
	VendorID  = 0x38FB
	ProductID = 0x1001
)

// XVF3800 control parameters
// Resource IDs and Command IDs from XMOS XVF3800 documentation
// See: https://www.xmos.com/documentation/XM-014888-PC/html/modules/fwk_xvf/doc/user_guide/AA_control_command_appendix.html
const (
	// GPO_SERVICER_RESID commands (resid=20)
	gpoResID      = 20
	doaCmdID      = 19 // DOA_VALUE_RADIANS: angle + speech flag
	doaValueCmdID = 18 // DOA_VALUE: raw uint32 values

	// AEC_RESID commands (resid=33)
	aecResID            = 33
	aecAzimuthCmdID     = 75 // AEC_AZIMUTH_VALUES: 4 floats (radians)
	aecSpEnergyCmdID    = 80 // AEC_SPENERGY_VALUES: 4 floats (speech energy per mic)
	aecMicArrayGeoCmdID = 74 // AEC_MIC_ARRAY_GEO: 12 floats (x,y,z for each mic)
)

// USBSource provides direct USB access to the XVF3800 audio DSP
// This is the preferred, pure Go implementation
type USBSource struct {
	logger *slog.Logger

	mu     sync.Mutex
	ctx    *gousb.Context
	dev    *gousb.Device
	closed bool

	// Health tracking
	healthy           bool
	consecutiveErrors int
	maxErrors         int
	lastError         error
	lastErrorTime     time.Time

	// Reconnection
	reconnectBackoff time.Duration
	maxBackoff       time.Duration
}

// USBSourceConfig configures the USB source
type USBSourceConfig struct {
	MaxConsecutiveErrors int
	InitialBackoff       time.Duration
	MaxBackoff           time.Duration
}

// DefaultUSBSourceConfig returns sensible defaults
func DefaultUSBSourceConfig() USBSourceConfig {
	return USBSourceConfig{
		MaxConsecutiveErrors: 5,
		InitialBackoff:       100 * time.Millisecond,
		MaxBackoff:           5 * time.Second,
	}
}

// NewUSBSource creates a new USB-based DOA source
func NewUSBSource(logger *slog.Logger) (*USBSource, error) {
	return NewUSBSourceWithConfig(logger, DefaultUSBSourceConfig())
}

// NewUSBSourceWithConfig creates a USB source with custom configuration
func NewUSBSourceWithConfig(logger *slog.Logger, cfg USBSourceConfig) (*USBSource, error) {
	if logger == nil {
		logger = slog.Default()
	}

	source := &USBSource{
		logger:           logger,
		healthy:          true,
		maxErrors:        cfg.MaxConsecutiveErrors,
		reconnectBackoff: cfg.InitialBackoff,
		maxBackoff:       cfg.MaxBackoff,
	}

	// Open USB context
	source.ctx = gousb.NewContext()

	// Find and open device
	if err := source.openDevice(); err != nil {
		source.ctx.Close()
		return nil, err
	}

	logger.Info("USB DOA source initialized",
		"vendor_id", fmt.Sprintf("0x%04X", VendorID),
		"product_id", fmt.Sprintf("0x%04X", ProductID),
	)

	return source, nil
}

func (u *USBSource) openDevice() error {
	dev, err := u.ctx.OpenDeviceWithVIDPID(VendorID, ProductID)
	if err != nil {
		return fmt.Errorf("failed to open XVF3800: %w", err)
	}

	if dev == nil {
		return fmt.Errorf("XVF3800 not found (VID=0x%04X PID=0x%04X)", VendorID, ProductID)
	}

	// Auto-detach kernel driver if attached
	if err := dev.SetAutoDetach(true); err != nil {
		u.logger.Debug("SetAutoDetach failed (non-fatal)", "error", err)
	}

	u.dev = dev
	u.healthy = true
	u.consecutiveErrors = 0

	return nil
}

// GetDOA returns the current direction of arrival
func (u *USBSource) GetDOA(ctx context.Context) (doa.Reading, error) {
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.closed {
		return doa.Reading{}, fmt.Errorf("device closed")
	}

	// Check if we need to reconnect
	if u.dev == nil {
		if err := u.reconnect(); err != nil {
			return doa.Reading{}, err
		}
	}

	start := time.Now()

	// USB control transfer to read DOA_VALUE_RADIANS
	// Request type: IN | Vendor | Device (0xC0)
	// wValue: 0x80 | cmdid (read flag)
	// wIndex: resid
	data := make([]byte, 9) // 1 status byte + 2 floats (4 bytes each)

	n, err := u.dev.Control(
		gousb.ControlIn|gousb.ControlVendor|gousb.ControlDevice,
		0,             // bRequest
		0x80|doaCmdID, // wValue (read flag | cmdid)
		gpoResID,      // wIndex (resid)
		data,          // data buffer
	)

	if err != nil {
		u.recordError(err)
		return doa.Reading{}, fmt.Errorf("USB control transfer failed: %w", err)
	}

	if n < 9 {
		err := fmt.Errorf("short read: got %d bytes, expected 9", n)
		u.recordError(err)
		return doa.Reading{}, err
	}

	// Check status byte
	if data[0] != 0 {
		err := fmt.Errorf("device returned error status: %d", data[0])
		u.recordError(err)
		return doa.Reading{}, err
	}

	u.recordSuccess()

	// Parse two floats (little-endian)
	angleBits := binary.LittleEndian.Uint32(data[1:5])
	speakingBits := binary.LittleEndian.Uint32(data[5:9])

	rawAngle := float64(math.Float32frombits(angleBits))
	speaking := math.Float32frombits(speakingBits) != 0

	latency := time.Since(start)

	// Read enhanced data (speech energy and per-mic azimuths)
	energyValues, azimuthValues := u.readEnhancedData()

	return doa.Reading{
		Angle:        doa.ToEvaAngle(rawAngle),
		RawAngle:     rawAngle,
		Speaking:     speaking,
		Timestamp:    time.Now(),
		LatencyMs:    latency.Milliseconds(),
		SpeechEnergy: energyValues,
		MicAzimuths:  azimuthValues,
		TotalEnergy:  sumEnergy(energyValues),
	}, nil
}

// readEnhancedData reads additional XVF3800 parameters for speech energy and per-mic azimuths.
// These are optional - errors are logged but don't fail the main DOA read.
func (u *USBSource) readEnhancedData() (energy [4]float64, azimuths [4]float64) {
	// Read AEC_SPENERGY_VALUES (4 floats)
	energyData := make([]byte, 17) // 1 status + 4 floats
	n, err := u.dev.Control(
		gousb.ControlIn|gousb.ControlVendor|gousb.ControlDevice,
		0,
		0x80|aecSpEnergyCmdID,
		aecResID,
		energyData,
	)
	if err == nil && n >= 17 && energyData[0] == 0 {
		for i := 0; i < 4; i++ {
			bits := binary.LittleEndian.Uint32(energyData[1+i*4 : 5+i*4])
			energy[i] = float64(math.Float32frombits(bits))
		}
	}

	// Read AEC_AZIMUTH_VALUES (4 floats in radians)
	azimuthData := make([]byte, 17) // 1 status + 4 floats
	n, err = u.dev.Control(
		gousb.ControlIn|gousb.ControlVendor|gousb.ControlDevice,
		0,
		0x80|aecAzimuthCmdID,
		aecResID,
		azimuthData,
	)
	if err == nil && n >= 17 && azimuthData[0] == 0 {
		for i := 0; i < 4; i++ {
			bits := binary.LittleEndian.Uint32(azimuthData[1+i*4 : 5+i*4])
			azimuths[i] = float64(math.Float32frombits(bits))
		}
	}

	return energy, azimuths
}

// sumEnergy calculates total speech energy across all mics
func sumEnergy(energy [4]float64) float64 {
	var total float64
	for _, e := range energy {
		total += e
	}
	return total
}

func (u *USBSource) recordError(err error) {
	u.consecutiveErrors++
	u.lastError = err
	u.lastErrorTime = time.Now()

	if u.consecutiveErrors >= u.maxErrors {
		u.healthy = false
		u.logger.Warn("USB source marked unhealthy, will attempt reconnect",
			"consecutive_errors", u.consecutiveErrors,
			"last_error", err,
		)

		// Close device to force reconnect on next call
		if u.dev != nil {
			u.dev.Close()
			u.dev = nil
		}
	}
}

func (u *USBSource) recordSuccess() {
	if u.consecutiveErrors > 0 {
		u.logger.Info("USB source recovered",
			"previous_errors", u.consecutiveErrors,
		)
	}
	u.consecutiveErrors = 0
	u.healthy = true
	u.reconnectBackoff = DefaultUSBSourceConfig().InitialBackoff
}

func (u *USBSource) reconnect() error {
	u.logger.Info("attempting USB reconnect",
		"backoff", u.reconnectBackoff,
	)

	// Apply backoff
	time.Sleep(u.reconnectBackoff)

	// Increase backoff for next attempt
	u.reconnectBackoff *= 2
	if u.reconnectBackoff > u.maxBackoff {
		u.reconnectBackoff = u.maxBackoff
	}

	// Try to reopen device
	if err := u.openDevice(); err != nil {
		u.logger.Warn("USB reconnect failed", "error", err)
		return err
	}

	u.logger.Info("USB reconnect successful")
	return nil
}

// Close releases the USB device
func (u *USBSource) Close() error {
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.closed {
		return nil
	}

	u.closed = true

	if u.dev != nil {
		u.dev.Close()
		u.dev = nil
	}

	if u.ctx != nil {
		u.ctx.Close()
		u.ctx = nil
	}

	u.logger.Info("USB source closed")

	return nil
}

// Healthy returns true if the source is operational
func (u *USBSource) Healthy() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.healthy
}

// Name returns the source type name
func (u *USBSource) Name() string {
	return "usb"
}

// Stats returns USB source statistics
func (u *USBSource) Stats() USBStats {
	u.mu.Lock()
	defer u.mu.Unlock()

	var lastErr string
	if u.lastError != nil {
		lastErr = u.lastError.Error()
	}

	return USBStats{
		Healthy:           u.healthy,
		ConsecutiveErrors: u.consecutiveErrors,
		LastError:         lastErr,
		LastErrorTime:     u.lastErrorTime,
		DeviceConnected:   u.dev != nil,
	}
}

// USBStats contains USB source statistics
type USBStats struct {
	Healthy           bool      `json:"healthy"`
	ConsecutiveErrors int       `json:"consecutive_errors"`
	LastError         string    `json:"last_error,omitempty"`
	LastErrorTime     time.Time `json:"last_error_time,omitempty"`
	DeviceConnected   bool      `json:"device_connected"`
}
