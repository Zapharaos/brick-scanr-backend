package setruntime

import (
	"testing"

	"github.com/Zapharaos/brick-scanr-backend/internal/brick"
	"github.com/Zapharaos/brick-scanr-backend/internal/set"
)

// newTestBrick builds a set.Brick carrying the given design/element pair the same way
// the Rebrickable mapper does (single ID, main ID mirrors it).
func newTestBrick(designID, elementID string) set.Brick {
	id := brick.ID{
		DesignID:  brick.DesignID(designID),
		ElementID: brick.ElementID(elementID),
	}
	core := brick.Core{ID: &id}
	if elementID != "" {
		core.IDs = []brick.ID{id}
	} else {
		core.IDs = []brick.ID{}
	}
	return set.NewBrick(brick.Inventory{Quantity: 1}, brick.Locale{Core: core})
}

func TestGroupBricksByDesign(t *testing.T) {
	bricks := []set.Brick{
		newTestBrick("3001", "300126"), // design A
		newTestBrick("3001", "300115"), // design A, other color
		newTestBrick("3002", "300226"), // design B
		newTestBrick("3957a", ""),      // no element ID -> design from main ID
		newTestBrick("", ""),           // nothing -> individual no-design job
	}

	jobs := groupBricksByDesign(bricks)

	byDesign := make(map[brick.DesignID]int)
	noDesignJobs := 0
	totalBricks := 0
	for _, j := range jobs {
		totalBricks += len(j.bricks)
		if j.designID == "" {
			noDesignJobs++
			if len(j.bricks) != 1 {
				t.Errorf("no-design job should hold exactly 1 brick, got %d", len(j.bricks))
			}
			continue
		}
		byDesign[j.designID] = len(j.bricks)
	}

	if totalBricks != len(bricks) {
		t.Errorf("jobs cover %d bricks, want %d (no brick lost or duplicated)", totalBricks, len(bricks))
	}
	if byDesign["3001"] != 2 {
		t.Errorf("design 3001 group = %d bricks, want 2", byDesign["3001"])
	}
	if byDesign["3002"] != 1 {
		t.Errorf("design 3002 group = %d bricks, want 1", byDesign["3002"])
	}
	if byDesign["3957a"] != 1 {
		t.Errorf("design 3957a (from main ID fallback) = %d bricks, want 1", byDesign["3957a"])
	}
	if noDesignJobs != 1 {
		t.Errorf("no-design jobs = %d, want 1", noDesignJobs)
	}
}
