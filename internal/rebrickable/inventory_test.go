package rebrickable

import "testing"

// samplePage mirrors one page of GET /lego/sets/{set_num}/parts/.
const samplePage = `{
  "count": 4,
  "next": "",
  "previous": null,
  "results": [
    {
      "part": {"part_num": "3001", "name": "Brick 2 x 4", "part_img_url": "https://cdn.rebrickable.com/media/parts/3001.jpg"},
      "color": {"id": 15, "name": "White"},
      "quantity": 4,
      "is_spare": false,
      "element_id": "300115"
    },
    {
      "part": {"part_num": "3002", "name": "Brick 2 x 3", "part_img_url": ""},
      "color": {"id": 4, "name": "Red"},
      "quantity": 1,
      "is_spare": true,
      "element_id": "300204"
    },
    {
      "part": {"part_num": "3003", "name": "Brick 2 x 2", "part_img_url": ""},
      "color": {"id": 0, "name": "Black"},
      "quantity": 2,
      "is_spare": false,
      "element_id": ""
    },
    {
      "part": {"part_num": "gen001", "name": "Custom sticker", "part_img_url": ""},
      "color": {"id": 9999, "name": "N/A"},
      "quantity": 1,
      "is_spare": false,
      "element_id": "999999"
    }
  ]
}`

func TestAppendPage(t *testing.T) {
	var resp setPartsResponse
	mustUnmarshal(t, samplePage, &resp)

	inv := &Inventory{}
	next := appendPage(inv, resp.Results, 0)

	if next != 4 {
		t.Errorf("next index = %d, want 4", next)
	}
	if len(inv.RegularItems) != 3 {
		t.Fatalf("RegularItems = %d, want 3", len(inv.RegularItems))
	}
	if len(inv.ExtraItems) != 1 {
		t.Fatalf("ExtraItems = %d, want 1", len(inv.ExtraItems))
	}

	first := inv.RegularItems[0]
	if first.DesignID != "3001" || first.ElementID != "300115" || first.ColorID != 15 || first.Quantity != 4 {
		t.Errorf("first regular item mismatch: %+v", first)
	}
	if first.ImageURL != "https://cdn.rebrickable.com/media/parts/3001.jpg" {
		t.Errorf("ImageURL = %q", first.ImageURL)
	}

	if inv.ExtraItems[0].DesignID != "3002" || inv.ExtraItems[0].Quantity != 1 {
		t.Errorf("extra item mismatch: %+v", inv.ExtraItems[0])
	}
}

func TestInventoryItemIsCustom(t *testing.T) {
	cases := []struct {
		name string
		item InventoryItem
		want bool
	}{
		{"normal", InventoryItem{DesignID: "3001", ElementID: "300115"}, false},
		{"no element id", InventoryItem{DesignID: "3003", ElementID: ""}, true},
		{"blank element id", InventoryItem{DesignID: "3003", ElementID: "   "}, true},
		{"gen prefix", InventoryItem{DesignID: "gen001", ElementID: "999999"}, true},
		{"idea prefix", InventoryItem{DesignID: "idea42", ElementID: "111111"}, true},
	}
	for _, tc := range cases {
		if got := tc.item.IsCustom(); got != tc.want {
			t.Errorf("%s: IsCustom() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestAppendPageGlobalIndexAcrossPages(t *testing.T) {
	var resp setPartsResponse
	mustUnmarshal(t, samplePage, &resp)

	inv := &Inventory{}
	next := appendPage(inv, resp.Results, 10)
	if next != 14 {
		t.Errorf("next index = %d, want 14 (continuing from 10)", next)
	}
	if inv.RegularItems[0].Index != 10 {
		t.Errorf("first item index = %d, want 10", inv.RegularItems[0].Index)
	}
}
