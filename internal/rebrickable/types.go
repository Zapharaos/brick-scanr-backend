package rebrickable

import (
	"strings"
	"time"
)

// setPartsResponse is one page of GET /lego/sets/{set_num}/parts/.
type setPartsResponse struct {
	Count    int           `json:"count"`
	Next     string        `json:"next"`
	Previous string        `json:"previous"`
	Results  []setPartLine `json:"results"`
}

// setPartLine is a single inventory line: a part in a given color with a quantity.
type setPartLine struct {
	Part struct {
		PartNum    string `json:"part_num"`
		Name       string `json:"name"`
		PartImgURL string `json:"part_img_url"`
	} `json:"part"`
	Color struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"color"`
	Quantity int  `json:"quantity"`
	IsSpare  bool `json:"is_spare"`
	// ElementID is the LEGO element ID for this part+color. It may be empty when
	// Rebrickable has no mapping (older or unreleased elements).
	ElementID string `json:"element_id"`
}

// InventoryItem represents a single line of a set inventory as provided by
// Rebrickable. Unlike the legacy BrickLink scraper, each line already carries a
// single, unambiguous LEGO element ID (or none), so there is no "X or Y or Z" case.
type InventoryItem struct {
	Index       int
	DesignID    string // LEGO design ID (part_num)
	ElementID   string // LEGO element ID (may be empty)
	ColorID     int
	Quantity    int
	Description string
	ImageURL    string
	IsSpare     bool
}

// customPrefixes are ItemNo prefixes that mark non-catalog / custom items which
// cannot be priced through Pick-a-Brick. Mirrors the legacy BrickLink behavior.
var customPrefixes = []string{"gen", "idea"}

// IsCustom reports whether the item is a custom/unavailable part: one whose design
// ID carries a custom prefix, or one with no usable LEGO element ID.
func (ii InventoryItem) IsCustom() bool {
	for _, p := range customPrefixes {
		if strings.HasPrefix(ii.DesignID, p) {
			return true
		}
	}
	return strings.TrimSpace(ii.ElementID) == ""
}

// Inventory is the complete inventory for a set. Rebrickable set-parts does not
// distinguish alternates/counterparts, so only regular and spare (extra) items are
// populated; the pricing pipeline processes RegularItems.
type Inventory struct {
	SetNumber    string
	RegularItems []InventoryItem
	ExtraItems   []InventoryItem
	FetchedAt    time.Time
}
