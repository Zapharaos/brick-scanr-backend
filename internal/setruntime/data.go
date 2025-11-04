package setruntime

import (
	"github.com/google/uuid"
	"go.uber.org/zap"
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
)

type dataChange struct {
	Id       uuid.UUID
	ParentId uuid.UUID
	Type     DataType
	Reason   DataChangeReason
}

// handleDataChangeCreated handles the creation of data
func (rs *RuntimeSet) handleDataChangeCreated(change dataChange) {
	// TODO : implement
	switch change.Type {
	case DataTypeSet:
		break
	case DataTypeSetInventory:
		break
	case DataTypeSetInventoryPrices:
		break
	default:
		break
	}
}

// handleDataChangeUpdated handles the update of data
func (rs *RuntimeSet) handleDataChangeUpdated(change dataChange) {
	// TODO : implement
	switch change.Type {
	case DataTypeSetInventoryPrices:
		break
	default:
		break
	}
}

// handleDataChangeCompleted handles the data completion
func (rs *RuntimeSet) handleDataChangeCompleted(change dataChange) {
	// TODO : implement
	switch change.Type {
	case DataTypeSet:
		break
	case DataTypeSetInventory:
		break
	case DataTypeSetInventoryPrices:
		break
	default:
		break
	}
}

// RError returns the value and if it was found or not, and logs the error if it exists
func RError[T any](t T, found bool, err error) (T, bool) {
	if err != nil {
		zap.L().Error("Error while loading changed data", zap.Error(err))
		return t, false
	}
	return t, found
}
