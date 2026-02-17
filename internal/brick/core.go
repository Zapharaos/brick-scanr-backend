package brick

import (
	"errors"
	"strings"
)

// ElementID is unique and represents a single brick for a given DesignID
type ElementID string

// DesignID represents a brick design for which there may be multiple ElementID (e.g. different colors or variations)
type DesignID string

type ID struct {
	ElementID ElementID `json:"element_id"`
	DesignID  DesignID  `json:"design_id"`
}

// Core represents a Lego brick with only the core identifying information and general fields that are not specific.
type Core struct {
	IsCustom bool   `json:"is_custom"`
	ID       *ID    `json:"id"`
	IDs      []ID   `json:"ids"`
	Name     string `json:"name"`
	ImageURL string `json:"image_url"`
}

// TODO : bricklinkURL ? bricklinkColor ? in Core or Locale ?

// GetElementID returns the appropriate ElementID to use
func (c *Core) GetElementID() (ElementID, error) {
	// Determine the ID
	var keyID ElementID
	if c.ID != nil {
		keyID = c.ID.ElementID
	} else if len(c.IDs) > 0 {
		// No main ID: return the first non-empty (after trimming) ID in the list
		for _, id := range c.IDs {
			if strings.TrimSpace(string(id.ElementID)) != "" {
				keyID = id.ElementID
				break
			}
		}
		// If no valid ID found in the slice, fall through to the error
	}
	if keyID == "" {
		// No ElementIDs at all - this shouldn't happen, but handle gracefully
		return "", errors.New("brick has no valid ID")
	}
	return keyID, nil
}

// SetIDs sets the ElementIDs slice
func (c *Core) SetIDs(ids []ID) {
	c.IDs = ids
}

// Copy creates a copy of the Core struct
func (c *Core) Copy() Core {
	return *c
}
