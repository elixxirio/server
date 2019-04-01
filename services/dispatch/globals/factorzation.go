package globals

// Do we have to pull in another dependency just for LCM?
import (
	"github.com/cznic/mathutil.git"
	"math"
	"sync"
)

var factorization map[uint32]mathutil.FactorTerms
var factorLock sync.Mutex

func init() {
	factorization = make(map[uint32]mathutil.FactorTerms)
}

func Factor(integer uint32) mathutil.FactorTerms {

	factorLock.Lock()

	terms, ok := factorization[integer]

	if !ok {
		terms = mathutil.FactorInt(integer)
		factorization[integer] = terms
	}

	factorLock.Unlock()

	return terms
}

func LCM(integers []uint32) uint32 {

	fMap := make(map[uint32]uint32)

	for _, i := range integers {

		terms := Factor(i)

		for _, t := range terms {

			power, ok := fMap[t.Prime]

			if !ok || t.Power > power {
				fMap[t.Prime] = t.Power
			}
		}
	}

	lcm := uint32(1)

	for factor, power := range fMap {
		lcm *= mathutil.ModPowUint32(factor, power, math.MaxUint32)
	}

	return lcm
}
