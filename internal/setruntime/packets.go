package setruntime

import (
	"encoding/json"

	"github.com/Zapharaos/brick-scanr-backend/internal/set"
)

type PacketType string

const (
	PacketTypeInit           PacketType = "init"
	PacketTypeLog            PacketType = "log"
	PacketTypeError          PacketType = "error"
	PacketTypeFatal          PacketType = "fatal"
	PacketTypeSuccess        PacketType = "success"
	PacketTypeSet            PacketType = "set"
	PacketTypeInventoryBatch PacketType = "inventoryBatch"
)

type PacketErrorCode int

const (
	ErrorClientInvalidPacket = PacketErrorCode(iota + 1)
	ErrorClientNotFound
)

type BatchStatus string

const (
	BatchStatusBricklinkInventory BatchStatus = "bricklinkInventory"
	BatchStatusPickabrickPrices   BatchStatus = "pickabrickPrices"
)

// packetSpec is a struct that contains all the possible packets, used for swagger doc and generation
type packetSpec struct {
	Packet               packet               `json:"packet"`
	PacketLog            PacketLog            `json:"packetLog"`
	PacketInit           PacketInit           `json:"packetInit"`
	PacketError          PacketError          `json:"packetError"`
	PacketFatal          PacketFatal          `json:"packetFatal"`
	PacketSet            PacketSet            `json:"packetSet"`
	PacketInventoryBatch PacketInventoryBatch `json:"packetInventoryBatch"`
}

// Packet interface
type Packet interface {
	ToJSON() ([]byte, error)
}

// Packet is a generic packet
type packet struct {
	Type PacketType `json:"type"`
	Hash string     `json:"hash"`
}

// ToJSON returns the JSON representation of the packet
func (p *packet) ToJSON() ([]byte, error) {
	return json.Marshal(p)
}

// --------------------------------------------
// --------------------------------------------
// --------------------------------------------

type LogSeverity string

const (
	LogSeverityInfo    LogSeverity = "info"
	LogSeverityWarning LogSeverity = "warning"
	LogSeverityError   LogSeverity = "error"
	LogSeverityFatal   LogSeverity = "fatal"
)

type PacketLog struct {
	packet
	Error    string      `json:"error"`
	Severity LogSeverity `json:"severity"`
}

func NewPacketLog(severity LogSeverity, error string) *PacketLog {
	return &PacketLog{
		packet: packet{
			Type: PacketTypeLog,
		},
		Error:    error,
		Severity: severity,
	}
}

func (p *PacketLog) ToJSON() ([]byte, error) {
	return json.Marshal(p)
}

// --------------------------------------------
// --------------------------------------------
// --------------------------------------------

// PacketInit is a packet to initialize the set
type PacketInit struct {
	packet
	Set set.Set `json:"set"`
}

func NewPacketInit(set set.Set) *PacketInit {
	return &PacketInit{
		packet: packet{
			Type: PacketTypeInit,
		},
		Set: set,
	}
}

func (p *PacketInit) ToJSON() ([]byte, error) {
	return json.Marshal(p)
}

// NewPacketSuccess creates a new PacketSuccess
func NewPacketSuccess(hash string) *packet {
	return &packet{
		Type: PacketTypeSuccess,
		Hash: hash,
	}
}

// PacketError is a packet to send an error
type PacketError struct {
	packet
	Code PacketErrorCode `json:"code"`
}

// NewPacketError creates a new PacketError
func NewPacketError(hash string, code PacketErrorCode) *PacketError {
	return &PacketError{
		packet: packet{
			Type: PacketTypeError,
			Hash: hash,
		},
		Code: code,
	}
}

// ToJSON returns the JSON representation of the packet
func (p *PacketError) ToJSON() ([]byte, error) {
	return json.Marshal(p)
}

// PacketFatal is a packet to send a fatal internal error
type PacketFatal struct {
	packet
	Step    set.FetchErrorStep `json:"step"`
	Message string             `json:"message"`
}

// NewPacketFatal creates a new PacketFatal
func NewPacketFatal(step set.FetchErrorStep, message string) *PacketFatal {
	return &PacketFatal{
		packet: packet{
			Type: PacketTypeFatal,
		},
		Step:    step,
		Message: message,
	}
}

// ToJSON returns the JSON representation of the packet
func (p *PacketFatal) ToJSON() ([]byte, error) {
	return json.Marshal(p)
}

// --------------------------------------------
// --------------------------------------------
// --------------------------------------------

type PacketSet struct {
	packet
	Set set.Set `json:"set"`
}

// NewPacketSet creates a new PacketSet
func NewPacketSet(s set.Set, bricks bool) *PacketSet {
	if !bricks {
		s.Bricks = []set.Brick{}
	}
	return &PacketSet{
		packet: packet{
			Type: PacketTypeSet,
		},
		Set: s,
	}
}

// ToJSON returns the JSON representation of the packet
func (p *PacketSet) ToJSON() ([]byte, error) {
	return json.Marshal(p)
}

// PacketInventoryBatch is a packet to send a batch of bricks
type PacketInventoryBatch struct {
	packet
	Bricks         []set.Brick `json:"bricks"`
	BricksProgress *Progress   `json:"bricksProgress"`
	Status         BatchStatus `json:"status"`
}

// NewPacketInventoryBatch creates a new PacketInventoryBatch
func NewPacketInventoryBatch(bricks []set.Brick, progress *Progress, status BatchStatus) *PacketInventoryBatch {
	return &PacketInventoryBatch{
		packet: packet{
			Type: PacketTypeInventoryBatch,
		},
		Bricks:         bricks,
		BricksProgress: progress,
		Status:         status,
	}
}

// ToJSON returns the JSON representation of the packet
func (p *PacketInventoryBatch) ToJSON() ([]byte, error) {
	return json.Marshal(p)
}
