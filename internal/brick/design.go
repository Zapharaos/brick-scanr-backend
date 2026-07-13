package brick

import (
	"context"
	"errors"

	"github.com/Zapharaos/brick-scanr-backend/internal/pickabrick"
	"github.com/Zapharaos/brick-scanr-backend/internal/rebrickable"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

type DesignStatus int

const (
	DesignStatusUnknown DesignStatus = iota
	DesignStatusMinimal
	DesignStatusBricks
	DesignStatusBricksNotFound
	DesignStatusComplete
)

type Design struct {
	Locale
	DesignStatus DesignStatus
	ElementIDs   []ElementID `json:"element_ids"` // Refers to brick:<elementID>:<locale>
	Alternates   []DesignID  `json:"alternates"`  // Refers to another Design struct
}

type DesignWithBricks struct {
	Design
	Bricks []Locale `json:"bricks"`
}

type DesignIndex map[DesignID]*DesignWithBricks

// Fetch attempts to fetch the design details and associated bricks for the given design ID and locale.
func (d *Design) Fetch(ctx context.Context, locale language.Tag) ([]Locale, error) {
	// First, fetch the minimal design data (name, image, etc.) from BrickLink
	err := d.FetchMinimal(ctx, locale)
	if err != nil {
		return nil, err
	}

	// Fetch the bricks for this design ID
	return d.FetchBricks(ctx, locale)
}

// FetchMinimal fetches the minimal design details (name, image, alternate molds) for
// the given design ID from Rebrickable. Design IDs are Rebrickable part numbers since
// the inventory source moved to Rebrickable, so its part endpoint is the authoritative
// (and structured) source — the previous BrickLink HTML scraping is gone.
// The locale parameter is kept for signature stability; Rebrickable names are English,
// localized display data comes from Pick-a-Brick via FetchBricks.
func (d *Design) FetchMinimal(ctx context.Context, locale language.Tag) error {
	// Query Rebrickable for part details
	part, err := rebrickable.C().FetchPartDetails(string(d.ID.DesignID))
	if err != nil && !errors.Is(err, rebrickable.ErrPartNotFound) {
		zap.L().Error("Failed to fetch part details from Rebrickable",
			zap.Error(err),
			zap.String("design_id", string(d.ID.DesignID)),
		)
		return err
	}

	// The part was not found on Rebrickable
	if errors.Is(err, rebrickable.ErrPartNotFound) {
		// Keep the design as-is; FetchBricks may still resolve it through Pick-a-Brick

	} else {

		// Part was found, populate the design details
		id := ID{DesignID: d.ID.DesignID}
		core := Core{
			ID:       &id,
			IDs:      []ID{id},
			Name:     part.Name,
			ImageURL: part.PartImgURL,
		}
		// Molds are physical variations of the same part: the closest equivalent to
		// the alternate item numbers previously scraped from BrickLink.
		for _, mold := range part.Molds {
			if mold != "" && mold != string(d.ID.DesignID) {
				core.IDs = append(core.IDs, ID{DesignID: DesignID(mold)})
			}
		}

		d.Core = core
		d.DesignStatus = DesignStatusMinimal
	}

	// Populate alternates
	d.Alternates = []DesignID{}
	for _, id := range d.Core.IDs {
		if id.DesignID != d.ID.DesignID {
			d.Alternates = append(d.Alternates, id.DesignID)
		}
	}

	// Cache the data
	if err = RedisSetDesign(ctx, *d, locale, true); err != nil {
		zap.L().Error("Failed to cache design in Redis",
			zap.Error(err),
			zap.String("design_id", string(d.ID.DesignID)),
		)
		// Not a critical error, we can still return the data without caching
	}

	return nil
}

// PrefetchDesignBricks fetches all bricks (all colors) for a design ID from the
// Pick-a-Brick API in a single request and caches each element's locale in Redis.
//
// It is used to warm the cache before per-brick price resolution: many bricks in a
// set share the same design (different colors), so grouping by design turns N
// per-element requests into one request per unique design. Subsequent per-element
// LoadFromRedis lookups then resolve from cache instead of hitting the API.
//
// It returns the number of elements returned by the API (0 on not-found), which is
// useful for measuring request savings. A not-found design is not an error.
func PrefetchDesignBricks(ctx context.Context, designID DesignID, locale language.Tag) (int, error) {
	pabBricks, err := pickabrick.C().FetchBricksByDesignID(string(designID), locale)
	if err != nil {
		// Not-found is expected for designs absent from Pick-a-Brick: skip silently.
		if errors.Is(err, pickabrick.ErrBrickNotFound) {
			return 0, nil
		}
		return 0, err
	}

	for _, pab := range pabBricks {
		// Map result to local representation
		mappedB := MapLocaleFromPickabrick(Locale{}, pab, locale)

		// Preserve an existing valid/not-found cache entry (which may hold a lower
		// price) rather than overwriting it. Mirrors Design.FetchBricks semantics.
		if _, valid, notfound := mappedB.LoadFromRedis(ctx, mappedB.ID.ElementID, locale, false, true); valid || notfound {
			continue
		}

		if cacheErr := RedisSetLocale(ctx, mappedB, locale, true); cacheErr != nil {
			zap.L().Warn("Failed to cache prefetched design brick",
				zap.Error(cacheErr),
				zap.String("element_id", string(mappedB.ID.ElementID)),
				zap.String("design_id", string(designID)),
			)
			// Not fatal, continue caching the rest
		}
	}

	return len(pabBricks), nil
}

// FetchBricks fetches the bricks associated with this design ID from the pick-a-brick API, updates the design with the element IDs, and caches the data.
func (d *Design) FetchBricks(ctx context.Context, locale language.Tag) ([]Locale, error) {
	// Fetch all bricks matching this design ID
	pabBricks, err := pickabrick.C().FetchBricksByDesignID(string(d.ID.DesignID), locale)
	if err != nil && !errors.Is(err, pickabrick.ErrBrickNotFound) {
		zap.L().Error("Failed to fetch bricks by design ID from pick-a-brick API",
			zap.Error(err),
			zap.String("design_id", string(d.ID.DesignID)),
		)
		return nil, err
	}

	// If no bricks found, mark design as not found and cache that result to prevent future lookups
	if errors.Is(err, pickabrick.ErrBrickNotFound) {
		d.DesignStatus = DesignStatusBricksNotFound
		// Cache the data
		if err = RedisSetDesign(ctx, *d, locale, true); err != nil {
			zap.L().Error("Failed to cache design in Redis",
				zap.Error(err),
				zap.String("design_id", string(d.ID.DesignID)),
			)
			// Not a critical error, we can still return the data without caching
		}
		return []Locale{}, nil
	}

	var elementIDs []ElementID
	var bricks []Locale

	// Process the bricks to update their data and cache them
	for _, pab := range pabBricks {

		// Map result to local representation
		mappedB := MapLocaleFromPickabrick(Locale{}, pab, locale)

		// Apply design IDS to brick locale
		mappedB.IDs = d.IDs

		// Load preferred data into new instance (default to pick-a-brick data, but if cache has valid price that is lower, use that instead)
		bLocale, valid, notfound := mappedB.LoadFromRedis(ctx, mappedB.ID.ElementID, locale, false, true)
		if !valid && !notfound {

			// Apply design IDS to brick locale
			bLocale.IDs = d.IDs

			// Not found in cache, cache the brick details in Redis for future searches and lookups
			err = RedisSetLocale(ctx, bLocale, locale, true)
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
	if d.DesignStatus == DesignStatusMinimal {
		d.DesignStatus = DesignStatusComplete
	} else {
		d.DesignStatus = DesignStatusBricks
	}

	// Cache the data
	if err = RedisSetDesign(ctx, *d, locale, true); err != nil {
		zap.L().Error("Failed to cache design in Redis",
			zap.Error(err),
			zap.String("design_id", string(d.ID.DesignID)),
		)
		// Not a critical error, we can still return the data without caching
	}

	return bricks, nil
}
