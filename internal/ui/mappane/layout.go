package mappane

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/ayder/gotin/internal/mapper"
	"github.com/ayder/nelib"
)

// Adapter owns only session-local display labels. UUIDs/vnums, discovery
// coordinates, persistence and undo remain entirely in mapper.Engine.
// Prepare runs on the UI thread; Draw runs on its copied request in a tea.Cmd.
type Adapter struct{ labels map[string]nelib.RoomID }

type Request struct {
	Scene  nelib.Scene
	Layer  string
	Anchor string
	Key    [32]byte
}

type Frame struct {
	Drawing nelib.SceneDrawing
	Anchor  string
}

func (a *Adapter) Prepare(m *mapper.Map, current, layerKey string) (Request, error) {
	if m == nil || m.Rooms[current] == nil {
		return Request{}, fmt.Errorf("current room missing from map")
	}
	if a.labels == nil {
		a.labels = make(map[string]nelib.RoomID)
	}
	ids := make([]string, 0, len(m.Rooms))
	for id, r := range m.Rooms {
		if r != nil {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	layers := layerMembership(m)
	anchor := current
	if m.Rooms[layerKey] != nil && layers[layerKey] != layers[current] {
		anchor = layerKey
	}
	scene := nelib.Scene{Rooms: make(map[string]nelib.SceneRoom, len(ids)), Current: current}
	for _, id := range ids {
		if a.labels[id] == "" {
			a.labels[id] = nelib.RoomID(strconv.Itoa(len(a.labels) + 1))
		}
		r := m.Rooms[id]
		node := nelib.SceneRoom{Label: a.labels[id], Layer: layers[id], Exits: make(map[nelib.Direction]string), Portals: make(map[string]string)}
		for dir, to := range r.Exits {
			if m.Rooms[to] == nil {
				return Request{}, fmt.Errorf("exit %s from %s has no destination room", dir, id)
			}
			if d, err := nelib.ParseDirection(string(dir)); err == nil {
				node.Exits[d] = to
			} else {
				node.Portals[string(dir)] = to
			}
		}
		scene.Rooms[id] = node
	}
	req := Request{Scene: scene, Layer: layers[anchor], Anchor: anchor}
	// Names/descriptions/recognition coordinates do not affect the drawing.
	// Anchor is included so remote-layer recentering cannot reuse a wrong frame.
	encoded, err := json.Marshal(struct {
		Scene         nelib.Scene
		Layer, Anchor string
	}{scene, req.Layer, anchor})
	if err != nil {
		return Request{}, err
	}
	req.Key = sha256.Sum256(encoded)
	return req, nil
}

func Draw(ctx context.Context, req Request) (*Frame, error) {
	drawing, err := nelib.DrawScene(ctx, req.Scene, nelib.SceneOptions{
		Layer:  req.Layer,
		Solver: nelib.NewNativeSolver(nelib.SolverOptions{Timeout: 2 * time.Second, OptimizationTimeout: 30 * time.Millisecond}),
		Render: nelib.RenderOptions{MaxCells: 200_000},
	})
	if err != nil {
		return nil, err
	}
	return &Frame{Drawing: drawing, Anchor: req.Anchor}, nil
}
