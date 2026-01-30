package set

import (
	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/Zapharaos/brick-scanr-backend/internal/lego"
	"github.com/google/uuid"
	"golang.org/x/text/language"
)

type FetchStatus int

const (
	FetchStatusPending FetchStatus = iota
	FetchStatusFetching
	FetchStatusCompleted
	FetchStatusFailed
)

type FetchErrorStep int

const (
	FetchErrorUnknown FetchErrorStep = iota + 1
	FetchErrorInitCache
	FetchErrorDetailsCache
	FetchErrorBatchCache
	FetchErrorFinalCache
	FetchErrorFetchDetails
	FetchErrorFetchInventory
	FetchErrorFetchPrices
)

type FetchError struct {
	Message string         `json:"message"`
	Step    FetchErrorStep `json:"step"`
}

type Status int

const (
	StatusRetired Status = iota
	StatusOutOfStock
	StatusBackorder
	StatusAvailable
)

type Set struct {
	FetchStatus     FetchStatus `json:"fetch_status"`
	FetchError      *FetchError `json:"fetch_error,omitempty"`
	Id              uuid.UUID   `json:"id"`
	LegoURL         string      `json:"lego_url"`
	Slug            string      `json:"slug"`
	Name            string      `json:"name"`
	Number          string      `json:"number"`
	ImageURL        string      `json:"image_url"`
	YearReleased    int         `json:"year_released"`
	Status          Status      `json:"status"`
	Price           Price       `json:"price"`
	Prices          PricePerCurrencies
	InstructionsURL string  `json:"instructions_url"`
	Parts           int     `json:"parts"`
	Bricks          []Brick `json:"bricks"`
	BricklinkID     int     `json:"bricklink_id"`
	BricklinkNumber string  `json:"bricklink_number"`
}

func (s *Set) BuildLegoURL(locale language.Tag) {
	s.LegoURL = "https://www.lego.com/" + locale.String() + "product/" + s.Slug
}

func (s *Set) BuildInstructionsURL(locale language.Tag) {
	s.InstructionsURL = "https://www.lego.com/" + locale.String() + "/service/building-instructions/" + s.Number
}

// MapSetFromBricklinkSearch maps a Bricklink search item to an internal Set representation
func MapSetFromBricklinkSearch(bs bricklink.SearchItem) (Set, error) {
	// Assign a local UUID to each set
	setId, err := uuid.NewUUID()
	if err != nil {
		return Set{}, err
	}

	// Map to internal set representation
	return Set{
		Id:              setId,
		Name:            bs.StrItemName,
		BricklinkID:     bs.IDItem,
		BricklinkNumber: bs.StrItemNo,
	}, nil
}

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

// ApplyCurrency sets the Set's Price based on the given locale tag
func (s *Set) ApplyCurrency(tag language.Tag) bool {
	price, ok := s.Prices.GetPrice(tag)
	if !ok {
		return false
	}
	s.Price = *price
	return true
}

// DetailsResponse represents the response for a set details request
type DetailsResponse struct {
	// WebsocketID is the WebSocket UUID to connect to for updates
	WebsocketID string `json:"websocket_id"`
	// Completed indicates if the job is already done
	Completed bool `json:"completed"`
	// Set contains the data if already completed, otherwise nil
	Set Set `json:"set,omitempty"`
}
