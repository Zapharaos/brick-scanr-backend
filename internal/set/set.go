package set

import (
	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
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
	StatusAvailable
)

type Set struct {
	FetchStatus     FetchStatus `json:"fetch_status"`
	FetchError      *FetchError `json:"fetch_error,omitempty"`
	Id              uuid.UUID   `json:"id"`
	Name            string      `json:"name"`
	Number          string      `json:"number"`
	ImageURL        string      `json:"image_url"`
	YearReleased    int         `json:"year_released"`
	Status          Status      `json:"status"`
	Price           Price       `json:"price"`
	Prices          map[language.Tag]*Price
	InstructionsURL string  `json:"instructions_url"`
	Parts           int     `json:"parts"`
	Bricks          []Brick `json:"bricks"`
	BricklinkID     int     `json:"bricklink_id"`
	BricklinkNumber string  `json:"bricklink_number"`
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

// DetailsResponse represents the response for a set details request
type DetailsResponse struct {
	// WebsocketID is the WebSocket UUID to connect to for updates
	WebsocketID string `json:"websocket_id"`
	// Completed indicates if the job is already done
	Completed bool `json:"completed"`
	// Set contains the data if already completed, otherwise nil
	Set Set `json:"set,omitempty"`
}
