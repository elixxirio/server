package realtime

import (
	"gitlab.com/privategrity/crypto/cyclic"
	"gitlab.com/privategrity/server/server"
	"gitlab.com/privategrity/server/services"
	"testing"
)

func TestRealTimeDecrypt(t *testing.T) {
	// NOTE: Does not test correctness

	test := 3
	pass := 0

	bs := uint64(3)

	round := server.NewRound(bs)

	var im []*services.Message

	gen := cyclic.NewGen(cyclic.NewInt(0), cyclic.NewInt(1000))

	g := cyclic.NewGroup(cyclic.NewInt(101), cyclic.NewInt(23), gen)

	im = append(im, &services.Message{uint64(0), []*cyclic.Int{
		cyclic.NewInt(int64(39)), cyclic.NewInt(int64(13)),
		cyclic.NewInt(int64(41)), cyclic.NewInt(int64(74)),
	}})

	im = append(im, &services.Message{uint64(1), []*cyclic.Int{
		cyclic.NewInt(int64(86)), cyclic.NewInt(int64(87)),
		cyclic.NewInt(int64(8)), cyclic.NewInt(int64(49)),
	}})

	im = append(im, &services.Message{uint64(2), []*cyclic.Int{
		cyclic.NewInt(int64(39)), cyclic.NewInt(int64(51)),
		cyclic.NewInt(int64(91)), cyclic.NewInt(int64(73)),
	}})

	round.R[0] = cyclic.NewInt(53)
	round.R[1] = cyclic.NewInt(24)
	round.R[2] = cyclic.NewInt(61)

	round.U[0] = cyclic.NewInt(52)
	round.U[1] = cyclic.NewInt(68)
	round.U[2] = cyclic.NewInt(11)

	expected := [][]*cyclic.Int{
		{cyclic.NewInt(8), cyclic.NewInt(42)},
		{cyclic.NewInt(49), cyclic.NewInt(60)},
		{cyclic.NewInt(46), cyclic.NewInt(46)},
	}

	dc := services.DispatchCryptop(&g, RealTimeDecrypt{}, nil, nil, round)

	for i := uint64(0); i < bs; i++ {
		dc.InChannel <- im[i]
		actual := <-dc.OutChannel

		expectedVal := expected[i]

		valid := true

		for j := 0; j < 2; j++ {
			valid = valid && (expectedVal[j].Cmp(actual.Data[j]) == 0)
		}

		if !valid {
			t.Errorf("Test of RealTimeDecrypt's cryptop failed on index: %v", i)
		} else {
			pass++
		}

	}

	println("RealTimeDecrypt", pass, "out of", test, "tests passed.")

}
