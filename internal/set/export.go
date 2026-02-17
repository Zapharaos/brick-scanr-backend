package set

import (
	"github.com/Zapharaos/brick-scanr-backend/internal/utils"
	"github.com/Zapharaos/go-spit"
	"github.com/Zapharaos/lingo"
	"golang.org/x/text/language"
)

// exportGetTableColumns returns the columns for the export table, with translated labels
func exportGetTableColumns(localizer interface{}) spit.Columns {
	var columns spit.Columns

	// Basic columns
	labelIndex := lingo.MustTranslate(localizer, lingo.NewMessage("set.export.columns.index"))
	labelStatus := lingo.MustTranslate(localizer, lingo.NewMessage("status.label"))
	labelColor := lingo.MustTranslate(localizer, lingo.NewMessage("set.export.columns.color"))
	labelName := lingo.MustTranslate(localizer, lingo.NewMessage("set.export.columns.name"))
	labelDesign := lingo.MustTranslate(localizer, lingo.NewMessage("set.export.columns.design"))
	labelElement := lingo.MustTranslate(localizer, lingo.NewMessage("set.export.columns.element"))
	labelQuantity := lingo.MustTranslate(localizer, lingo.NewMessage("set.export.columns.quantity"))
	labelUnitPrice := lingo.MustTranslate(localizer, lingo.NewMessage("set.export.columns.price-unit"))
	labelTotalPrice := lingo.MustTranslate(localizer, lingo.NewMessage("set.export.columns.price-total"))
	labelURL := lingo.MustTranslate(localizer, lingo.NewMessage("set.export.columns.url"))

	columns = append(columns,
		spit.NewColumn("index", labelIndex),
		spit.NewColumn("status", labelStatus),
		spit.NewColumn("color", labelColor),
		spit.NewColumn("name", labelName),
		spit.NewColumn("element", labelElement),
		spit.NewColumn("design", labelDesign),
		spit.NewColumn("quantity", labelQuantity),
		spit.NewColumn("unit-price", labelUnitPrice),
		spit.NewColumn("total-price", labelTotalPrice),
		spit.NewColumn("url", labelURL),
	)

	return columns
}

// ExportBuildTable builds a spit table from a set
func ExportBuildTable(set Locale, localizer interface{}, tag language.Tag) *spit.Table {

	var data spit.DataSlice
	for _, b := range set.Bricks {
		rowData := make(map[string]interface{})

		// Map the status enum value to a translation key
		var statusKey string
		switch b.Status {
		case utils.StatusOutOfStock:
			statusKey = "status.out-of-stock"
			break
		case utils.StatusAvailable:
			statusKey = "status.available"
			break
		default:
			statusKey = "status.unknown"
			break
		}

		rowData["index"] = b.Index
		rowData["status"] = lingo.MustTranslate(localizer, lingo.NewMessage(statusKey))
		rowData["color"] = b.Color.Name
		rowData["name"] = b.Name
		if b.ID != nil {
			rowData["element"] = b.ID.ElementID
		}
		rowData["design"] = b.ID.DesignID
		rowData["quantity"] = b.Quantity
		rowData["unit-price"] = b.Price.Formatted(tag)
		rowData["total-price"] = b.TotalPrice.Formatted(tag)
		rowData["url"] = b.PickabrickURL

		data = append(data, rowData)
	}

	columns := exportGetTableColumns(localizer)
	return &spit.Table{
		Data:    data,
		Columns: columns,
	}
}
