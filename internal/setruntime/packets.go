package setruntime

import (
	"encoding/json"

	"github.com/Zapharaos/brick-scanr-backend/internal/set"
	"github.com/Zapharaos/brick-scanr-backend/internal/throttle"
	"github.com/Zapharaos/brick-scanr-backend/internal/wsruntime"
)

type PacketType string

const (
	PacketTypeInit           PacketType = "init"
	PacketTypeFatal          PacketType = "fatal"
	PacketTypeSet            PacketType = "set"
	PacketTypeInventoryBatch PacketType = "inventoryBatch"
	PacketTypeThrottleStatus PacketType = "throttleStatus"
)

type BatchStatus string

const (
	BatchStatusBricklinkInventory BatchStatus = "bricklinkInventory"
	BatchStatusPickabrickPrices   BatchStatus = "pickabrickPrices"
)

// packetSpec is a struct that contains all the possible packets
// WARNING : used for swagger doc and generation
type packetSpec struct {
	Packet               packet               `json:"packet"`
	PacketInit           PacketInit           `json:"packetInit"`
	PacketFatal          PacketFatal          `json:"packetFatal"`
	PacketSet            PacketSet            `json:"packetSet"`
	PacketInventoryBatch PacketInventoryBatch `json:"packetInventoryBatch"`
	PacketThrottleStatus PacketThrottleStatus `json:"packetThrottleStatus"`
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

// PacketInit is a packet to initialize the set
type PacketInit struct {
	packet
	Set set.External `json:"set"`
}

func NewPacketInit(set set.External) *PacketInit {
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
	Set set.External `json:"set"`
}

// NewPacketSet creates a new PacketSet
func NewPacketSet(s set.External, bricks bool) *PacketSet {
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

// PacketInventoryBatch is a packet to send a batch of Bricks
type PacketInventoryBatch struct {
	packet
	Bricks         []set.Brick         `json:"bricks"`
	BricksProgress *wsruntime.Progress `json:"bricksProgress"`
	Status         BatchStatus         `json:"status"`
	// EtaSeconds is the estimated remaining time for the current phase, based on
	// the processing rate over the last 30s. Omitted when it cannot be estimated
	// (0 = unknown), e.g. too little history or a stall due to upstream throttling.
	EtaSeconds int `json:"etaSeconds,omitempty"`
}

// NewPacketInventoryBatch creates a new PacketInventoryBatch
func NewPacketInventoryBatch(bricks []set.Brick, progress *wsruntime.Progress, status BatchStatus, etaSeconds int) *PacketInventoryBatch {
	return &PacketInventoryBatch{
		packet: packet{
			Type: PacketTypeInventoryBatch,
		},
		Bricks:         bricks,
		BricksProgress: progress,
		Status:         status,
		EtaSeconds:     etaSeconds,
	}
}

// ToJSON returns the JSON representation of the packet
func (p *PacketInventoryBatch) ToJSON() ([]byte, error) {
	return json.Marshal(p)
}

// --------------------------------------------
// --------------------------------------------
// --------------------------------------------

// PacketThrottleStatus informs the client that the upstream APIs are throttling
// us, so a frozen progress bar can be explained ("resuming in ~Xs") instead of
// looking broken.
type PacketThrottleStatus struct {
	packet
	// State is the current throttle state: normal / slowed / paused / blocked.
	State throttle.State `json:"state"`
	// ResumeAt is the epoch milliseconds at which the wait is expected to end.
	// Meaningful when State is "blocked" (server block) or "paused" (our own
	// sliding-window pause); omitted otherwise.
	ResumeAt int64 `json:"resumeAt,omitempty"`
}

// NewPacketThrottleStatus creates a new PacketThrottleStatus
func NewPacketThrottleStatus(state throttle.State, resumeAt int64) *PacketThrottleStatus {
	return &PacketThrottleStatus{
		packet: packet{
			Type: PacketTypeThrottleStatus,
		},
		State:    state,
		ResumeAt: resumeAt,
	}
}

// ToJSON returns the JSON representation of the packet
func (p *PacketThrottleStatus) ToJSON() ([]byte, error) {
	return json.Marshal(p)
}
