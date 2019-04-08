package node

import (
	"gitlab.com/elixxir/crypto/cyclic"
)

type RoundID uint64

type RoundBuffer struct {
	Grp *cyclic.Group

	id RoundID

	R *cyclic.IntBuffer // First unpermuted internode message key
	S *cyclic.IntBuffer // Permuted internode message key
	T *cyclic.IntBuffer // Second unpermuted internode message key
	U *cyclic.IntBuffer // Permuted *cyclic.Internode recipient key
	V *cyclic.IntBuffer // Unpermuted internode associated data key

	CypherPublicKey *cyclic.Int // Global Cypher Key
	Z               *cyclic.Int // This node's private Cypher Key

	// Private keys for the above
	Y_R *cyclic.IntBuffer
	Y_S *cyclic.IntBuffer
	Y_T *cyclic.IntBuffer
	Y_V *cyclic.IntBuffer
	Y_U *cyclic.IntBuffer

	// Size of batch
	batchSize         uint32
	expandedBatchSize uint32
}

// Function to initialize a new round
func NewRound(g *cyclic.Group, id RoundID, batchsize, expandedBatchSize uint32) *RoundBuffer {

	return &RoundBuffer{
		Grp: g,

		R: g.NewIntBuffer(expandedBatchSize, g.NewInt(1)),
		S: g.NewIntBuffer(expandedBatchSize, g.NewInt(1)),
		T: g.NewIntBuffer(expandedBatchSize, g.NewInt(1)),
		V: g.NewIntBuffer(expandedBatchSize, g.NewInt(1)),
		U: g.NewIntBuffer(expandedBatchSize, g.NewInt(1)),

		CypherPublicKey: g.NewMaxInt(),
		Z:               g.NewMaxInt(),

		Y_R: g.NewIntBuffer(expandedBatchSize, g.NewInt(1)),
		Y_S: g.NewIntBuffer(expandedBatchSize, g.NewInt(1)),
		Y_T: g.NewIntBuffer(expandedBatchSize, g.NewInt(1)),
		Y_V: g.NewIntBuffer(expandedBatchSize, g.NewInt(1)),
		Y_U: g.NewIntBuffer(expandedBatchSize, g.NewInt(1)),

		batchSize:         batchsize,
		expandedBatchSize: expandedBatchSize,
	}
}

func (r *RoundBuffer) GetBatchSize() uint32 {
	return r.batchSize
}

func (r *RoundBuffer) GetExpandedBatchSize() uint32 {
	return r.expandedBatchSize
}