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
	ItemIDs     []string `json:"item_ids"`
	ItemNo      string   `json:"item_no"`
	Quantity    string   `json:"quantity"`
	Description string   `json:"description"`
	Color       string   `json:"color"`
	ImageURL    string   `json:"image_url"`
}

// HasUniqueItemID checks if the inventory item has exactly one associated Item ID
func (ii *InventoryItem) HasUniqueItemID() bool {
	return len(ii.ItemIDs) == 1
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

// setDetailsResponse represents the response structure from BrickLink's getItemImageList API
type setDetailsResponse struct {
	Item          Set    `json:"item"`
	ReturnCode    int    `json:"returnCode"`
	ReturnMessage string `json:"returnMessage"`
	ErrorTicket   int    `json:"errorTicket"`
	ProcssingTime int    `json:"procssingTime"`
	StrRefNo      string `json:"strRefNo"`
}

type Set struct {
	TypeItem       string    `json:"typeItem"`
	StrItemName    string    `json:"strItemName"`
	StrItemNo      string    `json:"strItemNo"`
	N1Seq          int       `json:"n1Seq"`
	StrItemNoFull  string    `json:"strItemNoFull"`
	NYearReleased  int       `json:"nYearReleased"`
	NInvSetCnt     int       `json:"nInvSetCnt"`
	NInvPartCnt    int       `json:"nInvPartCnt"`
	NInvMinifigCnt int       `json:"nInvMinifigCnt"`
	NInvBookCnt    int       `json:"nInvBookCnt"`
	NInvGearCnt    int       `json:"nInvGearCnt"`
	ImageList      ImageList `json:"imglist"`
	HasLegacyLarge bool      `json:"hasLegacyLarge"`
}

type ImageType string

const (
	ImageTypeMain ImageType = "M"
)

type Image struct {
	Type       ImageType `json:"type"`
	Thumb1Url  string    `json:"thumb1_url"`
	Thumb2Url  string    `json:"thumb2_url"`
	MainUrl    string    `json:"main_url"`
	IdColorImg int       `json:"idColorImg"`
	TypeItem   string    `json:"typeItem"`
}

type ImageList []Image

func (il ImageList) GetMainImageURL() string {
	for _, img := range il {
		if img.Type == ImageTypeMain {
			return img.MainUrl
		}
	}
	return ""
}
