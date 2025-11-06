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
	DataTypeSetInventory
	DataTypeSetInventoryPrices
)

const (
	DataTypeCreated DataChangeReason = iota
	DataTypeUpdated
	DataTypeCompleted
	DataTypeFailed
)

type dataChange struct {
	Id     uuid.UUID
	Type   DataType
	Reason DataChangeReason
}

// handleDataChangeCreated handles the creation of data
func (rs *RuntimeSet) handleDataChangeCreated(change dataChange) {
	switch change.Type {
	case DataTypeSet:
		rs.refreshSet(change.Id)
		break
	case DataTypeSetInventory:
		rs.set.FetchStatus = set.FetchStatusFetchingInventory
		break
	case DataTypeSetInventoryPrices:
		rs.set.FetchStatus = set.FetchStatusFetchingInventoryPrices
		break
	default:
		break
	}
}

// handleDataChangeUpdated handles the update of data
func (rs *RuntimeSet) handleDataChangeUpdated(change dataChange) {
	switch change.Type {
	case DataTypeSetInventoryPrices:
		rs.refreshSet(change.Id)
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
		rs.set.FetchStatus = set.FetchStatusCompleted
		break
	case DataTypeSetInventory:
		rs.refreshSet(change.Id)
		break
	case DataTypeSetInventoryPrices:
		rs.refreshSet(change.Id)
		break
	default:
		break
	}
}

// handleDataChangeFailed handles the data failure
func (rs *RuntimeSet) handleDataChangeFailed(change dataChange) {
	rs.set.FetchStatus = set.FetchStatusFailed
}

// refreshSet refreshes the set data from Redis
func (rs *RuntimeSet) refreshSet(setId uuid.UUID) {
	cachedSet, err := set.GetRedisSet(context.Background(), setId)
	if err != nil {
		return
	}
	rs.set = cachedSet
}
