package set

import (
	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/google/uuid"
)

type FetchStatus int

const (
	FetchStatusUnknown FetchStatus = iota
	FetchStatusStarting
	FetchStatusFetching
	FetchStatusFetchingInventory
	FetchStatusFetchingInventoryPrices
	FetchStatusCompleted
	FetchStatusFailed
)

type Set struct {
	FetchStatus     FetchStatus `json:"fetch_status"`
	Id              uuid.UUID   `json:"id"`
	Name            string      `json:"name"`
	Number          string      `json:"number"`
	YearReleased    int         `json:"year_released"`
	ImageURL        string      `json:"image_url"`
	Bricks          []Brick     `json:"bricks"`
	BricklinkID     int         `json:"bricklink_id"`
	BricklinkNumber string      `json:"bricklink_number"`
}

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

func MapBricksFromBricklinkInventory(bi *bricklink.Inventory) []Brick {
	bricks := make([]Brick, 0, len(bi.Items))
	for _, item := range bi.Items {
		brick := MapBrickFromBricklinkInventoryItem(item)
		bricks = append(bricks, brick)
	}

	return bricks
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
