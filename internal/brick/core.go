package brick

import (
	"errors"
	"strings"
)

// ElementID is unique and represents a single brick for a given DesignID
type ElementID string

// DesignID represents a brick design for which there may be multiple ElementID (e.g. different colors or variations)
type DesignID string

// Core represents a Lego brick with only the core identifying information and general fields that are not specific.
type Core struct {
	IsCustom   bool        `json:"is_custom"`
	ElementIDs []ElementID `json:"ids"`
	ElementID  *ElementID  `json:"element_id"`
	DesignID   DesignID    `json:"design_id"`
	Name       string      `json:"name"`
	ImageURL   string      `json:"image_url"`
}

// TODO : bricklinkURL ? bricklinkColor ? in Core or Locale ?

// GetID returns the appropriate ElementID to use
func (c *Core) GetID() (ElementID, error) {
	// Determine the ID
	var keyID ElementID
	if c.ElementID != nil {
		keyID = *c.ElementID
	} else if len(c.ElementIDs) > 0 {
		// No main ID: return the first non-empty (after trimming) ID in the list
		for _, id := range c.ElementIDs {
			if strings.TrimSpace(string(id)) != "" {
				keyID = id
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

// SetElementIDs sets the ElementIDs slice
func (c *Core) SetElementIDs(ids []ElementID) {
	c.ElementIDs = ids
}

// Copy creates a copy of the Core struct
func (c *Core) Copy() Core {
	return *c
}
