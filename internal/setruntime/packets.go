package setruntime

import (
	"encoding/json"

	"github.com/Zapharaos/brick-scanr-backend/internal/set"
)

type PacketType string

const (
	PacketTypeInit             PacketType = "init"
	PacketTypeLog              PacketType = "log"
	PacketTypeError            PacketType = "error"
	PacketTypeSuccess          PacketType = "success"
	PacketTypeClientDisconnect PacketType = "clientDisconnect"
)

type PacketErrorCode int

const (
	ErrorClientInvalidPacket = PacketErrorCode(iota + 1)
	ErrorClientNotFound
)

// packetSpec is a struct that contains all the possible packets, used for swagger doc and generation
type packetSpec struct {
	Packet      packet      `json:"packet"`
	PacketLog   PacketLog   `json:"packetLog"`
	PacketInit  PacketInit  `json:"packetInit"`
	PacketError PacketError `json:"packetError"`
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

// NewPacketClientDisconnect creates a new PacketClientDisconnect
func NewPacketClientDisconnect() *packet {
	return &packet{
		Type: PacketTypeClientDisconnect,
	}
}
