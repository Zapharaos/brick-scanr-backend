package brick

import (
	"context"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/pickabrick"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

type DesignStatus int

const (
	DesignStatusUnknown DesignStatus = iota
	DesignStatusMinimal
	DesignStatusComplete
)

type Design struct {
	Locale
	DesignStatus DesignStatus
	ElementIDs   []ElementID `json:"element_ids"` // Refers to brick:<elementID>:<xlocale>
	Alternates   []DesignID  `json:"alternates"`  // Refers to another Design struct
}

type DesignWithBricks struct {
	Design
	Bricks []Locale `json:"bricks"`
}

type DesignIndex map[DesignID]*DesignWithBricks

// Fetch attempts to fetch the design details and associated bricks for the given design ID and locale.
func (d *Design) Fetch(ctx context.Context, lang, xlocale language.Tag) ([]Locale, error) {
	// First, fetch the minimal design data (name, image, etc.) from BrickLink
	err := d.FetchMinimal(ctx, lang, xlocale)
	if err != nil {
		return nil, err
	}

	// Fetch the bricks for this design ID
	return d.FetchBricks(ctx, lang, xlocale)
}

// FetchMinimal fetches the minimal design details (name, image, etc.) for the given design ID and locale from BrickLink.
func (d *Design) FetchMinimal(ctx context.Context, lang, xlocale language.Tag) error {
	// Query BrickLink for brick details
	bricklinkBrick, err := bricklink.C().FetchBrickDetails(string(d.ID.DesignID), lang)
	if err != nil {
		zap.L().Error("Failed to fetch brick details from BrickLink",
			zap.Error(err),
			zap.String("design_id", string(d.ID.DesignID)),
		)
		return err
	}

	// Map BrickLink brick details to internal representation
	bCore := NewCoreFromBricklinkBrick(bricklinkBrick)

	d.Core = bCore
	d.DesignStatus = DesignStatusMinimal

	// Cache the data
	if err = RedisSetDesign(ctx, *d, xlocale, true); err != nil {
		zap.L().Error("Failed to cache design in Redis",
			zap.Error(err),
			zap.String("design_id", string(d.ID.DesignID)),
		)
		// Not a critical error, we can still return the data without caching
	}

	return nil
}

// FetchBricks fetches the bricks associated with this design ID from the pick-a-brick API, updates the design with the element IDs, and caches the data.
func (d *Design) FetchBricks(ctx context.Context, lang, xlocale language.Tag) ([]Locale, error) {
	// Fetch all bricks matching this design ID
	pabBricks, err := pickabrick.C().FetchBricksByDesignID(string(d.ID.DesignID), lang, xlocale)
	if err != nil {
		zap.L().Error("Failed to fetch bricks by design ID from pick-a-brick API",
			zap.Error(err),
			zap.String("design_id", string(d.ID.DesignID)),
		)
		return nil, err
	}

	var elementIDs []ElementID
	var bricks []Locale

	// Process the bricks to update their data and cache them
	for _, pab := range pabBricks {

		// Map result to local representation
		mappedB := MapLocaleFromPickabrick(Locale{}, pab, xlocale)

		// Load preferred data into new instance (default to pick-a-brick data, but if cache has valid price that is lower, use that instead)
		bLocale, valid, notfound := mappedB.LoadFromRedis(ctx, mappedB.ID.ElementID, xlocale, false, true)
		if !valid && !notfound {
			// Not found in cache, cache the brick details in Redis for future searches and lookups
			err = RedisSetLocale(ctx, bLocale, xlocale, true)
			if err != nil {
				zap.L().Error("Failed to cache brick in Redis",
					zap.Error(err),
					zap.String("element_id", string(mappedB.ID.ElementID)),
				)
				// Not a critical error, we can still return the data without caching
			}
		}

		// Add to element IDs for back tracking through design ID
		elementIDs = append(elementIDs, ElementID(pab.ID))

		// Add to design ID bricks for client response
		bricks = append(bricks, bLocale)
	}

	// Update the design with the element IDs and mark it as complete
	d.ElementIDs = elementIDs
	d.DesignStatus = DesignStatusComplete

	// Cache the data
	if err = RedisSetDesign(ctx, *d, xlocale, true); err != nil {
		zap.L().Error("Failed to cache design in Redis",
			zap.Error(err),
			zap.String("design_id", string(d.ID.DesignID)),
		)
		// Not a critical error, we can still return the data without caching
	}

	return bricks, nil
}
