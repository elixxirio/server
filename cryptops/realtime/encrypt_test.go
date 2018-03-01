////////////////////////////////////////////////////////////////////////////////
// Copyright © 2018 Privategrity Corporation                                   /
//                                                                             /
// All rights reserved.                                                        /
////////////////////////////////////////////////////////////////////////////////

package realtime

import (
	"gitlab.com/privategrity/crypto/cyclic"
	"gitlab.com/privategrity/server/globals"
	"gitlab.com/privategrity/server/services"
	"testing"
)

func TestEncrypt(t *testing.T) {
	// NOTE: Does not test correctness

	test := 6
	pass := 0

	bs := uint64(3)

	round := globals.NewRound(bs)

	rng := cyclic.NewRandom(cyclic.NewInt(0), cyclic.NewInt(1000))

	grp := cyclic.NewGroup(cyclic.NewInt(101), cyclic.NewInt(23), cyclic.NewInt(27), rng)

	recipientIds := [3]uint64{uint64(5), uint64(7), uint64(9)}

	var im []services.Slot

	im = append(im, &RealtimeSlot{
		Slot:       uint64(0),
		CurrentID:  recipientIds[0],
		Message:    cyclic.NewInt(int64(39)),
		CurrentKey: cyclic.NewInt(int64(65))})

	im = append(im, &RealtimeSlot{
		Slot:       uint64(1),
		CurrentID:  recipientIds[1],
		Message:    cyclic.NewInt(int64(86)),
		CurrentKey: cyclic.NewInt(int64(44))})

	im = append(im, &RealtimeSlot{
		Slot:       uint64(2),
		CurrentID:  recipientIds[2],
		Message:    cyclic.NewInt(int64(66)),
		CurrentKey: cyclic.NewInt(int64(94))})

	// Set the keys
	round.T[0] = cyclic.NewInt(52)
	round.T[1] = cyclic.NewInt(68)
	round.T[2] = cyclic.NewInt(11)

	expected := [][]*cyclic.Int{
		{cyclic.NewInt(15)},
		{cyclic.NewInt(65)},
		{cyclic.NewInt(69)},
	}

	dc := services.DispatchCryptop(&grp, Encrypt{}, nil, nil, round)

	for i := uint64(0); i < bs; i++ {
		dc.InChannel <- &(im[i])
		rtn := <-dc.OutChannel

		result := expected[i]

		rtnXtc := (*rtn).(*RealtimeSlot)

		// Test EncryptedMessage results
		for j := 0; j < 1; j++ {
			if result[j].Cmp(rtnXtc.Message) != 0 {
				t.Errorf("Test of RealtimeEncrypt's EncryptedMessage output "+
					"failed on index: %v on value: %v.  Expected: %v Received: %v ",
					i, j, result[j].Text(10), rtnXtc.Message.Text(10))
			} else {
				pass++
			}
		}

		// Test RecipientID pass through
		if recipientIds[i] != rtnXtc.CurrentID {
			t.Errorf("Test of RealtimeEncrypt's RecipientID ouput failed on index %v.  Expected: %v Received: %v ",
				i, recipientIds[i], rtnXtc.CurrentID)
		} else {
			pass++
		}

	}

	println("Realtime Encrypt", pass, "out of", test, "tests passed.")

}