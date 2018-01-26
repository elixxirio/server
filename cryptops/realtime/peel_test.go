package realtime

import (
	"gitlab.com/privategrity/crypto/cyclic"
	"gitlab.com/privategrity/server/node"
	"gitlab.com/privategrity/server/services"
	"testing"
)

func TestPeel(t *testing.T) {
	// NOTE: Does not test correctness

	test := 6
	pass := 0

	bs := uint64(3)

	round := node.NewRound(bs)

	rng := cyclic.NewRandom(cyclic.NewInt(5), cyclic.NewInt(1000))

	grp := cyclic.NewGroup(cyclic.NewInt(101), cyclic.NewInt(27), cyclic.NewInt(97), rng)

	recipientIds := [3]uint64{uint64(5), uint64(7), uint64(9)}

	var im []services.Slot

	im = append(im, &SlotPeel{
		slot:             uint64(0),
		RecipientID:      recipientIds[0],
		EncryptedMessage: cyclic.NewInt(int64(39))})

	im = append(im, &SlotPeel{
		slot:             uint64(1),
		RecipientID:      recipientIds[1],
		EncryptedMessage: cyclic.NewInt(int64(86))})

	im = append(im, &SlotPeel{
		slot:             uint64(2),
		RecipientID:      recipientIds[2],
		EncryptedMessage: cyclic.NewInt(int64(66))})

	// Set the keys
	round.LastNode.MessagePrecomputation = make([]*cyclic.Int, round.BatchSize)
	round.LastNode.MessagePrecomputation[0] = cyclic.NewInt(77)
	round.LastNode.MessagePrecomputation[1] = cyclic.NewInt(93)
	round.LastNode.MessagePrecomputation[2] = cyclic.NewInt(47)

	expected := [][]*cyclic.Int{
		{cyclic.NewInt(74)},
		{cyclic.NewInt(19)},
		{cyclic.NewInt(72)},
	}

	dc := services.DispatchCryptop(&grp, Peel{}, nil, nil, round)

	for i := uint64(0); i < bs; i++ {
		dc.InChannel <- &(im[i])
		rtn := <-dc.OutChannel

		result := expected[i]

		rtnXtc := (*rtn).(*SlotPeel)

		// Test EncryptedMessage results
		for j := 0; j < 1; j++ {
			if result[j].Cmp(rtnXtc.EncryptedMessage) != 0 {
				t.Errorf("Test of RealtimePeel's EncryptedMessage output "+
					"failed on index: %v on value: %v.  Expected: %v Received: %v ",
					i, j, result[j].Text(10), rtnXtc.EncryptedMessage.Text(10))
			} else {
				pass++
			}
		}

		// Test RecipientID pass through
		if recipientIds[i] != rtnXtc.RecipientID {
			t.Errorf("Test of RealtimePeel's RecipientID ouput failed on index %v.  Expected: %v Received: %v ",
				i, recipientIds[i], rtnXtc.RecipientID)
		} else {
			pass++
		}
	}

	println("Realtime Peel", pass, "out of", test, "tests passed.")

}
