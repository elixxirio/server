///////////////////////////////////////////////////////////////////////////////
// Copyright © 2020 xx network SEZC                                          //
//                                                                           //
// Use of this source code is governed by a license that can be found in the //
// LICENSE file                                                              //
///////////////////////////////////////////////////////////////////////////////

package storage

import (
	"crypto/sha256"
	"encoding/binary"
	"gitlab.com/elixxir/crypto/cyclic"
	"gitlab.com/xx_network/primitives/id"
	"strconv"
	"sync"
)

// Number of hard-coded users to create
var numDemoUsers = int(256)

// PrecanStore is a map of precanned IDs to precanned keys.
// This map is static, and should not be modified after a
// call to NewPrecanStore. This is used for development purposes only
type PrecanStore struct {
	store map[id.ID][]byte
	mux   sync.Mutex
}

// NewPrecanStore builds a PrecanStore object prepopulated
// with precanned values.
func NewPrecanStore(grp *cyclic.Group) *PrecanStore {
	store := make(map[id.ID][]byte, numDemoUsers)
	ps := &PrecanStore{
		store: store,
		mux:   sync.Mutex{},
	}

	// Generate junk message dummy user
	dummyId := id.DummyUser.DeepCopy()
	dummyKey := grp.NewIntFromBytes(dummyId.Marshal()[:]).Bytes()
	ps.store[*dummyId] = dummyKey


	// Deterministically create named users for demo
	for i := 1; i < numDemoUsers; i++ {
		h := sha256.New()
		h.Reset()
		h.Write([]byte(strconv.Itoa(4000 + i)))
		usrID := new(id.ID)
		binary.BigEndian.PutUint64(usrID[:], uint64(i))
		usrID.SetType(id.User)
		ps.store[*usrID] = grp.NewIntFromBytes(h.Sum(nil)).Bytes()
	}

	return ps
}

// Get retrieves the precanned key associated with userID if it exists.
// If it does not exist, this userID is not a designated precanned ID,
// and the boolean returned is false. If it does exist, the precanned key
// is returned and the boolean returned is true.
func (ps *PrecanStore) Get(userId *id.ID) ([]byte, bool) {
	ps.mux.Lock()
	defer ps.mux.Unlock()

	val, ok := ps.store[*userId]
	return val, ok
}
