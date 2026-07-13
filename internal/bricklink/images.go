package bricklink

import "fmt"

// partImageURLFormat is the public BrickLink catalog image for a part item number
// (the "PL" large part image, independent of color). Verified to resolve for
// printed parts that are absent from Rebrickable.
const partImageURLFormat = "https://img.bricklink.com/ItemImage/PL/%s.png"

// PartImageURL returns the public BrickLink catalog image URL for a part item number.
func PartImageURL(itemNo string) string {
	return fmt.Sprintf(partImageURLFormat, itemNo)
}
