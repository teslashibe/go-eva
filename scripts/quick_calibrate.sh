#!/bin/bash
# Quick calibration - just speak near the robot and we'll measure
# Usage: ./quick_calibrate.sh [distance_in_meters]

ROBOT_IP="${ROBOT_IP:-192.168.68.63}"
API_URL="http://${ROBOT_IP}:9000/api/audio/doa"
DISTANCE="${1:-1.0}"
DURATION=10

echo "🎤 Quick Distance Calibration"
echo "=============================="
echo "Robot: ${API_URL}"
echo "Assumed distance: ${DISTANCE}m"
echo ""
echo "SPEAK NOW for ${DURATION} seconds..."
echo ""

valid_energies=()

for i in $(seq 1 $((DURATION * 4))); do
    result=$(curl -s "${API_URL}" 2>/dev/null)
    speaking=$(echo "$result" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('speaking', False))" 2>/dev/null || echo "false")
    energy=$(echo "$result" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('total_energy', 0))" 2>/dev/null || echo "0")
    angle=$(echo "$result" | python3 -c "import sys,json; d=json.load(sys.stdin); print(f\"{d.get('angle', 0):.2f}\")" 2>/dev/null || echo "0")
    
    if [ "$speaking" = "True" ]; then
        echo "✓ speaking=true angle=${angle} energy=${energy}"
        if [ "$(echo "$energy > 0" | bc -l)" -eq 1 ]; then
            valid_energies+=("$energy")
        fi
    else
        echo "○ speaking=false angle=${angle}"
    fi
    sleep 0.25
done

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

if [ ${#valid_energies[@]} -gt 0 ]; then
    # Calculate average
    total=0
    for e in "${valid_energies[@]}"; do
        total=$(echo "$total + $e" | bc -l)
    done
    avg=$(echo "scale=2; $total / ${#valid_energies[@]}" | bc -l)
    
    # Calculate reference energy (k = energy * distance^2)
    ref_energy=$(echo "scale=0; $avg * $DISTANCE * $DISTANCE" | bc -l)
    
    echo ""
    echo "📊 Results (${#valid_energies[@]} valid samples):"
    echo "   Average energy at ${DISTANCE}m: ${avg}"
    echo "   Reference energy (k): ${ref_energy}"
    echo ""
    echo "📝 Update go-eva/internal/doa/source.go:"
    echo ""
    echo "const referenceEnergy = ${ref_energy}"
    echo ""
    echo "Distance formula: distance = sqrt(${ref_energy} / energy)"
else
    echo "❌ No valid samples! Make sure you're speaking loud enough."
fi

