package searchruntime

import (
	"encoding/json"
)

type PacketType string

const (
	PacketTypeInit     PacketType = "init"
	PacketTypeBatch    PacketType = "batch"
	PacketTypeComplete PacketType = "complete"
	PacketTypeError    PacketType = "error"
)

// packetSpec is a struct that contains all the possible packets
// WARNING : used for swagger doc and generation
type packetSpec struct {
	Packet         packet         `json:"packet"`
	PacketInit     PacketInit     `json:"packetInit"`
	PacketBatch    PacketBatch    `json:"packetBatch"`
	PacketComplete PacketComplete `json:"packetComplete"`
	PacketError    PacketError    `json:"packetError"`
}

// Packet interface
type Packet interface {
	ToJSON() ([]byte, error)
}

// packet is a generic base packet
type packet struct {
	Type PacketType `json:"type"`
}

func (p *packet) ToJSON() ([]byte, error) {
	return json.Marshal(p)
}

// --------------------------------------------

// PacketInit is sent when a client first connects, providing context about the search
type PacketInit struct {
	packet
	Total int `json:"total"` // total number of BrickLink results (sets + bricks)
}

func NewPacketInit(total int) *PacketInit {
	return &PacketInit{
		packet: packet{Type: PacketTypeInit},
		Total:  total,
	}
}

func (p *PacketInit) ToJSON() ([]byte, error) {
	return json.Marshal(p)
}

// --------------------------------------------

// PacketBatch is sent for each batch of processed search results
type PacketBatch struct {
	packet
	Items    []interface{} `json:"items"`
	Done     int           `json:"done"`     // items processed so far (including this batch)
	Total    int           `json:"total"`    // total items to process
	Category string        `json:"category"` // "sets" or "bricks"
}

func NewPacketBatch(items []interface{}, done, total int, category string) *PacketBatch {
	return &PacketBatch{
		packet:   packet{Type: PacketTypeBatch},
		Items:    items,
		Done:     done,
		Total:    total,
		Category: category,
	}
}

func (p *PacketBatch) ToJSON() ([]byte, error) {
	return json.Marshal(p)
}

// --------------------------------------------

// PacketComplete is sent once all results have been processed
type PacketComplete struct {
	packet
}

func NewPacketComplete() *PacketComplete {
	return &PacketComplete{
		packet: packet{Type: PacketTypeComplete},
	}
}

func (p *PacketComplete) ToJSON() ([]byte, error) {
	return json.Marshal(p)
}

// --------------------------------------------

// PacketError is sent when a fatal error stops processing
type PacketError struct {
	packet
	Message string `json:"message"`
}

func NewPacketError(message string) *PacketError {
	return &PacketError{
		packet:  packet{Type: PacketTypeError},
		Message: message,
	}
}

func (p *PacketError) ToJSON() ([]byte, error) {
	return json.Marshal(p)
}
