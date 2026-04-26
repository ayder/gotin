package input

import (
	"testing"

	"github.com/ayder/gotin/internal/command"
)

func TestCmdMap_Show(t *testing.T) {
	h := NewHandler()
	res := h.HandleInput("/map show")
	if len(res) != 1 {
		t.Fatalf("expected 1 result, got %d", len(res))
	}
	if _, ok := res[0].Command.(*command.MapShow); !ok {
		t.Errorf("expected *command.MapShow, got %T", res[0].Command)
	}
}
