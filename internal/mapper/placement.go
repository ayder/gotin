package mapper

import "sort"

// nearestFree places discovery cells used by proximity matching, not display layout.
func nearestFree(x, y int, occupied map[[2]int]bool) (int, int) {
	for radius := 1; radius < 1024; radius++ {
		type cand struct{ dx, dy int }
		var cands []cand
		for dx := -radius; dx <= radius; dx++ {
			for dy := -radius; dy <= radius; dy++ {
				if absInt(dx)+absInt(dy) == radius {
					cands = append(cands, cand{dx: dx, dy: dy})
				}
			}
		}
		sort.Slice(cands, func(i, j int) bool {
			if cands[i].dx != cands[j].dx {
				return cands[i].dx < cands[j].dx
			}
			return cands[i].dy < cands[j].dy
		})
		for _, c := range cands {
			nx, ny := x+c.dx, y+c.dy
			if !occupied[[2]int{nx, ny}] {
				return nx, ny
			}
		}
	}
	return x, y
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
