package mapper

// DirectionVector returns the (dx, dy, dz) integer offset implied by walking
// one step in d. The bool reports whether d is a recognised Cartesian
// direction.
//
// In/Out are bridge directions without a Cartesian offset in the current
// layer, so callers get ok=false for them.
func DirectionVector(d Direction) (dx, dy, dz int, ok bool) {
	switch d {
	case North:
		return 0, 1, 0, true
	case South:
		return 0, -1, 0, true
	case East:
		return 1, 0, 0, true
	case West:
		return -1, 0, 0, true
	case NorthEast:
		return 1, 1, 0, true
	case NorthWest:
		return -1, 1, 0, true
	case SouthEast:
		return 1, -1, 0, true
	case SouthWest:
		return -1, -1, 0, true
	case Up:
		return 0, 0, 1, true
	case Down:
		return 0, 0, -1, true
	}
	return 0, 0, 0, false
}
