package brick

import (
	"strings"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
)

// GetIDsFromBricklinkSearchItem extracts the ElementID and DesignID from a Bricklink SearchItem
func GetIDsFromBricklinkSearchItem(bsi bricklink.SearchItem) (ElementID, DesignID) {
	elementID := GetElementIDFromBricklinkSearchItem(bsi)
	designID := GetDesignIDFromBricklinkSearchItem(bsi)
	return elementID, designID
}

// GetDesignIDFromBricklinkSearchItem extracts the DesignID from a Bricklink SearchItem
func GetDesignIDFromBricklinkSearchItem(bsi bricklink.SearchItem) DesignID {
	// A : "strItemNo": "4073", "strPCC": null
	// strItemNo is the Design ID, we have no element ID
	return DesignID(bsi.StrItemNo)
}

// GetElementIDFromBricklinkSearchItem extracts the ElementID from a Bricklink SearchItem, if available
func GetElementIDFromBricklinkSearchItem(bsi bricklink.SearchItem) ElementID {
	var elementID ElementID
	if bsi.StrPCC != nil {
		// Extract the numeric part before the parentheses

		// B : "strItemNo": "2780", "strPCC": "278026(11)"
		// strItemNo is the Design ID, parse strPCC to get the element ID
		// we could get the color code as well from the parentheses, but we don't need it for now

		pccParts := strings.Split(*bsi.StrPCC, "(")
		if len(pccParts) > 0 {
			elementID = ElementID(pccParts[0])
		}
	}

	return elementID
}
