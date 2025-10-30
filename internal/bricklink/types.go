package bricklink

import "time"

// SearchItem represents a single item from BrickLink search results
type SearchItem struct {
	IDItem          int     `json:"idItem"`
	TypeItem        string  `json:"typeItem"`
	StrItemNo       string  `json:"strItemNo"`
	StrItemName     string  `json:"strItemName"`
	IDColor         int     `json:"idColor"`
	IDColorImg      int     `json:"idColorImg"`
	CItemImgTypeS   string  `json:"cItemImgTypeS"`
	BHasLargeImg    bool    `json:"bHasLargeImg"`
	N4NewQty        int     `json:"n4NewQty"`
	N4NewSellerCnt  int     `json:"n4NewSellerCnt"`
	MNewMinPrice    string  `json:"mNewMinPrice"`
	MNewMaxPrice    string  `json:"mNewMaxPrice"`
	N4UsedQty       int     `json:"n4UsedQty"`
	N4UsedSellerCnt int     `json:"n4UsedSellerCnt"`
	MUsedMinPrice   string  `json:"mUsedMinPrice"`
	MUsedMaxPrice   string  `json:"mUsedMaxPrice"`
	StrCategory     string  `json:"strCategory"`
	StrPCC          *string `json:"strPCC"`
}

// InventoryItem represents a single item from the BrickLink inventory
type InventoryItem struct {
	ItemID      string `json:"item_id"`
	ItemNo      string `json:"item_no"`
	Quantity    string `json:"quantity"`
	Description string `json:"description"`
	Color       string `json:"color"`
	ColorCode   string `json:"color_code"`
	ImageURL    string `json:"image_url"`
	ItemType    string `json:"item_type"`
}

// Inventory represents the complete inventory for a set
type Inventory struct {
	SetNumber string          `json:"set_number"`
	Items     []InventoryItem `json:"items"`
	FetchedAt time.Time       `json:"fetched_at"`
}

type searchTypeList struct {
	Type  string       `json:"type"`
	Count int          `json:"count"`
	Items []SearchItem `json:"items"`
}

type searchResult struct {
	TypeList       []searchTypeList `json:"typeList"`
	NCustomItemCnt int              `json:"nCustomItemCnt"`
}

type searchResponse struct {
	Result        searchResult `json:"result"`
	ReturnCode    int          `json:"returnCode"`
	ReturnMessage string       `json:"returnMessage"`
	ErrorTicket   int          `json:"errorTicket"`
	ProcssingTime int          `json:"procssingTime"`
	StrRefNo      string       `json:"strRefNo"`
}
