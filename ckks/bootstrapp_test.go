package ckks

import (
	"math"
	"math/cmplx"
	"math/rand"
	"testing"
	"time"

	"github.com/ldsec/lattigo/v2/ckks/bettersine"
)

func TestBootstrapp(t *testing.T) {

	rand.Seed(time.Now().UnixNano())

	var err error
	var testContext = new(testParams)

	paramSet := uint64(0)

	shemeParams := DefaultBootstrappSchemeParams[paramSet : paramSet+1]
	bootstrappParams := DefaultBootstrappParams[paramSet : paramSet+1]

	for paramSet := range shemeParams {

		params := shemeParams[paramSet]
		btpParams := bootstrappParams[paramSet]

		q := params.qi[params.MaxLevel()-uint64(len(btpParams.CtSLevel))]

		SineScale := math.Exp2(math.Round(math.Log2(float64(q))))

		// Insecure params for fast testing only
		if testing.Short() {
			params.logN = 14
			params.logSlots = 13
		}

		if testContext, err = genTestParams(params, btpParams.H); err != nil {
			panic(err)
		}

		slots := testContext.params.Slots()

		t.Run(testString(testContext, "ChebySin/"), func(t *testing.T) {

			eval := testContext.evaluator

			DefaultScale := testContext.params.scale

			testContext.params.scale = SineScale
			eval.(*evaluator).scale = SineScale

			deg := 127
			K := float64(15)

			values, _, ciphertext := newTestVectorsSineBootstrapp(testContext, testContext.encryptorSk, -K+1, K-1, t)
			eval.DropLevel(ciphertext, uint64(len(btpParams.CtSLevel))-1)

			cheby := Approximate(sin2pi2pi, -complex(K, 0), complex(K, 0), deg)

			for i := range values {
				values[i] = sin2pi2pi(values[i])
			}

			//fmt.Println(ciphertext.Level() - 1)
			//start := time.Now()
			ciphertext = testContext.evaluator.EvaluateCheby(ciphertext, cheby, testContext.rlk)
			//fmt.Printf("Elapsed : %s \n", time.Since(start))
			//fmt.Println(ciphertext.Level())

			verifyTestVectors(testContext, testContext.decryptor, values, ciphertext, t)

			testContext.params.scale = DefaultScale
			eval.(*evaluator).scale = DefaultScale
		})

		t.Run(testString(testContext, "ChebyCos/"), func(t *testing.T) {

			eval := testContext.evaluator

			DefaultScale := testContext.params.scale

			testContext.params.scale = SineScale
			eval.(*evaluator).scale = SineScale

			K := 21
			deg := 52
			dev := float64(testContext.params.qi[0]) / DefaultScale
			scNum := 2

			scFac := complex(float64(int(1<<scNum)), 0)

			values, _, ciphertext := newTestVectorsSineBootstrapp(testContext, testContext.encryptorSk, float64(-K+1), float64(K-1), t)
			eval.DropLevel(ciphertext, uint64(len(btpParams.CtSLevel))-1)

			cheby := new(ChebyshevInterpolation)
			cheby.coeffs = bettersine.Approximate(K, deg, dev, scNum)
			cheby.maxDeg = cheby.Degree()
			cheby.a = complex(float64(-K), 0) / scFac
			cheby.b = complex(float64(K), 0) / scFac
			cheby.lead = true

			sqrt2pi := math.Pow(0.15915494309189535, 1.0/real(scFac))

			for i := range cheby.coeffs {
				cheby.coeffs[i] *= complex(sqrt2pi, 0)
			}

			for i := range values {

				values[i] = cmplx.Cos(6.283185307179586 * (1 / scFac) * (values[i] - 0.25))

				for j := 0; j < scNum; j++ {
					values[i] = 2*values[i]*values[i] - 1
				}

				values[i] /= 6.283185307179586
			}

			testContext.evaluator.AddConst(ciphertext, -0.25, ciphertext)

			//fmt.Println(ciphertext.Level(), ciphertext.Scale())
			//start := time.Now()
			ciphertext = testContext.evaluator.EvaluateChebySpecial(ciphertext, scFac, cheby, testContext.rlk)
			//fmt.Println(ciphertext.Level(), ciphertext.Scale())

			for i := 0; i < scNum; i++ {
				sqrt2pi *= sqrt2pi
				testContext.evaluator.MulRelin(ciphertext, ciphertext, testContext.rlk, ciphertext)
				testContext.evaluator.Add(ciphertext, ciphertext, ciphertext)
				testContext.evaluator.AddConst(ciphertext, -sqrt2pi, ciphertext)
				testContext.evaluator.Rescale(ciphertext, eval.(*evaluator).scale, ciphertext)
			}

			//fmt.Printf("Elapsed : %s \n", time.Since(start))
			//fmt.Println(ciphertext.Level(), ciphertext.Scale())
			verifyTestVectors(testContext, testContext.decryptor, values, ciphertext, t)

			testContext.params.scale = DefaultScale
			eval.(*evaluator).scale = DefaultScale

		})

		t.Run(testString(testContext, "ChebyCosNaive/"), func(t *testing.T) {

			eval := testContext.evaluator

			DefaultScale := testContext.params.scale

			testContext.params.scale = SineScale
			eval.(*evaluator).scale = SineScale

			K := 257
			deg := 250
			scNum := 3

			scFac := complex(float64(int(1<<scNum)), 0)

			values, _, ciphertext := newTestVectorsSineBootstrapp(testContext, testContext.encryptorSk, float64(-K+1), float64(K-1), t)
			eval.DropLevel(ciphertext, uint64(len(btpParams.CtSLevel))-1)

			cheby := Approximate(cos2pi, -complex(float64(K), 0)/scFac, complex(float64(K), 0)/scFac, deg)

			sqrt2pi := math.Pow(0.15915494309189535, 1.0/real(scFac))

			for i := range cheby.coeffs {
				cheby.coeffs[i] *= complex(sqrt2pi, 0)
			}

			for i := range values {

				values[i] = cmplx.Cos(6.283185307179586 * (1 / scFac) * (values[i] - 0.25))

				for j := 0; j < scNum; j++ {
					values[i] = 2*values[i]*values[i] - 1
				}

				values[i] /= 6.283185307179586
			}

			testContext.evaluator.AddConst(ciphertext, -0.25, ciphertext)

			//fmt.Println(ciphertext.Level(), ciphertext.Scale())
			//start := time.Now()
			ciphertext = testContext.evaluator.EvaluateChebySpecial(ciphertext, scFac, cheby, testContext.rlk)
			//fmt.Println(ciphertext.Level(), ciphertext.Scale())

			for i := 0; i < scNum; i++ {
				sqrt2pi *= sqrt2pi
				testContext.evaluator.MulRelin(ciphertext, ciphertext, testContext.rlk, ciphertext)
				testContext.evaluator.Add(ciphertext, ciphertext, ciphertext)
				testContext.evaluator.AddConst(ciphertext, -sqrt2pi, ciphertext)
				testContext.evaluator.Rescale(ciphertext, eval.(*evaluator).scale, ciphertext)
			}

			//fmt.Printf("Elapsed : %s \n", time.Since(start))
			//fmt.Println(ciphertext.Level(), ciphertext.Scale())
			verifyTestVectors(testContext, testContext.decryptor, values, ciphertext, t)

			testContext.params.scale = DefaultScale
			eval.(*evaluator).scale = DefaultScale

		})

		t.Run(testString(testContext, "Bootstrapp/"), func(t *testing.T) {

			btpKey := testContext.kgen.GenBootstrappingKey(testContext.params.logSlots, btpParams, testContext.sk)
			btp, err := NewBootstrapper(testContext.params, btpParams, btpKey)
			if err != nil {
				panic(err)
			}

			values := make([]complex128, slots)
			for i := range values {
				values[i] = complex(randomFloat(-1, 1), randomFloat(-1, 1))
			}

			values[0] = complex(0.9238795325112867, 0.3826834323650898)
			values[1] = complex(0.9238795325112867, 0.3826834323650898)
			if slots > 2 {
				values[2] = complex(0.9238795325112867, 0.3826834323650898)
				values[3] = complex(0.9238795325112867, 0.3826834323650898)
			}

			plaintext := NewPlaintext(testContext.params, testContext.params.MaxLevel(), testContext.params.scale)
			testContext.encoder.Encode(plaintext, values, slots)

			ciphertext := testContext.encryptorPk.EncryptNew(plaintext)

			for i := 0; i < 1; i++ {

				ciphertext = btp.Bootstrapp(ciphertext)

				//testContext.evaluator.SetScale(ciphertext, testContext.params.scale)

				verifyTestVectors(testContext, testContext.decryptor, values, ciphertext, t)
			}

		})
	}
}

func newTestVectorsSineBootstrapp(testContext *testParams, encryptor Encryptor, a, b float64, t *testing.T) (values []complex128, plaintext *Plaintext, ciphertext *Ciphertext) {

	slots := testContext.params.Slots()

	values = make([]complex128, slots)

	for i := uint64(0); i < slots; i++ {
		values[i] = complex(math.Round(randomFloat(a, b))+randomFloat(-1, 1)/1000, 0)
	}

	plaintext = NewPlaintext(testContext.params, testContext.params.MaxLevel(), testContext.params.Scale())

	testContext.encoder.EncodeNTT(plaintext, values, slots)

	if encryptor != nil {
		ciphertext = encryptor.EncryptNew(plaintext)
	}

	return values, plaintext, ciphertext
}
