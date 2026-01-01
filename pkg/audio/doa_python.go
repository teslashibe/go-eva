package audio

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// PythonDOA reads DOA using Python (works on the robot where gousb has issues)
type PythonDOA struct {
	pythonPath string
}

// NewPythonDOA creates a Python-based DOA reader
func NewPythonDOA() *PythonDOA {
	// Try to find Python in the mini_daemon venv
	pythonPath := "/venvs/mini_daemon/bin/python3"
	return &PythonDOA{pythonPath: pythonPath}
}

// GetDOA reads DOA using Python's USB library
func (p *PythonDOA) GetDOA() (angle float64, speaking bool, err error) {
	script := `
from reachy_mini.media.audio_control_utils import init_respeaker_usb
rs = init_respeaker_usb()
if rs:
    r = rs.read('DOA_VALUE_RADIANS')
    print(r[0], r[1])
    rs.close()
else:
    print("ERROR: No device")
`

	cmd := exec.Command(p.pythonPath, "-c", script)
	output, err := cmd.Output()
	if err != nil {
		return 0, false, fmt.Errorf("python DOA failed: %w", err)
	}

	// Parse output: "1.45 0.0" or "1.45 1.0"
	parts := strings.Fields(strings.TrimSpace(string(output)))
	if len(parts) < 2 {
		if strings.Contains(string(output), "ERROR") {
			return 0, false, fmt.Errorf("XVF3800 device not found")
		}
		return 0, false, fmt.Errorf("unexpected output: %s", output)
	}

	angle, err = strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, false, fmt.Errorf("invalid angle: %s", parts[0])
	}

	speakingVal, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return 0, false, fmt.Errorf("invalid speaking: %s", parts[1])
	}

	speaking = speakingVal != 0

	if Debug {
		fmt.Printf("🎤 Python DOA: angle=%.3f, speaking=%v\n", angle, speaking)
	}

	return angle, speaking, nil
}

