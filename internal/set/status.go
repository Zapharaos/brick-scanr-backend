package set

import (
	"github.com/Zapharaos/brick-scanr-backend/internal/lego"
	"github.com/Zapharaos/brick-scanr-backend/internal/pickabrick"
)

type Status int

const (
	StatusUnknown Status = iota
	StatusRetired
	StatusOutOfStock
	StatusBackorder
	StatusAvailable
)

func MapLegoProductStatus(p lego.Product) Status {
	switch p.Variant.Attributes.AvailabilityStatus {
	case lego.EAvailable:
		return StatusAvailable
	case lego.HOutOfStock:
		return StatusOutOfStock
	case lego.FBackorderForDate:
		return StatusBackorder
	case lego.RRetired:
	default:
		break
	}
	return StatusRetired
}

func MapPickabrickStatus(s string) Status {
	switch s {
	case pickabrick.Available:
		return StatusAvailable
	case pickabrick.OutOfStock:
		return StatusOutOfStock
	default:
		break
	}
	return StatusRetired
}
