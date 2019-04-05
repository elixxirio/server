package precomputation

import (
	"fmt"
	"gitlab.com/elixxir/crypto/cryptops"
	"gitlab.com/elixxir/crypto/cyclic"
	"gitlab.com/elixxir/crypto/large"
	"gitlab.com/elixxir/server/graphs"
	"gitlab.com/elixxir/server/node"
	"gitlab.com/elixxir/server/services"
	"testing"
)

//Test that DecryptStream.GetName() returns the correct name
func TestDecryptStream_GetName(t *testing.T) {
	expected := "PrecompDecryptStream"

	ds := DecryptStream{}

	if ds.GetName() != expected {
		t.Errorf("DecryptStream.GetName(), Expected %s, Recieved %s", expected, ds.GetName())
	}
}

//Test that DecryptStream.Link() Links correctly
func TestDecryptStream_Link(t *testing.T) {
	primeString := "FFFFFFFFFFFFFFFFC90FDAA22168C234C4C6628B80DC1CD1" +
		"29024E088A67CC74020BBEA63B139B22514A08798E3404DD" +
		"EF9519B3CD3A431B302B0A6DF25F14374FE1356D6D51C245" +
		"E485B576625E7EC6F44C42E9A637ED6B0BFF5CB6F406B7ED" +
		"EE386BFB5A899FA5AE9F24117C4B1FE649286651ECE45B3D" +
		"C2007CB8A163BF0598DA48361C55D39A69163FA8FD24CF5F" +
		"83655D23DCA3AD961C62F356208552BB9ED529077096966D" +
		"670C354E4ABC9804F1746C08CA18217C32905E462E36CE3B" +
		"E39E772C180E86039B2783A2EC07A28FB5C55DF06F4C52C9" +
		"DE2BCBF6955817183995497CEA956AE515D2261898FA0510" +
		"15728E5A8AACAA68FFFFFFFFFFFFFFFF"
	grp := cyclic.NewGroup(large.NewIntFromString(primeString, 16), large.NewInt(2), large.NewInt(1283))

	ds := DecryptStream{}

	batchSize := uint32(100)

	round := node.NewRound(grp, 1, batchSize, batchSize)

	ds.Link(batchSize, round)

	checkStreamIntBuffer(grp, ds.R, round.R, "R", t)
	checkStreamIntBuffer(grp, ds.U, round.U, "U", t)
	checkStreamIntBuffer(grp, ds.R, round.R, "Y_R", t)
	checkStreamIntBuffer(grp, ds.U, round.U, "Y_U", t)

	checkIntBuffer(ds.KeysMsg, batchSize, "KeysMsg", grp.NewInt(1), t)
	checkIntBuffer(ds.CypherMsg, batchSize, "CypherMsg", grp.NewInt(1), t)
	checkIntBuffer(ds.KeysAD, batchSize, "KeysAD", grp.NewInt(1), t)
	checkIntBuffer(ds.CypherAD, batchSize, "CypherAD", grp.NewInt(1), t)
}

func TestDecryptGraph(t *testing.T) {
	primeString := "FFFFFFFFFFFFFFFFC90FDAA22168C234C4C6628B80DC1CD1" +
		"29024E088A67CC74020BBEA63B139B22514A08798E3404DD" +
		"EF9519B3CD3A431B302B0A6DF25F14374FE1356D6D51C245" +
		"E485B576625E7EC6F44C42E9A637ED6B0BFF5CB6F406B7ED" +
		"EE386BFB5A899FA5AE9F24117C4B1FE649286651ECE45B3D" +
		"C2007CB8A163BF0598DA48361C55D39A69163FA8FD24CF5F" +
		"83655D23DCA3AD961C62F356208552BB9ED529077096966D" +
		"670C354E4ABC9804F1746C08CA18217C32905E462E36CE3B" +
		"E39E772C180E86039B2783A2EC07A28FB5C55DF06F4C52C9" +
		"DE2BCBF6955817183995497CEA956AE515D2261898FA0510" +
		"15728E5A8AACAA68FFFFFFFFFFFFFFFF"
	grp := cyclic.NewGroup(large.NewIntFromString(primeString, 16), large.NewInt(2), large.NewInt(1283))

	batchSize := uint32(100)

	expectedName := "PrecompDecrypt"

	//Show that the Inti function meets the function type
	var graphInit graphs.Initializer
	graphInit = InitDecryptGraph

	//Initialize graph
	g := graphInit(func(err error) { return })

	if g.GetName() != expectedName {
		t.Errorf("PrecompDecrypt has incorrect name Expected %s, Recieved %s", expectedName, g.GetName())
	}

	//Build the graph
	g.Build(batchSize)

	//Build the round
	round := node.NewRound(grp, 1, g.GetBatchSize(), g.GetExpandedBatchSize())

	//Link the graph to the round. building the stream object
	g.Link(round)

	stream := g.GetStream().(*DecryptStream)

	//fill the fields of the stream object for testing
	grp.Random(stream.PublicCypherKey)

	for i := uint32(0); i < g.GetExpandedBatchSize(); i++ {
		grp.Random(stream.R.Get(i))
		grp.Random(stream.U.Get(i))
		grp.Random(stream.Y_R.Get(i))
		grp.Random(stream.Y_U.Get(i))
	}

	//Build i/o used for testing
	KeysMsgExpected := grp.NewIntBuffer(g.GetExpandedBatchSize(), grp.NewInt(1))
	CypherMsgExpected := grp.NewIntBuffer(g.GetExpandedBatchSize(), grp.NewInt(1))
	KeysADExpected := grp.NewIntBuffer(g.GetExpandedBatchSize(), grp.NewInt(1))
	CypherADExpected := grp.NewIntBuffer(g.GetExpandedBatchSize(), grp.NewInt(1))

	//Run the graph
	g.Run()

	//Send inputs into the graph
	go func(g *services.Graph) {
		for i := uint32(0); i < g.GetExpandedBatchSize(); i++ {
			g.Send(services.NewChunk(i, i+1))
		}
	}(g)

	//Get the output
	s := g.GetStream().(*DecryptStream)

	for chunk := range g.ChunkDoneChannel() {
		for i := chunk.Begin(); i < chunk.End(); i++ {
			// Compute expected result for this slot
			cryptops.ElGamal(s.Grp, s.R.Get(i), s.Y_R.Get(i), s.PublicCypherKey, KeysMsgExpected.Get(i), CypherMsgExpected.Get(i))
			//Execute elgamal on the keys for the Associated Data
			cryptops.ElGamal(s.Grp, s.U.Get(i), s.Y_U.Get(i), s.PublicCypherKey, KeysADExpected.Get(i), CypherADExpected.Get(i))

			if KeysMsgExpected.Get(i).Cmp(s.KeysMsg.Get(i)) != 0 {
				t.Error(fmt.Sprintf("PrecompDecrypt: Message Keys not equal on slot %v", i))
			}
			if CypherMsgExpected.Get(i).Cmp(s.CypherMsg.Get(i)) != 0 {
				t.Error(fmt.Sprintf("PrecompDecrypt: Message Keys Cypher not equal on slot %v", i))
			}
			if KeysADExpected.Get(i).Cmp(s.KeysAD.Get(i)) != 0 {
				t.Error(fmt.Sprintf("PrecompDecrypt: AD Keys not equal on slot %v", i))
			}
			if CypherADExpected.Get(i).Cmp(s.CypherAD.Get(i)) != 0 {
				t.Error(fmt.Sprintf("PrecompDecrypt: AD Keys Cypher not equal on slot %v", i))
			}
		}
	}
}

func checkStreamIntBuffer(grp *cyclic.Group, ib, sourceib *cyclic.IntBuffer, source string, t *testing.T) {
	if ib.Len() != sourceib.Len() {
		t.Errorf("preomp.DecryptStream.Link: Length of intBuffer %s not correct, "+
			"Expected %v, Recieved: %v", source, sourceib.Len(), ib.Len())
	}

	numBad := 0
	for i := 0; i < sourceib.Len(); i++ {
		grp.SetUint64(sourceib.Get(uint32(i)), uint64(i))
		ci := ib.Get(uint32(i))
		if ci.Cmp(sourceib.Get(uint32(i))) != 0 {
			numBad++
		}
	}

	if numBad != 0 {
		t.Errorf("preomp.DecryptStream.Link: Ints in %v/%v intBuffer %s intilized incorrectly",
			numBad, sourceib.Len(), source)
	}
}

func checkIntBuffer(ib *cyclic.IntBuffer, expandedBatchSize uint32, source string, defaultInt *cyclic.Int, t *testing.T) {
	if ib.Len() != int(expandedBatchSize) {
		t.Errorf("New RoundBuffer: Length of intBuffer %s not correct, "+
			"Expected %v, Recieved: %v", source, expandedBatchSize, ib.Len())
	}

	numBad := 0
	for i := uint32(0); i < expandedBatchSize; i++ {
		ci := ib.Get(i)
		if ci.Cmp(defaultInt) != 0 {
			numBad++
		}
	}

	if numBad != 0 {
		t.Errorf("New RoundBuffer: Ints in %v/%v intBuffer %s intilized incorrectly",
			numBad, expandedBatchSize, source)
	}
}
