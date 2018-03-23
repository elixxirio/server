////////////////////////////////////////////////////////////////////////////////
// Copyright © 2018 Privategrity Corporation                                   /
//                                                                             /
// All rights reserved.                                                        /
////////////////////////////////////////////////////////////////////////////////

package realtime

import (
	"gitlab.com/privategrity/crypto/cyclic"
	"gitlab.com/privategrity/crypto/format"
	"gitlab.com/privategrity/crypto/verification"
	"gitlab.com/privategrity/server/globals"
	"gitlab.com/privategrity/server/services"
	"testing"
)

func TestRealTimeIdentify(t *testing.T) {
	var im []services.Slot
	batchSize := uint64(2)
	round := globals.NewRound(batchSize)
	round.RecipientPrecomputation = make([]*cyclic.Int, batchSize)
	round.RecipientPrecomputation[0] = cyclic.NewInt(42)
	round.RecipientPrecomputation[1] = cyclic.NewInt(42)

	grp := cyclic.NewGroup(cyclic.NewIntFromString(
		"FFFFFFFFFFFFFFFFC90FDAA22168C234C4C6628B80DC1CD1"+
			"29024E088A67CC74020BBEA63B139B22514A08798E3404DD"+
			"EF9519B3CD3A431B302B0A6DF25F14374FE1356D6D51C245"+
			"E485B576625E7EC6F44C42E9A637ED6B0BFF5CB6F406B7ED"+
			"EE386BFB5A899FA5AE9F24117C4B1FE649286651ECE45B3D"+
			"C2007CB8A163BF0598DA48361C55D39A69163FA8FD24CF5F"+
			"83655D23DCA3AD961C62F356208552BB9ED529077096966D"+
			"670C354E4ABC9804F1746C08CA18217C32905E462E36CE3B"+
			"E39E772C180E86039B2783A2EC07A28FB5C55DF06F4C52C9"+
			"DE2BCBF6955817183995497CEA956AE515D2261898FA0510"+
			"15728E5A8AAAC42DAD33170D04507A33A85521ABDF1CBA64"+
			"ECFB850458DBEF0A8AEA71575D060C7DB3970F85A6E1E4C7"+
			"ABF5AE8CDB0933D71E8C94E04A25619DCEE3D2261AD2EE6B"+
			"F12FFA06D98A0864D87602733EC86A64521F2B18177B200C"+
			"BBE117577A615D6C770988C0BAD946E208E24FA074E5AB31"+
			"43DB5BFCE0FD108E4B82D120A92108011A723C12A787E6D7"+
			"88719A10BDBA5B2699C327186AF4E23C1A946834B6150BDA"+
			"2583E9CA2AD44CE8DBBBC2DB04DE8EF92E8EFC141FBECAA6"+
			"287C59474E6BC05D99B2964FA090C3A2233BA186515BE7ED"+
			"1F612970CEE2D7AFB81BDD762170481CD0069127D5B05AA9"+
			"93B4EA988D8FDDC186FFB7DC90A6C08F4DF435C934063199"+
			"FFFFFFFFFFFFFFFF", 16), cyclic.NewInt(5),
		cyclic.NewInt(23),
		cyclic.NewRandom(cyclic.NewInt(1), cyclic.NewInt(42)))

	recip, _ := format.NewRecipientPayload(42)
	recip.GetRecipientInitVect().Set(cyclic.NewInt(1))
	payloadMicList := [][]byte{
		recip.GetRecipientInitVect().LeftpadBytes(format.RIV_LEN),
		recip.GetRecipientID().LeftpadBytes(format.RID_LEN),
	}
	recip.GetRecipientMIC().SetBytes(verification.GenerateMIC(payloadMicList,
		format.RMIC_LEN))

	invsprecomp := grp.Inverse(cyclic.NewInt(42), cyclic.NewInt(0))

	encrRecp := grp.Mul(recip.SerializeRecipient(),
		invsprecomp, cyclic.NewInt(0))

	im = append(im, &RealtimeSlot{
		Slot:               0,
		EncryptedRecipient: encrRecp})

	im = append(im, &RealtimeSlot{
		Slot:               1,
		EncryptedRecipient: encrRecp})

	ExpectedOutputs := []*cyclic.Int{cyclic.NewInt(42), cyclic.NewInt(42)}

	dc := services.DispatchCryptop(&grp, Identify{}, nil, nil, round)

	for i := uint64(0); i < batchSize; i++ {
		dc.InChannel <- &im[i]
		trn := <-dc.OutChannel

		rtn := (*trn).(*RealtimeSlot)
		ExpectedOutput := ExpectedOutputs[i]

		if rtn.EncryptedRecipient.Cmp(ExpectedOutput) != 0 {
			t.Errorf("%v - Expected: %v, Got: %v", i, ExpectedOutput.Text(10),
				rtn.EncryptedRecipient.Text(10))
		}
	}
}

// Smoke test test the identify function
func TestIdentifyRun(t *testing.T) {
	keys := KeysIdentify{
		RecipientPrecomputation: cyclic.NewInt(69)}

	grp := cyclic.NewGroup(cyclic.NewIntFromString(
		"FFFFFFFFFFFFFFFFC90FDAA22168C234C4C6628B80DC1CD1"+
			"29024E088A67CC74020BBEA63B139B22514A08798E3404DD"+
			"EF9519B3CD3A431B302B0A6DF25F14374FE1356D6D51C245"+
			"E485B576625E7EC6F44C42E9A637ED6B0BFF5CB6F406B7ED"+
			"EE386BFB5A899FA5AE9F24117C4B1FE649286651ECE45B3D"+
			"C2007CB8A163BF0598DA48361C55D39A69163FA8FD24CF5F"+
			"83655D23DCA3AD961C62F356208552BB9ED529077096966D"+
			"670C354E4ABC9804F1746C08CA18217C32905E462E36CE3B"+
			"E39E772C180E86039B2783A2EC07A28FB5C55DF06F4C52C9"+
			"DE2BCBF6955817183995497CEA956AE515D2261898FA0510"+
			"15728E5A8AAAC42DAD33170D04507A33A85521ABDF1CBA64"+
			"ECFB850458DBEF0A8AEA71575D060C7DB3970F85A6E1E4C7"+
			"ABF5AE8CDB0933D71E8C94E04A25619DCEE3D2261AD2EE6B"+
			"F12FFA06D98A0864D87602733EC86A64521F2B18177B200C"+
			"BBE117577A615D6C770988C0BAD946E208E24FA074E5AB31"+
			"43DB5BFCE0FD108E4B82D120A92108011A723C12A787E6D7"+
			"88719A10BDBA5B2699C327186AF4E23C1A946834B6150BDA"+
			"2583E9CA2AD44CE8DBBBC2DB04DE8EF92E8EFC141FBECAA6"+
			"287C59474E6BC05D99B2964FA090C3A2233BA186515BE7ED"+
			"1F612970CEE2D7AFB81BDD762170481CD0069127D5B05AA9"+
			"93B4EA988D8FDDC186FFB7DC90A6C08F4DF435C934063199"+
			"FFFFFFFFFFFFFFFF", 16), cyclic.NewInt(5),
		cyclic.NewInt(23),
		cyclic.NewRandom(cyclic.NewInt(1), cyclic.NewInt(42)))

	recip, _ := format.NewRecipientPayload(42)
	recip.GetRecipientInitVect().Set(cyclic.NewInt(1))
	payloadMicList := [][]byte{
		recip.GetRecipientInitVect().LeftpadBytes(format.RIV_LEN),
		recip.GetRecipientID().LeftpadBytes(format.RID_LEN),
	}
	recip.GetRecipientMIC().SetBytes(verification.GenerateMIC(payloadMicList,
		format.RMIC_LEN))

	invsprecomp := grp.Inverse(cyclic.NewInt(69), cyclic.NewInt(0))

	encrRecp := grp.Mul(recip.SerializeRecipient(),
		invsprecomp, cyclic.NewInt(0))

	im := RealtimeSlot{
		Slot:               0,
		EncryptedRecipient: encrRecp}

	om := RealtimeSlot{
		Slot:               0,
		EncryptedRecipient: cyclic.NewInt(0)}

	ExpectedOutput := cyclic.NewInt(42)

	// Identify(&grp, EncryptedRecipient, DecryptedRecipient, RecipientPrecomp)
	identify := Identify{}
	identify.Run(&grp, &im, &om, &keys)

	if om.EncryptedRecipient.Cmp(ExpectedOutput) != 0 {
		t.Errorf("Expected: %v, Got: %v", ExpectedOutput.Text(10),
			om.EncryptedRecipient.Text(10))
	}
}
