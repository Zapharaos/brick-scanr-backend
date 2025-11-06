package setruntime

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/set"
	"github.com/Zapharaos/brick-scanr-backend/internal/supervisor"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type RuntimeSet struct {
	ID  uuid.UUID
	set set.Set
	*clientHolder
	receive    chan clientMessage
	register   chan Client
	unregister chan uuid.UUID
	changeChan chan dataChange

	opt RuntimeOptions

	errorLogger *supervisor.AsyncErrorLogger

	done  chan struct{}
	onEnd func(uuid.UUID)

	wg *sync.WaitGroup
}

// NewRuntimeSet creates a new runtime set
func NewRuntimeSet(s set.Set, opt RuntimeOptions, wg *sync.WaitGroup, errorLogger *supervisor.AsyncErrorLogger) *RuntimeSet {
	rs := &RuntimeSet{
		ID:           uuid.New(),
		set:          s,
		errorLogger:  errorLogger,
		opt:          opt,
		clientHolder: newClientHolder(true),
		receive:      make(chan clientMessage, opt.ReceiveChanCap),
		register:     make(chan Client, opt.ClientChanCap),
		unregister:   make(chan uuid.UUID, opt.ClientChanCap),
		changeChan:   make(chan dataChange, opt.ChangeChanCap),
		done:         make(chan struct{}),
		wg:           wg,
	}

	return rs
}

// Start starts the runtime set
func (rs *RuntimeSet) Start() {
	go rs.run()
}

// PushChange pushes a data change to the runtime set
func (rs *RuntimeSet) PushChange(change dataChange) {
	rs.changeChan <- change
}

// Unregister returns the unregister channel
func (rs *RuntimeSet) Unregister() chan uuid.UUID {
	return rs.unregister
}

// Receive returns the reception channel
func (rs *RuntimeSet) Receive() chan clientMessage {
	return rs.receive
}

// Register returns the register channel
func (rs *RuntimeSet) Register() chan Client {
	return rs.register
}

// HasClient checks if a client is connected
func (rs *RuntimeSet) HasClient(id uuid.UUID) bool {
	defer rs.clientMutex.RUnlock()
	rs.clientMutex.RLock()
	_, ok := rs.clients[id]
	return ok
}

// logError logs an error message
func (rs *RuntimeSet) logError(scope string, err error) {
	if rs.errorLogger == nil || err == nil {
		return
	}

	rs.errorLogger.LogError(set.Error{
		Scope:    scope,
		Message:  err.Error(),
		Severity: string(LogSeverityError),
		SetId:    rs.set.Id,
	})
}

// stop stops the runtime set
func (rs *RuntimeSet) stop() {
	zap.L().Info("Set ended", zap.String("set", rs.set.Id.String()))

	rs.clientMutex.RLock()
	// unregister all clients
	for _, c := range rs.clients {
		c.close()
	}
	rs.clientMutex.RUnlock()

	rs.onEnd(rs.ID)
}

// run starts the runtime set
func (rs *RuntimeSet) run() {
	rs.wg.Add(1)

	setActivityTimer := time.NewTimer(rs.opt.Timeout)
	clientExpire := time.NewTicker(rs.opt.ClientTimeoutCheckFreq)
	setChangesListenerTimer := time.NewTicker(rs.opt.setChangeCheckFreq)
	defer func() {

		// handle possible panics
		if r := recover(); r != nil {
			zap.L().Error("Panic in set runtime, recovering and stopping set", zap.Any("panic", r))
		}

		setActivityTimer.Stop()
		clientExpire.Stop()
		rs.stop()
		rs.wg.Done()
	}()

	for {
		select {
		case cli := <-rs.register:
			setActivityTimer.Reset(rs.opt.Timeout)
			rs.handleClientConnect(cli)

		case id := <-rs.unregister:
			setActivityTimer.Reset(rs.opt.Timeout)
			rs.handleClientDisconnect(id)

		case msg := <-rs.receive:

			// ignore messages from clients that are not connected
			rs.clientMutex.RLock()
			_, ok := rs.clients[msg.client.ID()]
			rs.clientMutex.RUnlock()

			if !ok {
				continue
			}

			setActivityTimer.Reset(rs.opt.Timeout)

			// handle the message
			rs.handle(msg)

		case change := <-rs.changeChan:
			rs.handleDataChange(change)

		case <-clientExpire.C:
			rs.runClientExpireChecker()

		case <-setChangesListenerTimer.C:
			rs.checkForChanges()

		case <-rs.done:
			rs.stop()
			return
		}
	}
}

// handle handles a message
func (rs *RuntimeSet) handle(msg clientMessage) {
	// first we need to decode the packet
	var p packet
	err := json.Unmarshal(msg.data, &p)
	if err != nil {
		zap.L().Error("Failed to unmarshal packet", zap.Error(err))
		// todo: v2 - do we close cli connection here ?
		return
	}

	if len(p.Hash) > 64 {
		rs.clientFatal(msg.client.ID(), errors.New("packet hash is too long"))
		return
	}

	// then we need to check the type of packet
	switch p.Type {

	// TODO : Client - case client update ?

	case PacketTypeClientDisconnect:
		// TODO : Client - clear set ?
	}
}

// checkForChanges checks for changes in the runtime set, like images, inventory, prices etc.
func (rs *RuntimeSet) checkForChanges() {
	// TODO : Client - implement
}

// handleDataChange handles a data change
func (rs *RuntimeSet) handleDataChange(change dataChange) {
	switch change.Reason {
	case DataTypeCreated:
		rs.handleDataChangeCreated(change)
	case DataTypeUpdated:
		rs.handleDataChangeUpdated(change)
	case DataTypeCompleted:
		rs.handleDataChangeCompleted(change)
	case DataTypeFailed:
		rs.handleDataChangeFailed(change)
	}
}

// Client functions

// clientFatal handles a fatal error
func (rs *RuntimeSet) clientFatal(id uuid.UUID, err error) {

	// unregister the client from the set
	rs.unregister <- id

	// send the error to the client
	rs.clientMutex.RLock()
	c, ok := rs.clients[id]
	rs.clientMutex.RUnlock()

	if !ok {
		return
	}

	c.SendPacket(NewPacketLog(LogSeverityFatal, err.Error()))

	// somehow close the client connection
	c.close()
}

// handleClientConnect handles a client connect
func (rs *RuntimeSet) handleClientConnect(client Client) {
	rs.registerClient(client)

	// Gather data and set it to the client
	client.SendPacket(NewPacketInit(
		rs.set,
	))
}

// handleClientDisconnect handles a client disconnect
func (rs *RuntimeSet) handleClientDisconnect(id uuid.UUID) {
	defer func() {
		rs.unregisterClient(id)
	}()

	rs.clientMutex.RLock()
	_, ok := rs.clients[id]
	rs.clientMutex.RUnlock()
	if !ok {
		return
	}
}

// sendErrorPacket sends an error packet to a client
func (rs *RuntimeSet) sendErrorPacket(client Client, packet packet, code PacketErrorCode) {
	client.SendPacket(NewPacketError(packet.Hash, code))
}

// sendSuccessPacket sends a success packet to a client
func (rs *RuntimeSet) sendSuccessPacket(client Client, packet packet) {
	client.SendPacket(NewPacketSuccess(packet.Hash))
}

// runClientExpireChecker handles client expiration
func (rs *RuntimeSet) runClientExpireChecker() {
	defer rs.clientMutex.Unlock()
	rs.clientMutex.Lock()

	now := time.Now()
	nb := 0

	for _, c := range rs.clients {
		if now.Sub(c.LastActivity()) > rs.opt.ClientTimeout {
			nb++
			c.close()
		}
	}

	if nb > 0 {
		zap.L().Info("Closed expired clients", zap.Int("nb", nb), zap.Duration("timeout", rs.opt.ClientTimeout))
	}
}
