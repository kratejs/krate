package scene3d

import "math"

// Light and exposure calibration. These constants mirror
// packages/3d/src/runtime/calibration.ts; calibration_test.go parses that file
// and fails if either side drifts.
const (
	// DirectionalLuxPerUnit is Filament illuminance (lux) per Three.js
	// directional intensity unit.
	DirectionalLuxPerUnit = 110000.0
	// AmbientIBLPerUnit is the IBL intensity per Three.js ambient unit.
	AmbientIBLPerUnit = 30000.0

	// DefaultAperture, DefaultShutter and DefaultISO are the default camera
	// exposure: f/16, 1/125 s, ISO 100.
	DefaultAperture = 16.0
	DefaultShutter  = 1.0 / 125
	DefaultISO      = 100.0
)

// PointLumensPerUnit is the luminous power (lumens) per Three.js point or
// spot intensity unit: 4 * pi * 1000.
var PointLumensPerUnit = 4 * math.Pi * 1000

// LightKind is a Three.js light type.
type LightKind string

// Light kinds understood by the pass.
const (
	LightDirectional LightKind = "directional"
	LightPoint       LightKind = "point"
	LightSpot        LightKind = "spot"
	LightAmbient     LightKind = "ambient"
)

// LightIntensity converts a Three.js-scale intensity into Filament units.
func LightIntensity(kind LightKind, intensity float64) float64 {
	switch kind {
	case LightDirectional:
		return intensity * DirectionalLuxPerUnit
	case LightAmbient:
		return intensity * AmbientIBLPerUnit
	}
	return intensity * PointLumensPerUnit
}
