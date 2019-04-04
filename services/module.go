package services

import (
	"gitlab.com/elixxir/crypto/cryptops"
	"sync"
)

type OutputNotify chan Chunk

type adapter func(stream Stream, cryptop cryptops.Cryptop, chunk Chunk) error

type Module struct {
	//Public
	// Its method should probably not be called Adapt, I think
	Adapt   adapter
	Cryptop cryptops.Cryptop

	AssignmentSize uint32
	ChunkSize      uint32

	Name string

	NumThreads uint32

	moduleState

	//Private
	input         OutputNotify
	inputClosed   bool
	inputLock     sync.Mutex
	id            uint64
	inputModules  []*Module
	outputModules []*Module

	assignmentList
}

func (m *Module) closeInput() {
	println("About to lock", m.Name)
	m.inputLock.Lock()
    println("locked it", m.Name)
	if !m.inputClosed {
		// Commenting this does prevent the send on closed channel, but also causes the program to not terminate
		close(m.input)
	}
	m.inputLock.Unlock()
	println("Unlocked it", m.Name)
}
