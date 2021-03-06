package ring

import (
	"math"
	"math/bits"

	"github.com/ldsec/lattigo/v2/utils"
)

const precision = uint64(56)

type baseSampler struct {
	prng     utils.PRNG
	baseRing *Ring
}

// TernarySampler keeps the state of a polynomial sampler in the ternary distribution.
type TernarySampler struct {
	baseSampler
	matrixProba  [2][precision - 1]uint8
	matrixValues [][3]uint64
	p            float64
	hw           uint64
	sample       func(poly *Poly)
}

// NewTernarySampler creates a new instance of TernarySampler from a PRNG, the ring definition and the distribution
// parameters: p is the probability of a coefficient being 0, (1-p)/2 is the probability of 1 and -1. If "montgomery"
// is set to true, polynomials read from this sampler are in Montgomery form.
func NewTernarySampler(prng utils.PRNG, baseRing *Ring, p float64, montgomery bool) *TernarySampler {
	ternarySampler := new(TernarySampler)
	ternarySampler.baseRing = baseRing
	ternarySampler.prng = prng
	ternarySampler.p = p
	ternarySampler.sample = ternarySampler.sampleProba

	ternarySampler.initializeMatrix(montgomery)

	if p != 0.5 {
		ternarySampler.computeMatrixTernary(p)
	}

	return ternarySampler
}

// NewTernarySamplerSparse creates a new instance of a fixed-hamming-weight TernarySampler from a PRNG, the ring definition and the desired
// hamming weight for the output polynomials. If "montgomery" is set to true, polynomials read from this sampler
// are in Montgomery form.
func NewTernarySamplerSparse(prng utils.PRNG, baseRing *Ring, hw uint64, montgomery bool) *TernarySampler {
	ternarySampler := new(TernarySampler)
	ternarySampler.baseRing = baseRing
	ternarySampler.prng = prng
	ternarySampler.hw = hw
	ternarySampler.sample = ternarySampler.sampleSparse

	ternarySampler.initializeMatrix(montgomery)

	return ternarySampler
}

// Read samples a polynomial into pol.
func (ts *TernarySampler) Read(pol *Poly) {
	ts.sample(pol)
}

// ReadNew allocates and samples a polynomial.
func (ts *TernarySampler) ReadNew() (pol *Poly) {
	pol = ts.baseRing.NewPoly()
	ts.sample(pol)
	return pol
}

func (ts *TernarySampler) initializeMatrix(montgomery bool) {
	ts.matrixValues = make([][3]uint64, len(ts.baseRing.Modulus))

	for i, Qi := range ts.baseRing.Modulus {

		ts.matrixValues[i][0] = 0

		if montgomery {
			ts.matrixValues[i][1] = MForm(1, Qi, ts.baseRing.BredParams[i])
			ts.matrixValues[i][2] = MForm(Qi-1, Qi, ts.baseRing.BredParams[i])
		} else {
			ts.matrixValues[i][1] = 1
			ts.matrixValues[i][2] = Qi - 1
		}
	}
}

func (ts *TernarySampler) computeMatrixTernary(p float64) {
	var g float64
	var x uint64

	g = p
	g *= math.Exp2(float64(precision))
	x = uint64(g)

	for j := uint64(0); j < precision-1; j++ {
		ts.matrixProba[0][j] = uint8((x >> (precision - j - 1)) & 1)
	}

	g = 1 - p
	g *= math.Exp2(float64(precision))
	x = uint64(g)

	for j := uint64(0); j < precision-1; j++ {
		ts.matrixProba[1][j] = uint8((x >> (precision - j - 1)) & 1)
	}

}

func (ts *TernarySampler) sampleProba(pol *Poly) {

	if ts.p == 0 {
		panic("cannot sample -> p = 0")
	}

	var coeff uint64
	var sign uint64
	var index uint64

	if ts.p == 0.5 {

		randomBytesCoeffs := make([]byte, ts.baseRing.N>>3)
		randomBytesSign := make([]byte, ts.baseRing.N>>3)

		ts.prng.Clock(randomBytesCoeffs)

		ts.prng.Clock(randomBytesSign)

		for i := uint64(0); i < ts.baseRing.N; i++ {
			coeff = uint64(uint8(randomBytesCoeffs[i>>3])>>(i&7)) & 1
			sign = uint64(uint8(randomBytesSign[i>>3])>>(i&7)) & 1

			index = (coeff & (sign ^ 1)) | ((sign & coeff) << 1)

			for j := range ts.baseRing.Modulus {
				pol.Coeffs[j][i] = ts.matrixValues[j][index] //(coeff & (sign^1)) | (qi - 1) * (sign & coeff)
			}
		}

	} else {

		randomBytes := make([]byte, ts.baseRing.N)

		pointer := uint8(0)
		bytePointer := uint64(0)

		ts.prng.Clock(randomBytes)

		for i := uint64(0); i < ts.baseRing.N; i++ {

			coeff, sign, randomBytes, pointer, bytePointer = ts.kysampling(ts.prng, randomBytes, pointer, bytePointer, ts.baseRing.N)

			index = (coeff & (sign ^ 1)) | ((sign & coeff) << 1)

			for j := range ts.baseRing.Modulus {
				pol.Coeffs[j][i] = ts.matrixValues[j][index] //(coeff & (sign^1)) | (qi - 1) * (sign & coeff)
			}
		}
	}
}

func (ts *TernarySampler) sampleSparse(pol *Poly) {

	if ts.hw > ts.baseRing.N {
		ts.hw = ts.baseRing.N
	}

	var mask, j uint64
	var coeff uint8

	index := make([]uint64, ts.baseRing.N)
	for i := uint64(0); i < ts.baseRing.N; i++ {
		index[i] = i
	}

	randomBytes := make([]byte, (uint64(math.Ceil(float64(ts.hw) / 8.0)))) // We sample ceil(hw/8) bytes
	pointer := uint8(0)

	ts.prng.Clock(randomBytes)

	for i := uint64(0); i < ts.hw; i++ {
		mask = (1 << uint64(bits.Len64(ts.baseRing.N-i))) - 1 // rejection sampling of a random variable between [0, len(index)]

		j = randInt32(ts.prng, mask)
		for j >= ts.baseRing.N-i {
			j = randInt32(ts.prng, mask)
		}

		coeff = (uint8(randomBytes[0]) >> (i & 7)) & 1 // random binary digit [0, 1] from the random bytes
		for i := range ts.baseRing.Modulus {
			pol.Coeffs[i][index[j]] = ts.matrixValues[i][coeff]
		}

		// Remove the element in position j of the slice (order not preserved)
		index[j] = index[len(index)-1]
		index = index[:len(index)-1]

		pointer++

		if pointer == 8 {
			randomBytes = randomBytes[1:]
			pointer = 0
		}
	}
}

// kysampling uses the binary expansion and random bytes matrix to sample a discrete Gaussian value and its sign.
func (ts *TernarySampler) kysampling(prng utils.PRNG, randomBytes []byte, pointer uint8, bytePointer uint64, byteLength uint64) (uint64, uint64, []byte, uint8, uint64) {

	var sign uint8

	d := 0
	col := 0
	colLen := len(ts.matrixProba)

	for {

		// Use one random byte per cycle and cycle through the randomBytes
		for i := pointer; i < 8; i++ {

			d = (d << 1) + 1 - int((uint8(randomBytes[bytePointer])>>i)&1)

			// There is small probability that it will get out of the bound, then
			// rerun until it gets a proper output
			if d > colLen-1 {
				return ts.kysampling(prng, randomBytes, i, bytePointer, byteLength)
			}

			for row := colLen - 1; row >= 0; row-- {

				d -= int(ts.matrixProba[row][col])

				if d == -1 {

					// Sign
					if i == 7 {
						pointer = 0
						// If the last bit of the array was read, sample a new one
						bytePointer++

						if bytePointer >= byteLength {
							bytePointer = 0
							prng.Clock(randomBytes)
						}

						sign = uint8(randomBytes[bytePointer]) & 1

					} else {
						pointer = i
						// Otherwise, the sign is the next bit of the byte
						sign = uint8(randomBytes[bytePointer]>>(i+1)) & 1
					}

					return uint64(row), uint64(sign), randomBytes, pointer + 1, bytePointer
				}
			}

			col++
		}

		// Reset the bit pointer and discard the used byte
		pointer = 0
		// If the last bit of the array was read, sample a new one
		bytePointer++

		if bytePointer >= byteLength {
			bytePointer = 0
			prng.Clock(randomBytes)
		}

	}
}
