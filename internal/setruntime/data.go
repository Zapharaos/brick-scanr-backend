package setruntime

import (
	"context"

	"github.com/Zapharaos/brick-scanr-backend/internal/set"
	"github.com/google/uuid"
)

type DataType uint8
type DataChangeReason uint8

const (
	DataTypeSet DataType = iota
	DataTypeBricklinkDetails
	DataTypeBricklinkBricks
	DataTypePickabrickBricks
)

const (
	DataTypeCreated DataChangeReason = iota
	DataTypeUpdated
	DataTypeCompleted
	DataTypeFailed
	DataTypeProgress
)

type dataChange struct {
	Id       uuid.UUID
	Type     DataType
	Reason   DataChangeReason
	Progress Progress
}

// handleDataChangeCreated handles the creation of data
func (rs *RuntimeSet) handleDataChangeCreated(change dataChange) {
	switch change.Type {
	case DataTypeSet:
		rs.refreshSet(change.Id)
		rs.broadcastPacket(NewPacketSet(rs.GetSet(), false))
		break
	default:
		break
	}
}

// handleDataChangeUpdated handles the update of data
func (rs *RuntimeSet) handleDataChangeUpdated(change dataChange) {
	switch change.Type {
	case DataTypeSet:
		rs.refreshSet(change.Id)
		rs.broadcastPacket(NewPacketSet(rs.GetSet(), false))
		break
	default:
		break
	}
}

// handleDataChangeCompleted handles the data completion
func (rs *RuntimeSet) handleDataChangeCompleted(change dataChange) {
	switch change.Type {
	case DataTypeSet:
		rs.refreshSet(change.Id)
		rs.broadcastPacket(NewPacketSet(rs.GetSet(), false))
		break
	default:
		break
	}
}

// handleDataChangeFailed handles the data failure
func (rs *RuntimeSet) handleDataChangeFailed(change dataChange) {
	// Refresh set from cache to get the latest error details
	rs.refreshSet(change.Id)

	// Determine the fatal error code and message based on the data type
	var fatalPacket *PacketFatal

	fetchError := rs.GetFetchError()
	if fetchError != nil {
		fatalPacket = NewPacketFatal(fetchError.Step, fetchError.Message)
	} else {
		// Fallback if FetchError is not set for some reason
		fatalPacket = NewPacketFatal(set.FetchErrorUnknown, "An unknown error occurred during set processing")
	}

	// Broadcast the fatal error to all connected clients
	rs.broadcastPacket(fatalPacket)
}

// handleDataChangeProgress handles batch progress updates
func (rs *RuntimeSet) handleDataChangeProgress(change dataChange) {
	// Type assert and handle based on the data type
	switch change.Type {
	case DataTypeBricklinkBricks, DataTypePickabrickBricks:
		// Convert Items ([]any) to []set.Brick
		bricks := make([]set.Brick, 0, len(change.Progress.Items))
		for _, item := range change.Progress.Items {
			if brick, ok := item.(set.Brick); ok {
				bricks = append(bricks, brick)
			}
		}

		// Store bricks in RuntimeSet for new clients joining later
		rs.AddBricks(bricks)

		// Determine the batch status
		var status BatchStatus
		if change.Type == DataTypeBricklinkBricks {
			status = BatchStatusBricklinkInventory
		} else {
			status = BatchStatusPickabrickPrices
		}

		// Create and broadcast the inventory batch packet
		packet := NewPacketInventoryBatch(bricks, &change.Progress, status)
		rs.broadcastPacket(packet)

	default:
		// Unknown data type for progress, ignore
		return
	}
}

// refreshSet refreshes the set data from Redis
func (rs *RuntimeSet) refreshSet(setId uuid.UUID) {
	// todo : ISSUE #9 - Currency : can't have a same rs with different currencies or
	// make sure that the cachedSet data has the right currency applied every time
	cachedSet, err := set.GetRedisSet(context.Background(), setId)
	if err != nil {
		return
	}

	// Update the entire set atomically
	rs.setMutex.Lock()
	rs.set = cachedSet
	rs.setMutex.Unlock()
}
