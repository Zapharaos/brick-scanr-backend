package brick

import "github.com/Zapharaos/brick-scanr-backend/internal/pickabrick"

// Color represents a color of a brick.
type Color struct {
	Name        string `json:"name"`
	Key         string `json:"key"`
	Hex         string `json:"hex"`
	ContrastHex string `json:"contrast_hex"`
	FamilyName  string `json:"family_name"`
	FamilyKey   string `json:"family_key"`
}

// MapColorFromPickabrick maps a pickabrick.Brick to a Color struct
func MapColorFromPickabrick(pab pickabrick.Brick) Color {
	color := Color{
		Hex:         pab.ColorHex,
		ContrastHex: pab.ContrastColorHex,
	}

	if pab.Facets != nil {
		if pab.Facets.Color != nil {
			color.Name = pab.Facets.Color.Name
			color.Key = pab.Facets.Color.Key
		}
		if pab.Facets.ColorFamily != nil {
			color.FamilyName = pab.Facets.ColorFamily.Name
			color.FamilyKey = pab.Facets.ColorFamily.Key
		}
	}

	return color
}
