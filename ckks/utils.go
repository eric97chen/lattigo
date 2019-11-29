package ckks

import (
	"github.com/ldsec/lattigo/ring"
	"math/big"
	"math/cmplx"
	"math/rand"
)

func exp2pi(x complex128) complex128 {
	return cmplx.Exp(2 * 3.141592653589793 * complex(0, 1) * x)
}

func randomFloat(min, max float64) float64 {
	return min + rand.Float64()*(max-min)
}

func randomComplex(min, max float64) complex128 {
	return complex(randomFloat(min, max), randomFloat(min, max))
}

func scaleUpExact(value float64, n float64, q uint64) (res uint64) {

	var isNegative bool
	var xFlo *big.Float
	var xInt *big.Int

	isNegative = false
	if value < 0 {
		isNegative = true
		xFlo = big.NewFloat(-n * value)
	} else {
		xFlo = big.NewFloat(n * value)
	}

	xInt = new(big.Int)
	xFlo.Int(xInt)
	xInt.Mod(xInt, ring.NewUint(q))

	res = xInt.Uint64()

	if isNegative {
		res = q - res
	}

	return
}

func scaleUpVecExact(values []float64, n float64, moduli []uint64, coeffs [][]uint64) {

	var isNegative bool
	var xFlo *big.Float
	var xInt *big.Int
	tmp := new(big.Int)

	for i := range values {

		if n*values[i] > 1.8446744073709552e+19 {

			isNegative = false
			if values[i] < 0 {
				isNegative = true
				xFlo = big.NewFloat(-n * values[i])
			} else {
				xFlo = big.NewFloat(n * values[i])
			}

			xInt = new(big.Int)
			xFlo.Int(xInt)

			for j := range moduli {
				tmp.Mod(xInt, ring.NewUint(moduli[j]))
				if isNegative {
					coeffs[j][i] = moduli[j] - tmp.Uint64()
				} else {
					coeffs[j][i] = tmp.Uint64()
				}
			}
		} else {

			if values[i] < 0 {
				for j := range moduli {
					coeffs[j][i] = moduli[j] - (uint64(-n*values[i]) % moduli[j])
				}
			} else {
				for j := range moduli {
					coeffs[j][i] = uint64(n*values[i]) % moduli[j]
				}
			}
		}
	}

	return
}

func modVec(values []*big.Int, q uint64, coeffs []uint64) {
	tmp := new(big.Int)
	for i := range values {
		coeffs[i] = tmp.Mod(values[i], ring.NewUint(q)).Uint64()
	}
}

// Divides x by n^2, returns a float
func scaleDown(coeff *big.Int, n float64) (x float64) {

	x, _ = new(big.Float).SetInt(coeff).Float64()
	x /= n

	return
}

func genBigIntChain(Q []uint64) (bigintChain []*big.Int) {

	bigintChain = make([]*big.Int, len(Q))
	bigintChain[0] = ring.NewUint(Q[0])
	for i := 1; i < len(Q); i++ {
		bigintChain[i] = ring.NewUint(Q[i])
		bigintChain[i].Mul(bigintChain[i], bigintChain[i-1])
	}
	return
}

// genModuli generates the appropriate primes from the parameters using generateCKKSPrimes such that all primes are different.
func genModuli(params *Parameters) (Q []uint64, P []uint64) {

	// Extracts all the different primes bit size and maps their number
	primesbitlen := make(map[uint64]uint64)
	for i, qi := range params.Q {

		primesbitlen[uint64(qi)]++

		if uint64(params.Q[i]) > 60 {
			panic("provided moduli must be smaller than 61")
		}
	}

	for _, pj := range params.P {
		primesbitlen[uint64(pj)]++

		if uint64(pj) > 60 {
			panic("provided P must be smaller than 61")
		}
	}

	// For each bitsize, finds that many primes
	primes := make(map[uint64][]uint64)
	for key, value := range primesbitlen {
		primes[key] = generateCKKSPrimes(key, uint64(params.LogN), value)
	}

	// Assigns the primes to the ckks moduli chain
	Q = make([]uint64, len(params.Q))
	for i, qi := range params.Q {
		Q[i] = primes[uint64(params.Q[i])][0]
		primes[uint64(qi)] = primes[uint64(qi)][1:]
	}

	// Assigns the primes to the special primes list for the the keyscontext
	P = make([]uint64, len(params.P))
	for i, pj := range params.P {
		P[i] = primes[uint64(pj)][0]
		primes[uint64(pj)] = primes[uint64(pj)][1:]
	}

	return Q, P
}

func generateCKKSPrimes(logQ, logN, levels uint64) (primes []uint64) {

	// generateCKKSPrimes generates primes given logQ = size of the primes, logN = size of N and level, the number
	// of levels required. Will return all the appropriate primes, up to the number of level, with the
	// best avaliable deviation from the base power of 2 for the given level.

	if logQ > 60 {
		panic("logQ must be between 1 and 60")
	}

	var x, y, Qpow2, _2N uint64

	primes = []uint64{}

	Qpow2 = 1 << logQ

	_2N = 2 << logN

	x = Qpow2 + 1
	y = Qpow2 + 1

	for true {

		if ring.IsPrime(y) {
			primes = append(primes, y)
			if uint64(len(primes)) == levels {
				return primes
			}
		}

		y -= _2N

		if ring.IsPrime(x) {
			primes = append(primes, x)
			if uint64(len(primes)) == levels {
				return primes
			}
		}

		x += _2N
	}

	return
}

func sliceBitReverseInPlaceComplex128(slice []complex128, N uint64) {

	var bit, j uint64

	for i := uint64(1); i < N; i++ {

		bit = N >> 1

		for j >= bit {
			j -= bit
			bit >>= 1
		}

		j += bit

		if i < j {
			slice[i], slice[j] = slice[j], slice[i]
		}
	}
}

func genSwitchkeysRescalingParams(Q, P []uint64) (params []uint64) {

	params = make([]uint64, len(Q))

	PBig := ring.NewUint(1)
	for _, pj := range P {
		PBig.Mul(PBig, ring.NewUint(pj))
	}

	tmp := ring.NewUint(0)

	for i := 0; i < len(Q); i++ {

		params[i] = tmp.Mod(PBig, ring.NewUint(Q[i])).Uint64()
		params[i] = ring.ModExp(params[i], Q[i]-2, Q[i])
		params[i] = ring.MForm(params[i], Q[i], ring.BRedParams(Q[i]))
	}

	return
}
