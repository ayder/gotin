package mapper

import (
	"math"
	"sort"
)

// SolverConfig controls layout solver behaviour. Zero values resolve to
// deterministic defaults via NewSolver.
type SolverConfig struct {
	RestLength    float64
	KSpring       float64
	KRepel        float64
	Damping       float64
	MaxIterations int
	EpsilonEnergy float64
}

// Solver computes settled integer-cell positions for a map.
type Solver struct{ cfg SolverConfig }

func NewSolver(cfg SolverConfig) *Solver {
	if cfg.RestLength == 0 {
		cfg.RestLength = 1
	}
	if cfg.KSpring == 0 {
		cfg.KSpring = 0.5
	}
	if cfg.KRepel == 0 {
		cfg.KRepel = 0.05
	}
	if cfg.Damping == 0 {
		cfg.Damping = 0.85
	}
	if cfg.MaxIterations == 0 {
		cfg.MaxIterations = 500
	}
	if cfg.EpsilonEnergy == 0 {
		cfg.EpsilonEnergy = 0.01
	}
	return &Solver{cfg: cfg}
}

// Solve mutates m by replacing each room's X/Y with settled integer cell
// coordinates. Z layers are solved independently and Z is preserved.
func (s *Solver) Solve(m *Map) {
	if m == nil {
		return
	}
	layers := groupByZ(m)
	zs := make([]int, 0, len(layers))
	for z := range layers {
		zs = append(zs, z)
	}
	sort.Ints(zs)
	for _, z := range zs {
		s.solveLayer(m, layers[z])
	}
}

func groupByZ(m *Map) map[int][]string {
	out := map[int][]string{}
	for id, r := range m.Rooms {
		if r == nil {
			continue
		}
		out[r.Z] = append(out[r.Z], id)
	}
	for z := range out {
		sort.Strings(out[z])
	}
	return out
}

func (s *Solver) solveLayer(m *Map, ids []string) {
	if len(ids) <= 1 {
		return
	}

	type particle struct {
		id     string
		x, y   float64
		vx, vy float64
	}
	parts := make([]particle, len(ids))
	indexOf := make(map[string]int, len(ids))
	for i, id := range ids {
		r := m.Rooms[id]
		parts[i] = particle{id: id, x: float64(r.X), y: float64(r.Y)}
		indexOf[id] = i
	}

	type edge struct {
		u, v   int
		dx, dy float64
	}
	var edges []edge
	seen := make(map[[2]int]bool)
	for _, id := range ids {
		r := m.Rooms[id]
		dirs := sortedRoomDirs(r)
		for _, d := range dirs {
			otherID := r.Exits[d]
			otherIdx, ok := indexOf[otherID]
			if !ok {
				continue
			}
			uIdx := indexOf[id]
			if uIdx == otherIdx {
				continue
			}
			a, b := uIdx, otherIdx
			if a > b {
				a, b = b, a
			}
			key := [2]int{a, b}
			if seen[key] {
				continue
			}
			seen[key] = true

			dx, dy, _, ok := DirectionVector(d)
			if !ok || (dx == 0 && dy == 0) {
				continue
			}
			if uIdx == b {
				dx, dy = -dx, -dy
			}
			fdx, fdy := float64(dx), float64(dy)
			if dx != 0 && dy != 0 {
				inv := 1 / math.Sqrt2
				fdx *= inv
				fdy *= inv
			}
			edges = append(edges, edge{u: a, v: b, dx: fdx, dy: fdy})
		}
	}

	for iter := 0; iter < s.cfg.MaxIterations; iter++ {
		fx := make([]float64, len(parts))
		fy := make([]float64, len(parts))

		for _, e := range edges {
			tx := parts[e.u].x + e.dx*s.cfg.RestLength
			ty := parts[e.u].y + e.dy*s.cfg.RestLength
			ex := parts[e.v].x - tx
			ey := parts[e.v].y - ty
			fx[e.v] -= s.cfg.KSpring * ex
			fy[e.v] -= s.cfg.KSpring * ey
			fx[e.u] += s.cfg.KSpring * ex
			fy[e.u] += s.cfg.KSpring * ey
		}

		for i := 0; i < len(parts); i++ {
			for j := i + 1; j < len(parts); j++ {
				ddx := parts[i].x - parts[j].x
				ddy := parts[i].y - parts[j].y
				dist2 := ddx*ddx + ddy*ddy
				if dist2 < 0.01 {
					// Deterministic perturbation for exact overlaps.
					if parts[i].id < parts[j].id {
						ddx = -0.1
					} else {
						ddx = 0.1
					}
					ddy = 0.1
					dist2 = ddx*ddx + ddy*ddy
				}
				f := s.cfg.KRepel / dist2
				inv := f / math.Sqrt(dist2)
				fx[i] += ddx * inv
				fy[i] += ddy * inv
				fx[j] -= ddx * inv
				fy[j] -= ddy * inv
			}
		}

		kinetic := 0.0
		for i := range parts {
			parts[i].vx = (parts[i].vx + fx[i]) * s.cfg.Damping
			parts[i].vy = (parts[i].vy + fy[i]) * s.cfg.Damping
			parts[i].x += parts[i].vx
			parts[i].y += parts[i].vy
			kinetic += parts[i].vx*parts[i].vx + parts[i].vy*parts[i].vy
		}
		if kinetic < s.cfg.EpsilonEnergy {
			break
		}
	}

	type snap struct {
		id   string
		x, y int
	}
	snapped := make([]snap, len(parts))
	for i, p := range parts {
		snapped[i] = snap{id: p.id, x: int(math.Round(p.x)), y: int(math.Round(p.y))}
	}
	sort.Slice(snapped, func(i, j int) bool { return snapped[i].id < snapped[j].id })
	occupied := map[[2]int]bool{}
	for i, sn := range snapped {
		key := [2]int{sn.x, sn.y}
		if !occupied[key] {
			occupied[key] = true
			continue
		}
		nx, ny := nearestFree(sn.x, sn.y, occupied)
		snapped[i].x = nx
		snapped[i].y = ny
		occupied[[2]int{nx, ny}] = true
	}
	for _, sn := range snapped {
		r := m.Rooms[sn.id]
		r.X = sn.x
		r.Y = sn.y
	}
}

func sortedRoomDirs(r *Room) []Direction {
	dirs := make([]Direction, 0, len(r.Exits))
	for d := range r.Exits {
		dirs = append(dirs, d)
	}
	sort.Slice(dirs, func(i, j int) bool { return string(dirs[i]) < string(dirs[j]) })
	return dirs
}

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
