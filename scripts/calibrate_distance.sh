#!/bin/bash
# Distance Calibration Script for XVF3800 DOA
# Run this to calibrate the energy-to-distance mapping

set -e

ROBOT_IP="${ROBOT_IP:-192.168.68.63}"
API_URL="http://${ROBOT_IP}:9000/api/audio/doa"
SAMPLES_PER_DISTANCE=20
OUTPUT_FILE="calibration_data.json"

echo "🎤 XVF3800 Distance Calibration Script"
echo "======================================="
echo ""
echo "Robot API: ${API_URL}"
echo ""
echo "This will guide you through measuring speech energy at different distances."
echo "You'll need to speak continuously at each distance while we collect samples."
echo ""

# Function to collect samples
collect_samples() {
    local distance=$1
    local samples=()
    local valid_count=0
    local total_energy=0
    
    echo ""
    echo "📏 Distance: ${distance}m - SPEAK NOW for 5 seconds..."
    sleep 1  # Give user time to start speaking
    
    for i in $(seq 1 $SAMPLES_PER_DISTANCE); do
        result=$(curl -s "${API_URL}" 2>/dev/null)
        speaking=$(echo "$result" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('speaking', False))" 2>/dev/null || echo "false")
        energy=$(echo "$result" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('total_energy', 0))" 2>/dev/null || echo "0")
        
        if [ "$speaking" = "True" ] && [ "$(echo "$energy > 0" | bc -l)" -eq 1 ]; then
            samples+=("$energy")
            valid_count=$((valid_count + 1))
            total_energy=$(echo "$total_energy + $energy" | bc -l)
            echo -n "█"
        else
            echo -n "░"
        fi
        sleep 0.25
    done
    
    echo ""
    
    if [ $valid_count -gt 0 ]; then
        avg_energy=$(echo "scale=2; $total_energy / $valid_count" | bc -l)
        echo "   ✅ Got $valid_count valid samples, avg energy: $avg_energy"
        echo "$distance,$avg_energy,$valid_count"
    else
        echo "   ❌ No valid samples! Make sure you're speaking loud enough."
        echo "$distance,0,0"
    fi
}

# Main calibration loop
echo "We'll measure at 4 distances: 0.5m, 1m, 2m, and 3m"
echo ""
read -p "Press ENTER when ready to start..."

MEASUREMENTS=()

# Distance 1: 0.5 meters
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "STEP 1: Stand 0.5 meters (arm's length) from the robot"
read -p "Press ENTER when in position..."
M1=$(collect_samples 0.5)
MEASUREMENTS+=("$M1")

# Distance 2: 1 meter
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "STEP 2: Stand 1 meter from the robot"
read -p "Press ENTER when in position..."
M2=$(collect_samples 1.0)
MEASUREMENTS+=("$M2")

# Distance 3: 2 meters
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "STEP 3: Stand 2 meters from the robot"
read -p "Press ENTER when in position..."
M3=$(collect_samples 2.0)
MEASUREMENTS+=("$M3")

# Distance 4: 3 meters
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "STEP 4: Stand 3 meters from the robot"
read -p "Press ENTER when in position..."
M4=$(collect_samples 3.0)
MEASUREMENTS+=("$M4")

# Parse and calculate
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "📊 CALIBRATION RESULTS"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "Distance | Avg Energy    | Samples"
echo "---------|---------------|--------"

# Store for JSON output
JSON_DATA="["

for m in "${MEASUREMENTS[@]}"; do
    dist=$(echo "$m" | cut -d',' -f1)
    energy=$(echo "$m" | cut -d',' -f2)
    samples=$(echo "$m" | cut -d',' -f3)
    printf "  %4sm  | %13s | %s\n" "$dist" "$energy" "$samples"
    
    if [ "$JSON_DATA" != "[" ]; then
        JSON_DATA="${JSON_DATA},"
    fi
    JSON_DATA="${JSON_DATA}{\"distance\":$dist,\"energy\":$energy,\"samples\":$samples}"
done

JSON_DATA="${JSON_DATA}]"

echo ""
echo "Raw data saved to: $OUTPUT_FILE"
echo "$JSON_DATA" | python3 -m json.tool > "$OUTPUT_FILE"

# Calculate calibration constants using inverse square law fitting
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🔧 CALCULATING CALIBRATION CONSTANTS"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

python3 << EOF
import json

data = $JSON_DATA

# Filter out zero measurements
valid = [d for d in data if d['energy'] > 0 and d['samples'] > 0]

if len(valid) < 2:
    print("❌ Not enough valid measurements for calibration!")
    exit(1)

# Use inverse square law: energy = k / distance^2
# So: distance = sqrt(k / energy)
# We need to find k (reference energy at 1m)

# Calculate k for each measurement and average
k_values = []
for d in valid:
    # k = energy * distance^2
    k = d['energy'] * (d['distance'] ** 2)
    k_values.append(k)
    print(f"  d={d['distance']}m, E={d['energy']:.0f} -> k={k:.0f}")

avg_k = sum(k_values) / len(k_values)
print(f"\n  Average k (reference constant): {avg_k:.0f}")

# The energy at 1m should be approximately k
energy_at_1m = avg_k

print(f"\n📋 CALIBRATION VALUES FOR source.go:")
print(f"   const referenceEnergy = {energy_at_1m:.0f}")
print(f"\n   Distance formula: distance = sqrt({energy_at_1m:.0f} / energy)")

# Test the formula
print(f"\n🧪 Formula verification:")
for d in valid:
    import math
    predicted_dist = math.sqrt(avg_k / d['energy']) if d['energy'] > 0 else 0
    error = abs(predicted_dist - d['distance'])
    print(f"   {d['distance']}m -> predicted {predicted_dist:.2f}m (error: {error:.2f}m)")

# Output the Go code snippet
print(f"\n📝 Go code to update in source.go EstimatedDistance():")
print(f'''
// Calibrated reference energy at 1 meter (from calibration script)
const referenceEnergy = {avg_k:.0f}

func (r Reading) EstimatedDistance() float64 {{
    if r.TotalEnergy <= 0 {{
        return 0
    }}
    // Inverse square law: distance = sqrt(refEnergy / measuredEnergy)
    distance := math.Sqrt({avg_k:.0f} / r.TotalEnergy)
    // Clamp to reasonable range (0.3m - 5m)
    if distance < 0.3 {{
        distance = 0.3
    }}
    if distance > 5.0 {{
        distance = 5.0
    }}
    return distance
}}
''')
EOF

echo ""
echo "✅ Calibration complete!"
echo ""

