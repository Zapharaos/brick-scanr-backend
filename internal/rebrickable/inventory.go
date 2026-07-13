package rebrickable

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"go.uber.org/zap"
)

// maxInventoryPages caps pagination to guard against a runaway loop if the API
// keeps returning a "next" link. A page holds up to 1000 lines, so this covers
// any real set with a wide margin.
const maxInventoryPages = 20

// FetchInventory fetches a set's inventory from Rebrickable
// (GET /lego/sets/{set_num}/parts/), following pagination.
//
// Minifigs are kept as whole items (inc_minifig_parts=0) to stay close to the
// behavior of the previous BrickLink inventory source.
func (c *Client) FetchInventory(setNumber string) (*Inventory, error) {
	if c.apiKey == "" {
		return nil, ErrMissingAPIKey
	}

	inventory := &Inventory{
		SetNumber:    setNumber,
		RegularItems: []InventoryItem{},
		ExtraItems:   []InventoryItem{},
		FetchedAt:    time.Now(),
	}

	// First page URL. Subsequent pages come from the "next" link in the response.
	params := url.Values{}
	params.Set("page_size", "1000")
	params.Set("inc_minifig_parts", "0")
	nextURL := fmt.Sprintf("%s/lego/sets/%s/parts/?%s",
		c.apiBaseURL, url.PathEscape(setNumber), params.Encode())

	itemIndex := 0
	for page := 0; nextURL != ""; page++ {
		if page >= maxInventoryPages {
			return nil, fmt.Errorf("inventory exceeded %d pages for set %s", maxInventoryPages, setNumber)
		}

		zap.L().Debug("Fetching Rebrickable inventory page", zap.String("url", nextURL))
		body, status, err := c.getJSON(nextURL)
		if err != nil {
			return nil, err
		}
		if status == http.StatusNotFound {
			return nil, ErrInventoryNotFound
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("unexpected status code: %d: %s", status, string(body))
		}

		var resp setPartsResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("failed to parse JSON response: %w", err)
		}

		itemIndex = appendPage(inventory, resp.Results, itemIndex)
		nextURL = resp.Next
	}

	if len(inventory.RegularItems) == 0 && len(inventory.ExtraItems) == 0 {
		return nil, ErrInventoryNotFound
	}

	zap.L().Info("Parsed Rebrickable inventory",
		zap.String("set_number", setNumber),
		zap.Int("regular_items", len(inventory.RegularItems)),
		zap.Int("extra_items", len(inventory.ExtraItems)))

	return inventory, nil
}

// appendPage maps one page of Rebrickable results into the inventory, assigning a
// running global index. It is exported-internal (lowercase) but kept independent of
// the HTTP layer so it can be unit-tested against fixtures. Returns the next index.
func appendPage(inv *Inventory, results []setPartLine, startIndex int) int {
	idx := startIndex
	for _, line := range results {
		if line.Part.PartNum == "" {
			continue
		}
		item := InventoryItem{
			Index:       idx,
			DesignID:    line.Part.PartNum,
			ElementID:   line.ElementID,
			ColorID:     line.Color.ID,
			Quantity:    line.Quantity,
			Description: line.Part.Name,
			ImageURL:    line.Part.PartImgURL,
			IsSpare:     line.IsSpare,
		}
		idx++

		if line.IsSpare {
			inv.ExtraItems = append(inv.ExtraItems, item)
		} else {
			inv.RegularItems = append(inv.RegularItems, item)
		}
	}
	return idx
}
