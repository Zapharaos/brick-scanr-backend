package brick

// Inventory represents the inventory information of a brick Core.
type Inventory struct {
	Index    int `json:"index"`
	Quantity int `json:"quantity"`
	// ColorID is the Rebrickable color ID of this inventory line. It enables the
	// part+color element fallback when the primary element ID is not purchasable.
	// Note: 0 is a valid color (Black); entries cached before this field existed
	// also unmarshal to 0, in which case a mismatched fallback query simply 404s.
	ColorID int `json:"color_id"`
}
