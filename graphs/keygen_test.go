////////////////////////////////////////////////////////////////////////////////
// Copyright © 2018 Privategrity Corporation                                   /
//                                                                             /
// All rights reserved.                                                        /
////////////////////////////////////////////////////////////////////////////////

package graphs

import (
	"bytes"
	"gitlab.com/elixxir/crypto/cryptops"
	"gitlab.com/elixxir/crypto/cyclic"
	"gitlab.com/elixxir/crypto/large"
	"gitlab.com/elixxir/primitives/id"
	"gitlab.com/elixxir/server/globals"
	"gitlab.com/elixxir/server/node"
	"gitlab.com/elixxir/server/services"
	"golang.org/x/crypto/blake2b"
	"runtime"
	"testing"
)

// Give compile error unless KeygenSubStream meets keygenSubStreamInterface
var _ keygenSubStreamInterface = &KeygenSubStream{}

// Example stream that includes a KeygenSubStream and can be put in a graph
type KeygenTestStream struct {
	KeygenSubStream
	// put other parts of the stream here if you have any
}

func (*KeygenTestStream) GetName() string {
	return "KeygenTestStream"
}

func (s *KeygenTestStream) Link(batchSize uint32, source interface{}) {
	round := source.(*node.RoundBuffer)
	// You may have to create these elsewhere and pass them to
	// KeygenSubStream's Link so they can be populated in-place by the
	// CommStream for the graph
	s.KeygenSubStream.LinkStream(round.Grp,
		make([][]byte, batchSize),
		make([]*id.User, batchSize),
		round.Grp.NewIntBuffer(batchSize, round.Grp.NewInt(1)),
		round.Grp.NewIntBuffer(batchSize, round.Grp.NewInt(1)))
}

// Test that triggers error cases in the keygen cryptop adapter
func TestKeygenStreamAdapt_Errors(t *testing.T) {
	// First error: failing type assert for stream
	err := Keygen.Adapt(nil, MockKeygenOp, services.NewChunk(0, 1))
	if err == nil {
		t.Error("Failing the type assert for Adapt should have given an error")
	}

	// Second error: failing type assert for cryptop
	err = Keygen.Adapt(&KeygenTestStream{}, nil, services.NewChunk(0, 1))
	if err == nil {
		t.Error("Failing the type assert for Adapt should have given an error")
	}

	// Third error: Slot includes a user that's not in the registry
	// First, create a keygen stream
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
	// Since the user registry has no users,
	// any user we pass into the stream will cause an error
	globals.Users = &globals.UserMap{}
	var stream KeygenTestStream
	stream.Link(1, &node.RoundBuffer{Grp: grp})
	stream.users[0] = id.ZeroID
	stream.salts[0] = []byte("cesium chloride")
	err = Keygen.Adapt(&stream, MockKeygenOp, services.NewChunk(0, 1))
	if err == nil {
		t.Error("Passing a user ID that wasn't in the DB didn't result in an error")
	}
}

var MockKeygenOp cryptops.KeygenPrototype = func(grp *cyclic.Group, salt []byte, baseKey, key *cyclic.Int) {
	// returns the base key XOR'd with the salt
	// this is the easiest way to ensure both pieces of data are passed to the
	// op from the adapter
	bitLen := baseKey.BitLen()
	// begone compile error
	func(_ int) {}(bitLen)
	x := baseKey.Bytes()
	for i := range x {
		x[i] = salt[i] ^ x[i]
	}
	grp.SetBytes(key, x)
}

// High-level test of the reception keygen adapter
// Also demonstrates how it can be part of a graph that could potentially also
// do other things
func TestKeygenStreamInGraph(t *testing.T) {
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

	// Create a user registry and make a user in it
	// Unfortunately, this has to time out the db connection before the rest
	// of the test can run. It would be nice to have a method that only makes
	// a user map to make tests run faster
	globals.Users = &globals.UserMap{}
	u := globals.Users.NewUser(grp)
	globals.Users.UpsertUser(u)

	// Reception base key should be around 256 bits long,
	// depending on generation, to feed the 256-bit hash
	if u.BaseKey.BitLen() < 250 || u.BaseKey.BitLen() > 256 {
		t.Errorf("Base key has wrong number of bits. "+
			"Had %v bits in reception base key",
			u.BaseKey.BitLen())
	}

	var stream KeygenTestStream
	batchSize := uint32(1)
	//stream.Link(batchSize, &node.RoundBuffer{Grp: grp})

	// make a salt for testing
	testSalt := []byte("sodium chloride")
	// pad to length of the base key
	testSalt = append(testSalt, make([]byte, 256/8-len(testSalt))...)

	hash, err := blake2b.New256(nil)

	if err != nil {
		t.Errorf("Keygen: Test could not get blake2b hash: %s", err.Error())
	}

	hash.Write(testSalt)

	testHashedSalt := hash.Sum(nil)

	PanicHandler := func(err error) {
		t.Errorf("Keygen: Error in adaptor: %s", err.Error())
		return
	}

	gc := services.NewGraphGenerator(4, PanicHandler, uint8(runtime.NumCPU()))

	// run the module in a graph
	g := gc.NewGraph("test", &stream)
	mod := Keygen.DeepCopy()
	mod.Cryptop = MockKeygenOp
	g.First(mod)
	g.Last(mod)
	//Keygen.NumThreads = 1
	g.Build(batchSize, services.AUTO_OUTPUTSIZE, 1.0)
	g.Link(&node.RoundBuffer{Grp: grp})
	// So, it's necessary to fill in the parts in the expanded batch with dummy
	// data to avoid crashing, or we need to exclude those parts in the cryptop
	for i := 0; i < int(g.GetExpandedBatchSize()); i++ {
		// Necessary to avoid crashing
		stream.users[i] = id.ZeroID
		// Not necessary to avoid crashing
		stream.salts[i] = []byte{}

		grp.SetUint64(stream.keysA.Get(uint32(i)), uint64(i))
		grp.SetUint64(stream.keysB.Get(uint32(i)), uint64(1000+i))
		stream.salts[i] = testSalt
		stream.users[i] = u.ID
	}
	// Here's the actual data for the test

	g.Run()
	go g.Send(services.NewChunk(0, g.GetExpandedBatchSize()))

	ok := true
	var chunk services.Chunk

	for ok {
		chunk, ok = g.GetOutput()
		for i := chunk.Begin(); i < chunk.End(); i++ {
			// inspect stream output: XORing the salt with the output should
			// return the original base key
			resultA := stream.keysA.Get(uint32(i))
			resultB := stream.keysB.Get(uint32(i))
			resultABytes := resultA.Bytes()
			resultBBytes := resultB.Bytes()
			// So, why is ResultBytes 256 bytes long,
			// while testSalt is 32 bytes long?
			// retrieve the original base key to prove that both data were passed to
			// the cryptop
			for j := range resultABytes {
				resultABytes[j] = resultABytes[j] ^ testSalt[j]
				resultBBytes[j] = resultBBytes[j] ^ testHashedSalt[j]
			}

			// Check result and base key. They should be equal
			if !bytes.Equal(resultABytes, u.BaseKey.Bytes()) {
				t.Error("Keygen: Base key and result key A weren't equal")
			}

			if !bytes.Equal(resultBBytes, u.BaseKey.Bytes()) {
				t.Error("Keygen: Base key and result key B weren't equal")
			}
		}
	}
}
