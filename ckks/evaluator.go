package ckks

import (
	"errors"
	"github.com/ldsec/lattigo/ring"
	"github.com/ldsec/lattigo/utils"
	"math"
)

// Evaluator is a struct holding the necessary elements to operates the homomorphic operations between ciphertext and/or plaintexts.
// It also holds a small memory pool used to store intermediate computations.
type Evaluator struct {
	params        *Parameters
	ckksContext   *Context
	ringpool      [6]*ring.Poly
	keyswitchpool [4]*ring.Poly

	poolQ [3]*ring.Poly
	poolP [3]*ring.Poly

	ctxpool     *Ciphertext
	rescalepool []uint64

	baseconverter *ring.FastBasisExtender
	decomposer    *ring.ArbitraryDecomposer
}

// NewEvaluator creates a new Evaluator, that can be used to do homomorphic
// operations on the ciphertexts and/or plaintexts. It stores a small pool of polynomials
// and ciphertexts that will be used for intermediate values.
func NewEvaluator(params *Parameters) (evaluator *Evaluator) {
	evaluator = new(Evaluator)

	evaluator.params = params.Copy()

	evaluator.ckksContext = NewContext(params)

	for i := 0; i < 5; i++ {
		evaluator.ringpool[i] = evaluator.ckksContext.contextQ.NewPoly()
	}

	for i := 0; i < 4; i++ {
		evaluator.keyswitchpool[i] = evaluator.ckksContext.contextKeys.NewPoly()
	}

	for i := 0; i < 3; i++ {
		evaluator.poolQ[i] = evaluator.ckksContext.contextQ.NewPoly()
		evaluator.poolP[i] = evaluator.ckksContext.contextP.NewPoly()
	}

	evaluator.rescalepool = make([]uint64, evaluator.ckksContext.n)

	evaluator.ctxpool = NewCiphertextFromParams(params, 1, evaluator.ckksContext.levels-1, evaluator.ckksContext.scale)

	evaluator.baseconverter = ring.NewFastBasisExtender(evaluator.ckksContext.contextQ.Modulus, evaluator.ckksContext.contextP.Modulus)

	evaluator.decomposer = ring.NewArbitraryDecomposer(evaluator.ckksContext.contextQ.Modulus, evaluator.ckksContext.contextP.Modulus)

	return evaluator
}

func (evaluator *Evaluator) getElemAndCheckBinary(op0, op1, opOut Operand, opOutMinDegree uint64) (el0, el1, elOut *ckksElement) {
	if op0 == nil || op1 == nil || opOut == nil {
		panic("operands cannot be nil")
	}

	if op0.Degree()+op1.Degree() == 0 {
		panic("operands cannot be both plaintext")
	}

	if opOut.Degree() < opOutMinDegree {
		panic("receiver operand degree is too small")
	}
	el0, el1, elOut = op0.Element(), op1.Element(), opOut.Element()
	return // TODO: more checks on elements
}

func (evaluator *Evaluator) getElemAndCheckUnary(op0, opOut Operand, opOutMinDegree uint64) (el0, elOut *ckksElement) {
	if op0 == nil || opOut == nil {
		panic("operand cannot be nil")
	}

	if op0.Degree() == 0 {
		panic("operand cannot be plaintext")
	}

	if opOut.Degree() < opOutMinDegree {
		panic("receiver operand degree is too small")
	}
	el0, elOut = op0.Element(), opOut.Element()
	return // TODO: more checks on elements
}

func (evaluator *Evaluator) newCiphertextBinary(op0, op1 Operand) (ctOut *Ciphertext) {

	maxDegree := utils.MaxUint64(op0.Degree(), op1.Degree())
	maxScale := utils.MaxFloat64(op0.Scale(), op1.Scale())
	minLevel := utils.MinUint64(op0.Level(), op1.Level())

	return NewCiphertextFromParams(evaluator.params, maxDegree, minLevel, maxScale)
}

// Add adds op0 to op1 and returns the result on ctOut.
func (evaluator *Evaluator) Add(op0, op1 Operand, ctOut *Ciphertext) {
	el0, el1, elOut := evaluator.getElemAndCheckBinary(op0, op1, ctOut, utils.MaxUint64(op0.Degree(), op1.Degree()))
	evaluator.evaluateInPlace(el0, el1, elOut, evaluator.ckksContext.contextQ.AddLvl)
}

// AddNoMod adds op0 to op1 and returns the result on ctOut, without modular reduction.
func (evaluator *Evaluator) AddNoMod(op0, op1 Operand, ctOut *Ciphertext) {
	el0, el1, elOut := evaluator.getElemAndCheckBinary(op0, op1, ctOut, utils.MaxUint64(op0.Degree(), op1.Degree()))
	evaluator.evaluateInPlace(el0, el1, elOut, evaluator.ckksContext.contextQ.AddNoModLvl)
}

// AddNew adds op0 to op1 and returns the result on a newly created element.
func (evaluator *Evaluator) AddNew(op0, op1 Operand) (ctOut *Ciphertext) {
	ctOut = evaluator.newCiphertextBinary(op0, op1)
	evaluator.Add(op0, op1, ctOut)
	return
}

// AddNoModNew adds op0 to op1 without modular reduction, and returns the result on a newly created element.
func (evaluator *Evaluator) AddNoModNew(op0, op1 Operand) (ctOut *Ciphertext) {
	ctOut = evaluator.newCiphertextBinary(op0, op1)
	evaluator.AddNoMod(op0, op1, ctOut)
	return
}

// Sub subtracts op0 to op1 and returns the result on ctOut.
func (evaluator *Evaluator) Sub(op0, op1 Operand, ctOut *Ciphertext) {

	el0, el1, elOut := evaluator.getElemAndCheckBinary(op0, op1, ctOut, utils.MaxUint64(op0.Degree(), op1.Degree()))

	evaluator.evaluateInPlace(el0, el1, elOut, evaluator.ckksContext.contextQ.SubLvl)

	level := utils.MinUint64(utils.MinUint64(el0.Level(), el1.Level()), elOut.Level())

	if el0.Degree() < el1.Degree() {
		for i := el0.Degree() + 1; i < el1.Degree()+1; i++ {
			evaluator.ckksContext.contextQ.NegLvl(level, elOut.Value()[i], elOut.Value()[i])
		}
	}

}

// SubNoMod subtracts op0 to op1 and returns the result on ctOut, without modular reduction.
func (evaluator *Evaluator) SubNoMod(op0, op1 Operand, ctOut *Ciphertext) {

	el0, el1, elOut := evaluator.getElemAndCheckBinary(op0, op1, ctOut, utils.MaxUint64(op0.Degree(), op1.Degree()))

	evaluator.evaluateInPlace(el0, el1, elOut, evaluator.ckksContext.contextQ.SubNoModLvl)

	level := utils.MinUint64(utils.MinUint64(el0.Level(), el1.Level()), elOut.Level())

	if el0.Degree() < el1.Degree() {
		for i := el0.Degree() + 1; i < el1.Degree()+1; i++ {
			evaluator.ckksContext.contextQ.NegLvl(level, elOut.Value()[i], elOut.Value()[i])
		}
	}

}

// SubNew subtracts op0 to op1 and returns the result on a newly created element.
func (evaluator *Evaluator) SubNew(op0, op1 Operand) (ctOut *Ciphertext) {
	ctOut = evaluator.newCiphertextBinary(op0, op1)
	evaluator.Sub(op0, op1, ctOut)
	return
}

// SubNoModNew subtracts op0 to op1 without modular reduction, and returns the result on a newly created element.
func (evaluator *Evaluator) SubNoModNew(op0, op1 Operand) (ctOut *Ciphertext) {
	ctOut = evaluator.newCiphertextBinary(op0, op1)
	evaluator.SubNoMod(op0, op1, ctOut)
	return
}

func (evaluator *Evaluator) evaluateInPlace(c0, c1, ctOut *ckksElement, evaluate func(uint64, *ring.Poly, *ring.Poly, *ring.Poly)) {

	var tmp0, tmp1 *ckksElement // TODO : use evaluator mem pool

	level := utils.MinUint64(utils.MinUint64(c0.Level(), c1.Level()), ctOut.Level())

	maxDegree := utils.MaxUint64(c0.Degree(), c1.Degree())
	minDegree := utils.MinUint64(c0.Degree(), c1.Degree())

	// Else resizes the receiver element
	ctOut.Resize(evaluator.params, maxDegree)
	evaluator.DropLevel(ctOut.Ciphertext(), ctOut.Level()-utils.MinUint64(c0.Level(), c1.Level()))

	// Checks wether or not the receiver element is the same as one of the input elements
	// and acts accordingly to avoid unnecessary element creation or element overwriting,
	// and scales properly the element before the evaluation.
	if ctOut == c0 {

		if c0.Scale() > c1.Scale() {

			tmp1 = evaluator.ctxpool.Element()

			if uint64(c0.Scale()/c1.Scale()) != 0 {
				evaluator.MultByConst(c1.Ciphertext(), uint64(c0.Scale()/c1.Scale()), tmp1.Ciphertext())
			}

		} else if c1.Scale() > c0.Scale() {

			if uint64(c1.Scale()/c0.Scale()) != 0 {
				evaluator.MultByConst(c0.Ciphertext(), uint64(c1.Scale()/c0.Scale()), c0.Ciphertext())
			}

			c0.SetScale(c1.Scale())

			tmp1 = c1

		} else {

			tmp1 = c1
		}

		tmp0 = c0

	} else if ctOut == c1 {

		if c1.Scale() > c0.Scale() {

			tmp0 = evaluator.ctxpool.Element()
			if uint64(c1.Scale()/c0.Scale()) != 0 {
				evaluator.MultByConst(c0.Ciphertext(), uint64(c1.Scale()/c0.Scale()), tmp0.Ciphertext())
			}

		} else if c0.Scale() > c1.Scale() {

			if uint64(c0.Scale()/c1.Scale()) != 0 {
				evaluator.MultByConst(c1.Ciphertext(), uint64(c0.Scale()/c1.Scale()), ctOut.Ciphertext())
			}

			ctOut.SetScale(c0.Scale())

			tmp0 = c0

		} else {

			tmp0 = c0
		}

		tmp1 = c1

	} else {

		if c1.Scale() > c0.Scale() {

			tmp0 = evaluator.ctxpool.Element()

			if uint64(c1.Scale()/c0.Scale()) != 0 {
				evaluator.MultByConst(c0.Ciphertext(), uint64(c1.Scale()/c0.Scale()), tmp0.Ciphertext())
			}

			tmp1 = c1

		} else if c0.Scale() > c1.Scale() {

			tmp1 = evaluator.ctxpool.Element()

			if uint64(c0.Scale()/c1.Scale()) != 0 {
				evaluator.MultByConst(c1.Ciphertext(), uint64(c0.Scale()/c1.Scale()), tmp1.Ciphertext())
			}

			tmp0 = c0

		} else {
			tmp0 = c0
			tmp1 = c1
		}
	}

	for i := uint64(0); i < minDegree+1; i++ {
		evaluate(level, tmp0.Value()[i], tmp1.Value()[i], ctOut.Value()[i])
	}

	ctOut.SetScale(utils.MaxFloat64(c0.Scale(), c1.Scale()))

	// If the inputs degree differ, copies the remaining degree on the receiver
	// Also checks that the receiver is ont one of the inputs to avoid unnecessary work.

	if c0.Degree() > c1.Degree() && tmp0 != ctOut {
		for i := minDegree + 1; i < maxDegree+1; i++ {
			evaluator.ckksContext.contextQ.CopyLvl(level, tmp0.Value()[i], ctOut.Value()[i])
		}
	} else if c1.Degree() > c0.Degree() && tmp1 != ctOut {
		for i := minDegree + 1; i < maxDegree+1; i++ {
			evaluator.ckksContext.contextQ.CopyLvl(level, tmp1.Value()[i], ctOut.Value()[i])
		}
	}
}

// Neg negates the ct0 and returns the result on ctOut.
func (evaluator *Evaluator) Neg(ct0 *Ciphertext, ctOut *Ciphertext) {

	level := utils.MinUint64(ct0.Level(), ctOut.Level())

	if ct0.Degree() != ctOut.Degree() {
		panic("cannot negate -> invalid receiver ciphertext does not match input ciphertext degree")
	}

	for i := range ct0.value {
		evaluator.ckksContext.contextQ.NegLvl(level, ct0.value[i], ctOut.Value()[i])
	}
}

// NegNew negates ct0 and returns the result on a newly created element.
func (evaluator *Evaluator) NegNew(ct0 *Ciphertext) (ctOut *Ciphertext) {
	ctOut = NewCiphertextFromParams(evaluator.params, ct0.Degree(), ct0.Level(), ct0.Scale())
	evaluator.Neg(ct0, ctOut)
	return
}

// AddConstNew adds the input constant (which can be an uint64, int64, float64 or complex128) to ct0 and returns the result on a new element.
func (evaluator *Evaluator) AddConstNew(ct0 *Ciphertext, constant interface{}) (ctOut *Ciphertext) {
	ctOut = ct0.CopyNew().Ciphertext()
	evaluator.AddConst(ct0, constant, ctOut)
	return ctOut
}

// AddConst adds the input constant (which can be an uint64, int64, float64 or complex128) to ct0 and returns the result on ctOut.
func (evaluator *Evaluator) AddConst(ct0 *Ciphertext, constant interface{}, ctOut *Ciphertext) {

	var level uint64

	level = utils.MinUint64(ct0.Level(), ctOut.Level())

	var cReal, cImag float64

	switch constant.(type) {
	case complex128:
		cReal = real(constant.(complex128))
		cImag = imag(constant.(complex128))

	case float64:
		cReal = constant.(float64)
		cImag = float64(0)

	case uint64:
		cReal = float64(constant.(uint64))
		cImag = float64(0)

	case int64:
		cReal = float64(constant.(int64))
		cImag = float64(0)

	case int:
		cReal = float64(constant.(int))
		cImag = float64(0)
	}

	var scaledConst, scaledConstReal, scaledConstImag uint64

	context := evaluator.ckksContext.contextQ

	// Component wise addition of the following vector to the ciphertext :
	// [a + b*psi_qi^2, ....., a + b*psi_qi^2, a - b*psi_qi^2, ...., a - b*psi_qi^2] mod Qi
	// [{                  N/2                }{                N/2               }]
	// Which is equivalent outside of the NTT domain of adding a to the first coefficient of ct0 and b to the N/2th coefficient of ct0.
	var qi uint64
	for i := uint64(0); i < level+1; i++ {
		scaledConstReal = 0
		scaledConstImag = 0
		scaledConst = 0

		qi = context.Modulus[i]

		if cReal != 0 {
			scaledConstReal = scaleUpExact(cReal, ct0.Scale(), qi)
			scaledConst = scaledConstReal
		}

		if cImag != 0 {
			scaledConstImag = ring.MRed(scaleUpExact(cImag, ct0.Scale(), qi), context.GetNttPsi()[i][1], qi, context.GetMredParams()[i])
			scaledConst = ring.CRed(scaledConst+scaledConstImag, qi)
		}

		p1tmp := ctOut.Value()[0].Coeffs[i]
		p0tmp := ct0.value[0].Coeffs[i]

		for j := uint64(0); j < evaluator.ckksContext.n>>1; j++ {
			p1tmp[j] = ring.CRed(p0tmp[j]+scaledConst, qi)
		}

		if cImag != 0 {
			scaledConst = ring.CRed(scaledConstReal+(qi-scaledConstImag), qi)
		}

		for j := evaluator.ckksContext.n >> 1; j < evaluator.ckksContext.n; j++ {
			p1tmp[j] = ring.CRed(p0tmp[j]+scaledConst, qi)
		}
	}
}

// MultByConstAndAdd multiplies ct0 by the input constant, and adds it to the receiver element (does not modify the input
// element), ex. ctOut(x) = ctOut(x) + ct0(x) * (a+bi). This functions  removes the need of storing the intermediate value c(x) * (a+bi).
// This function will modifie the level and the scale of the receiver element depending on the level and the scale of the input
// element and the type of the constant. The level of the receiver element will be set to min(input.level, receiver.level).
// The scale of the receiver element will be set to the scale that the input element would have after the multiplication by the constant.
func (evaluator *Evaluator) MultByConstAndAdd(ct0 *Ciphertext, constant interface{}, ctOut *Ciphertext) {

	var level uint64

	level = utils.MinUint64(ct0.Level(), ctOut.Level())

	// Forces a drop of cOut level to ct0 level
	if ctOut.Level() > level {
		evaluator.DropLevel(ctOut, ctOut.Level()-level)
	}

	var cReal, cImag float64
	var scale float64

	// Converts to float64 and determines if a scale is required (which is the case if either real or imag has a rational part)
	scale = 1
	switch constant.(type) {
	case complex128:
		cReal = real(constant.(complex128))
		cImag = imag(constant.(complex128))

		if cReal != 0 {
			valueInt := int64(cReal)
			valueFloat := cReal - float64(valueInt)

			if valueFloat != 0 {
				scale = evaluator.ckksContext.scale
			}
		}

		if cImag != 0 {
			valueInt := int64(cImag)
			valueFloat := cImag - float64(valueInt)

			if valueFloat != 0 {
				scale = evaluator.ckksContext.scale
			}
		}

	case float64:
		cReal = constant.(float64)
		cImag = float64(0)

		if cReal != 0 {
			valueInt := int64(cReal)
			valueFloat := cReal - float64(valueInt)

			if valueFloat != 0 {
				scale = evaluator.ckksContext.scale
			}
		}

	case uint64:
		cReal = float64(constant.(uint64))
		cImag = float64(0)

	case int64:
		cReal = float64(constant.(int64))
		cImag = float64(0)

	case int:
		cReal = float64(constant.(int))
		cImag = float64(0)
	}

	var scaledConst, scaledConstReal, scaledConstImag uint64

	context := evaluator.ckksContext.contextQ

	// If a scaling will be required to multiply by the constant,
	// equalizes scales such that the scales match in the end.
	if scale != 1 {

		// If ctOut scaling is smaller than ct0's scale + the default scaling,
		// then brings ctOut scale to ct0's scale.
		if ctOut.Scale() < ct0.Scale()*evaluator.ckksContext.scale {

			if uint64((evaluator.ckksContext.scale*ct0.Scale())/ctOut.Scale()) != 0 {

				evaluator.MultByConst(ctOut, uint64((evaluator.ckksContext.scale*ct0.Scale())/ctOut.Scale()), ctOut)

			}

			ctOut.SetScale(evaluator.ckksContext.scale * ct0.Scale())

			// If ctOut.Scale() > ((a+bi)*scale)*ct0(x), then sets the scale to
			// bring c(x)*scale to the level of ctOut(x) scale
		} else if ctOut.Scale() > ct0.Scale()*evaluator.ckksContext.scale {
			scale = ctOut.Scale() / ct0.Scale()
		}

		// If no scaling is required, the sets the appropriate scale such that
		// ct0(x)*scale matches ctOut(x) scale without modifiying ct0(x) scale.
	} else {

		if ctOut.Scale() > ct0.Scale() {

			scale = ctOut.Scale() / ct0.Scale()

		} else if ct0.Scale() > ctOut.Scale() {

			if uint64(ct0.Scale()/ctOut.Scale()) != 0 {
				evaluator.MultByConst(ctOut, ct0.Scale()/ctOut.Scale(), ctOut)
			}

			ctOut.SetScale(ct0.Scale())
		}
	}

	// Component wise multiplication of the following vector to the ciphertext :
	// [a + b*psi_qi^2, ....., a + b*psi_qi^2, a - b*psi_qi^2, ...., a - b*psi_qi^2] mod Qi
	// [{                  N/2                }{                N/2               }]
	// Which is equivalent outside of the NTT domain of adding a to the first coefficient of ct0 and b to the N/2th coefficient of ct0.
	for i := uint64(0); i < level+1; i++ {

		qi := context.Modulus[i]
		mredParams := context.GetMredParams()[i]
		bredParams := context.GetBredParams()[i]

		scaledConstReal = 0
		scaledConstImag = 0
		scaledConst = 0

		if cReal != 0 {
			scaledConstReal = scaleUpExact(cReal, scale, qi)
			scaledConst = scaledConstReal
		}

		if cImag != 0 {
			scaledConstImag = scaleUpExact(cImag, scale, qi)
			scaledConstImag = ring.MRed(scaledConstImag, context.GetNttPsi()[i][1], qi, mredParams)
			scaledConst = ring.CRed(scaledConst+scaledConstImag, qi)
		}

		scaledConst = ring.MForm(scaledConst, qi, bredParams)

		for u := range ct0.Value() {
			p0tmp := ct0.Value()[u].Coeffs[i]
			p1tmp := ctOut.Value()[u].Coeffs[i]
			for j := uint64(0); j < evaluator.ckksContext.n>>1; j++ {
				p1tmp[j] = ring.CRed(p1tmp[j]+ring.MRed(p0tmp[j], scaledConst, qi, mredParams), qi)
			}
		}

		if cImag != 0 {
			scaledConst = ring.CRed(scaledConstReal+(qi-scaledConstImag), qi)
			scaledConst = ring.MForm(scaledConst, qi, bredParams)
		}

		for u := range ct0.Value() {
			p0tmp := ct0.Value()[u].Coeffs[i]
			p1tmp := ctOut.Value()[u].Coeffs[i]
			for j := evaluator.ckksContext.n >> 1; j < evaluator.ckksContext.n; j++ {
				p1tmp[j] = ring.CRed(p1tmp[j]+ring.MRed(p0tmp[j], scaledConst, qi, mredParams), qi)
			}
		}
	}
}

// MultByConstNew multiplies ct0 by the input constant and returns the result on a newly created element.
// The scale of the output element will depend on the scale of the input element and the constant (if the constant
// needs to be scaled (its rational part is not zero)). The constant can be an uint64, int64, float64 or complex128.
func (evaluator *Evaluator) MultByConstNew(ct0 *Ciphertext, constant interface{}) (ctOut *Ciphertext) {
	ctOut = NewCiphertextFromParams(evaluator.params, ct0.Degree(), ct0.Level(), ct0.Scale())
	evaluator.MultByConst(ct0, constant, ctOut)
	return
}

// MultByConst multiplies ct0 by the input constant and returns the result on ctOut.
// The scale of the output element will depend on the scale of the input element and the constant (if the constant
// needs to be scaled (its rational part is not zero)). The constant can be an uint64, int64, float64 or complex128.
func (evaluator *Evaluator) MultByConst(ct0 *Ciphertext, constant interface{}, ctOut *Ciphertext) {

	var level uint64

	level = utils.MinUint64(ct0.Level(), ctOut.Level())

	var cReal, cImag float64
	var scale float64

	// Converts to float64 and determines if a scale is required (which is the case if either real or imag has a rational part)
	scale = 1
	switch constant.(type) {
	case complex128:
		cReal = real(constant.(complex128))
		cImag = imag(constant.(complex128))

		if cReal != 0 {
			valueInt := int64(cReal)
			valueFloat := cReal - float64(valueInt)

			if valueFloat != 0 {
				scale = evaluator.ckksContext.scale
			}
		}

		if cImag != 0 {
			valueInt := int64(cImag)
			valueFloat := cImag - float64(valueInt)

			if valueFloat != 0 {
				scale = evaluator.ckksContext.scale
			}
		}

	case float64:
		cReal = constant.(float64)
		cImag = float64(0)

		if cReal != 0 {
			valueInt := int64(cReal)
			valueFloat := cReal - float64(valueInt)

			if valueFloat != 0 {
				scale = evaluator.ckksContext.scale
			}
		}

	case uint64:
		cReal = float64(constant.(uint64))
		cImag = float64(0)

	case int64:
		cReal = float64(constant.(int64))
		cImag = float64(0)

	case int:
		cReal = float64(constant.(int))
		cImag = float64(0)
	}

	// Component wise multiplication of the following vector to the ciphertext :
	// [a + b*psi_qi^2, ....., a + b*psi_qi^2, a - b*psi_qi^2, ...., a - b*psi_qi^2] mod Qi
	// [{                  N/2                }{                N/2               }]
	// Which is equivalent outside of the NTT domain of adding a to the first coefficient of ct0 and b to the N/2th coefficient of ct0.
	context := evaluator.ckksContext.contextQ
	var scaledConst, scaledConstReal, scaledConstImag uint64
	for i := uint64(0); i < level+1; i++ {

		qi := context.Modulus[i]
		bredParams := context.GetBredParams()[i]
		mredParams := context.GetMredParams()[i]

		scaledConstReal = 0
		scaledConstImag = 0
		scaledConst = 0

		if cReal != 0 {
			scaledConstReal = scaleUpExact(cReal, scale, qi)
			scaledConst = scaledConstReal
		}

		if cImag != 0 {
			scaledConstImag = scaleUpExact(cImag, scale, qi)
			scaledConstImag = ring.MRed(scaledConstImag, context.GetNttPsi()[i][1], qi, mredParams)
			scaledConst = ring.CRed(scaledConst+scaledConstImag, qi)
		}

		scaledConst = ring.MForm(scaledConst, qi, bredParams)

		for u := range ct0.Value() {
			p0tmp := ct0.Value()[u].Coeffs[i]
			p1tmp := ctOut.Value()[u].Coeffs[i]
			for j := uint64(0); j < evaluator.ckksContext.n>>1; j++ {
				p1tmp[j] = ring.MRed(p0tmp[j], scaledConst, qi, mredParams)
			}
		}

		if cImag != 0 {
			scaledConst = ring.CRed(scaledConstReal+(qi-scaledConstImag), qi)
			scaledConst = ring.MForm(scaledConst, qi, bredParams)
		}

		for u := range ct0.Value() {
			p0tmp := ct0.Value()[u].Coeffs[i]
			p1tmp := ctOut.Value()[u].Coeffs[i]
			for j := evaluator.ckksContext.n >> 1; j < evaluator.ckksContext.n; j++ {
				p1tmp[j] = ring.MRed(p0tmp[j], scaledConst, qi, mredParams)
			}
		}
	}

	ctOut.SetScale(ct0.Scale() * scale)
}

// MultByiNew multiplies ct0 by the imaginary number i, and returns the result on a newly created element.
// Does not change the scale.
func (evaluator *Evaluator) MultByiNew(ct0 *Ciphertext) (ctOut *Ciphertext) {
	ctOut = NewCiphertextFromParams(evaluator.params, 1, ct0.Level(), ct0.Scale())
	evaluator.MultByi(ct0, ctOut)
	return ctOut
}

// MultByi multiplies ct0 by the imaginary number i, and returns the result on c1.
// Does not change the scale.
func (evaluator *Evaluator) MultByi(ct0 *Ciphertext, ct1 *Ciphertext) {

	var level uint64

	level = utils.MinUint64(ct0.Level(), ct1.Level())

	context := evaluator.ckksContext.contextQ

	var imag uint64

	// Equivalent to a mult by monomial x^(n/2) outside of the NTT domain
	for i := uint64(0); i < level+1; i++ {

		qi := context.Modulus[i]
		mredParams := context.GetMredParams()[i]

		imag = context.GetNttPsi()[i][1] // Psi^2

		for u := range ct1.value {
			p0tmp := ct0.value[u].Coeffs[i]
			p1tmp := ct1.value[u].Coeffs[i]
			for j := uint64(0); j < context.N>>1; j++ {
				p1tmp[j] = ring.MRed(p0tmp[j], imag, qi, mredParams)
			}
		}

		imag = qi - imag

		for u := range ct1.value {
			p0tmp := ct0.value[u].Coeffs[i]
			p1tmp := ct1.value[u].Coeffs[i]
			for j := context.N >> 1; j < context.N; j++ {
				p1tmp[j] = ring.MRed(p0tmp[j], imag, qi, mredParams)

			}
		}
	}
}

// DivByiNew multiplies ct0 by the imaginary number 1/i = -i, and returns the result on a newly created element.
// Does not change the scale.
func (evaluator *Evaluator) DivByiNew(ct0 *Ciphertext) (ctOut *Ciphertext) {
	ctOut = NewCiphertextFromParams(evaluator.params, 1, ct0.Level(), ct0.Scale())
	evaluator.DivByi(ct0, ctOut)
	return
}

// DivByi multiplies ct0 by the imaginary number 1/i = -i, and returns the result on c1.
// Does not change the scale.
func (evaluator *Evaluator) DivByi(ct0 *Ciphertext, ct1 *Ciphertext) {

	var level uint64

	level = utils.MinUint64(ct0.Level(), ct1.Level())

	context := evaluator.ckksContext.contextQ

	var imag uint64

	// Equivalent to a mult by monomial x^(3*n/2) outside of the NTT domain
	for i := uint64(0); i < level+1; i++ {

		qi := context.Modulus[i]
		mredParams := context.GetMredParams()[i]

		imag = qi - context.GetNttPsi()[i][1] // -Psi^2

		for u := range ct1.value {
			p0tmp := ct0.value[u].Coeffs[i]
			p1tmp := ct1.value[u].Coeffs[i]
			for j := uint64(0); j < context.N>>1; j++ {
				p1tmp[j] = ring.MRed(p0tmp[j], imag, qi, mredParams)
			}
		}

		imag = context.GetNttPsi()[i][1] // Psi^2

		for u := range ct1.value {
			p0tmp := ct0.value[u].Coeffs[i]
			p1tmp := ct1.value[u].Coeffs[i]
			for j := context.N >> 1; j < context.N; j++ {
				p1tmp[j] = ring.MRed(p0tmp[j], imag, qi, mredParams)
			}
		}
	}
}

// ScaleUpNew multiplies ct0 by 2^scale and sets its scale to its previous scale
// plus 2^n. Returns the result on a newly created element.
func (evaluator *Evaluator) ScaleUpNew(ct0 *Ciphertext, scale float64) (ctOut *Ciphertext) {
	ctOut = NewCiphertextFromParams(evaluator.params, ct0.Degree(), ct0.Level(), ct0.Scale())
	evaluator.ScaleUp(ct0, scale, ctOut)
	return
}

// ScaleUp multiplies ct0 by 2^scale and sets its scale to its previous scale
// plus 2^n. Returns the result on ctOut.
func (evaluator *Evaluator) ScaleUp(ct0 *Ciphertext, scale float64, ctOut *Ciphertext) {
	evaluator.MultByConst(ct0, uint64(scale), ctOut)
	ctOut.SetScale(ct0.Scale() * scale)
	return
}

// MulByPow2New multiplies the ct0 by 2^pow2 and returns the result on a newly created element.
func (evaluator *Evaluator) MulByPow2New(ct0 *Ciphertext, pow2 uint64) (ctOut *Ciphertext) {
	ctOut = NewCiphertextFromParams(evaluator.params, ct0.Degree(), ct0.Level(), ct0.Scale())
	evaluator.MulByPow2(ct0.Element(), pow2, ctOut.Element())
	return
}

// MulByPow2 multiplies ct0 by 2^pow2 and returns the result on ctOut.
func (evaluator *Evaluator) MulByPow2(ct0 *ckksElement, pow2 uint64, ctOut *ckksElement) {
	var level uint64
	level = utils.MinUint64(ct0.Level(), ctOut.Level())
	for i := range ctOut.Value() {
		evaluator.ckksContext.contextQ.MulByPow2Lvl(level, ct0.value[i], pow2, ctOut.Value()[i])
	}
}

// ReduceNew applies a modular reduction ct0 and returns the result on a newly created element.
// To be used in conjonction with function not applying modular reduction.
func (evaluator *Evaluator) ReduceNew(ct0 *Ciphertext) (ctOut *Ciphertext) {

	ctOut = NewCiphertextFromParams(evaluator.params, ct0.Degree(), ct0.Level(), ct0.Scale())

	_ = evaluator.Reduce(ct0, ctOut)

	return ctOut
}

// Reduce applies a modular reduction ct0 and returns the result on ctOut.
// To be used in conjonction with function not applying modular reduction.
func (evaluator *Evaluator) Reduce(ct0 *Ciphertext, ctOut *Ciphertext) error {

	if ct0.Degree() != ctOut.Degree() {
		return errors.New("cannot reduce -> receiver ciphertext does not match input ciphertext degree")
	}

	for i := range ct0.value {
		evaluator.ckksContext.contextQ.ReduceLvl(utils.MinUint64(ct0.Level(), ctOut.Level()), ct0.value[i], ctOut.value[i])
	}

	return nil
}

// DropLevelNew reduces the level of ct0 by levels and returns the result on a newly created element.
// No rescaling is applied during this procedure.
func (evaluator *Evaluator) DropLevelNew(ct0 *Ciphertext, levels uint64) (ctOut *Ciphertext) {
	ctOut = ct0.CopyNew().Ciphertext()
	evaluator.DropLevel(ctOut, levels)
	return
}

// DropLevel reduces the level of ct0 by levels and returns the result on ct0.
// No rescaling is applied during this procedure.
func (evaluator *Evaluator) DropLevel(ct0 *Ciphertext, levels uint64) (err error) {

	if ct0.Level() == 0 {
		return errors.New("cannot drop level -> ciphertext already at level 0")
	}

	level := ct0.Level()

	for i := range ct0.value {
		ct0.value[i].Coeffs = ct0.value[i].Coeffs[:level+1-levels]
	}

	return nil
}

// RescaleNew divides ct0 by the last modulus in the modulus chain, repeats this
// procedure (each time consuming a level) until the scale reaches the original scale or would go below it, and returns the result
// on a newly created element. Since all the moduli in the modulus chain are generated to be close to the
// original scale, this procedure is equivalement to dividing the input element by the scale and adding
// some error.
func (evaluator *Evaluator) RescaleNew(ct0 *Ciphertext, threshold float64) (ctOut *Ciphertext, err error) {

	ctOut = NewCiphertextFromParams(evaluator.params, ct0.Degree(), ct0.Level(), ct0.Scale())

	return ctOut, evaluator.Rescale(ct0, threshold, ctOut)
}

// Rescale divides ct0 by the last modulus in the modulus chain, repeats this
// procedure (each time consuming a level) until the scale reaches the original scale or would go below it, and returns the result
// on c1. Since all the moduli in the modulus chain are generated to be close to the
// original scale, this procedure is equivalement to dividing the input element by the scale and adding
// some error.
func (evaluator *Evaluator) Rescale(ct0 *Ciphertext, threshold float64, c1 *Ciphertext) (err error) {

	ringContext := evaluator.ckksContext.contextQ

	if ct0.Level() == 0 {
		return errors.New("cannot rescale -> input ciphertext already at level 0")
	}

	if ct0.Level() != c1.Level() {
		panic("cannot rescale -> reciever ciphertext does not match input ciphertext level")
	}

	if ct0.Scale() >= (threshold*float64(ringContext.Modulus[c1.Level()]))/2 {

		if !ct0.IsNTT() {
			panic("cannot rescale -> input ciphertext not in NTT")
		}

		c1.Copy(ct0.Element())

		for c1.Scale() >= (threshold*float64(ringContext.Modulus[c1.Level()]))/2 && c1.Level() != 0 {

			c1.DivScale(float64(ringContext.Modulus[c1.Level()]))

			for i := range c1.Value() {
				evaluator.ckksContext.contextQ.DivRoundByLastModulusNTT(c1.Value()[i])
			}

		}

	} else {
		c1.Copy(ct0.Element())
	}

	return nil
}

// RescaleMany applies Rescale several times in a row on the input Ciphertext.
func (evaluator *Evaluator) RescaleMany(ct0 *Ciphertext, nbRescales uint64, c1 *Ciphertext) (err error) {

	if ct0.Level() < nbRescales {
		return errors.New("cannot rescale many -> input ciphertext level too low")
	}

	if ct0.Level() != c1.Level() {
		panic("cannot rescale many -> reciever ciphertext does not match input ciphertext level")
	}

	if !ct0.IsNTT() {
		panic("cannot rescale many -> input ciphertext not in NTT")
	}

	c1.Copy(ct0.Element())

	for i := uint64(0); i < nbRescales; i++ {
		c1.DivScale(float64(evaluator.ckksContext.contextQ.Modulus[c1.Level()-i]))
	}

	for i := range c1.Value() {
		evaluator.ckksContext.contextQ.DivRoundByLastModulusManyNTT(c1.Value()[i], nbRescales)
	}

	return nil
}

// MulRelinNew multiplies ct0 by ct1 and returns the result on a newly created element. The new scale is
// the multiplication between scales of the input elements (addition when the scale is represented in log2). An evaluation
// key can be provided to apply a relinearization step and reduce the degree of the output element. This evaluation key is only
// required when the two inputs elements are ciphertexts. If not evaluationkey is provided and the input elements are two ciphertexts,
// the resulting ciphertext will be of degree two. This function only accepts plaintexts (degree zero) and/or ciphertexts of degree one.
func (evaluator *Evaluator) MulRelinNew(op0, op1 Operand, evakey *EvaluationKey) (ctOut *Ciphertext) {

	ctOut = NewCiphertextFromParams(evaluator.params, 1, utils.MinUint64(op0.Level(), op1.Level()), op0.Scale()+op1.Scale())
	evaluator.MulRelin(op0, op1, evakey, ctOut)

	return ctOut
}

// MulRelin multiplies ct0 by ct1 and returns the result on ctOut. The new scale is
// the multiplication between scales of the input elements (addition when the scale is represented in log2). An evaluation
// key can be provided to apply a relinearization step and reduce the degree of the output element. This evaluation key is only
// required when the two inputs elements are ciphertexts. If not evaluationkey is provided and the input elements are two ciphertexts,
// the resulting ciphertext will be of degree two. This function only accepts plaintexts (degree zero) and/or ciphertexts of degree one.
func (evaluator *Evaluator) MulRelin(op0, op1 Operand, evakey *EvaluationKey, ctOut *Ciphertext) {

	el0, el1, elOut := evaluator.getElemAndCheckBinary(op0, op1, ctOut, utils.MaxUint64(op0.Degree(), op1.Degree()))

	level := utils.MinUint64(utils.MinUint64(el0.Level(), el1.Level()), elOut.Level())

	if ctOut.Level() > level {
		evaluator.DropLevel(elOut.Ciphertext(), elOut.Level()-level)
	}

	if el0.Degree() > 1 || el1.Degree() > 1 {
		panic("cannot mul -> input element and output element must be of degree 0 or 1")
	}

	if !el0.IsNTT() {
		panic("cannot mul -> op0 must be in NTT to multiply")
	}

	if !el1.IsNTT() {
		panic("cannot mul -> op1 must be in NTT to multiply")
	}

	elOut.SetScale(el0.Scale() * el1.Scale())

	context := evaluator.ckksContext.contextQ

	var c00, c01, c0, c1, c2 *ring.Poly

	// Case Ciphertext (x) Ciphertext
	if el0.Degree()+el1.Degree() == 2 {

		c00 = evaluator.ringpool[0]
		c01 = evaluator.ringpool[1]

		// If the receiver ciphertext is neither of the inptus,
		// we can write directly on it.
		if elOut != el0 && elOut != el1 {

			c0 = elOut.value[0]
			c1 = elOut.value[1]

			// If the evaluation key is nil and we can write directly on the receiver, then
			// resizes the cipher text to a degree 2 ciphertext
			if evakey == nil {

				elOut.Resize(evaluator.params, 2)
				c2 = elOut.value[2]

				// If there is however an evaluation key, then
				// we still use the mempool for the third element
			} else {

				c2 = evaluator.ringpool[4]
			}

			// If the receiver ciphertext either one of the inputs,
			// then makes use of the mempool for the three elements
		} else {

			c0 = evaluator.ringpool[2]
			c1 = evaluator.ringpool[3]
			c2 = evaluator.ringpool[4]
		}

		context.MFormLvl(level, el0.value[0], c00)
		context.MFormLvl(level, el0.value[1], c01)

		if el0 == el1 { // squaring case

			context.MulCoeffsMontgomeryLvl(level, c00, el1.value[0], c0) // c0 = c[0]*c[0]
			context.MulCoeffsMontgomeryLvl(level, c00, el1.value[1], c1) // c1 = 2*c[0]*c[1]
			context.AddLvl(level, c1, c1, c1)
			context.MulCoeffsMontgomeryLvl(level, c01, el1.value[1], c2) // c2 = c[1]*c[1]

		} else { // regular case

			context.MulCoeffsMontgomeryLvl(level, c00, el1.value[0], c0) // c0 = c0[0]*c0[0]
			context.MulCoeffsMontgomeryLvl(level, c00, el1.value[1], c1)
			context.MulCoeffsMontgomeryAndAddNoModLvl(level, c01, el1.value[0], c1) // c1 = c0[0]*c1[1] + c0[1]*c1[0]
			context.MulCoeffsMontgomeryLvl(level, c01, el1.value[1], c2)            // c2 = c0[1]*c1[1]
		}

		// Relinearize if a key was provided
		if evakey != nil {

			context.CopyLvl(level, c0, elOut.value[0])
			context.CopyLvl(level, c1, elOut.value[1])

			evaluator.switchKeysInPlace(c2, evakey.evakey, elOut.Ciphertext())

		} else { // Or copies the result on the output ciphertext if it was one of the inputs
			if elOut == el0 || elOut == el1 {
				elOut.Resize(evaluator.params, 2)
				context.CopyLvl(level, c0, elOut.value[0])
				context.CopyLvl(level, c1, elOut.value[1])
				context.CopyLvl(level, c2, elOut.value[2])
			}
		}

		// Case Plaintext (x) Ciphertext or Ciphertext (x) Plaintext
	} else {

		var tmp0, tmp1 *ckksElement

		if el0.Degree() == 1 {
			tmp0, tmp1 = el1, el0
		} else {
			tmp0, tmp1 = el0, el1
		}

		c00 := evaluator.ringpool[0]
		c00.Zero()

		context.MFormLvl(level, tmp0.value[0], c00)
		context.MulCoeffsMontgomeryLvl(level, c00, tmp1.value[0], elOut.value[0])
		context.MulCoeffsMontgomeryLvl(level, c00, tmp1.value[1], elOut.value[1])
	}
}

// RelinearizeNew applies the relinearization procedure on ct0 and returns the result on a newly
// created ciphertext. Requires the input ciphertext to be of degree two.
func (evaluator *Evaluator) RelinearizeNew(ct0 *Ciphertext, evakey *EvaluationKey) (ctOut *Ciphertext) {
	ctOut = NewCiphertextFromParams(evaluator.params, 1, ct0.Level(), ct0.Scale())
	evaluator.Relinearize(ct0, evakey, ctOut)
	return
}

// Relinearize applies the relinearization procedure on ct0 and returns the result on ctOut. Requires the input ciphertext to be of degree two.
func (evaluator *Evaluator) Relinearize(ct0 *Ciphertext, evakey *EvaluationKey, ctOut *Ciphertext) {
	if ct0.Degree() != 2 {
		panic("cannot relinearize -> input is not of degree 2")
	}

	if ctOut != ct0 {
		ctOut.SetScale(ct0.Scale())
	}

	level := utils.MinUint64(ct0.Level(), ctOut.Level())
	context := evaluator.ckksContext.contextQ

	context.CopyLvl(level, ct0.value[0], ctOut.value[0])
	context.CopyLvl(level, ct0.value[1], ctOut.value[1])

	evaluator.switchKeysInPlace(ct0.value[2], evakey.evakey, ctOut)

	ctOut.Resize(evaluator.params, 1)
}

// SwitchKeysNew re-encrypts ct0 under a different key and returns the result on a newly created element.
// Requires a switchinkey, which is computed from the key under which the ciphertext is currently encrypted,
// and the key under which the ciphertext will be re-encrypted.
func (evaluator *Evaluator) SwitchKeysNew(ct0 *Ciphertext, switchingKey *SwitchingKey) (ctOut *Ciphertext) {
	ctOut = NewCiphertextFromParams(evaluator.params, ct0.Degree(), ct0.Level(), ct0.Scale())
	evaluator.SwitchKeys(ct0, switchingKey, ctOut)
	return
}

// SwitchKeys re-encrypts ct0 under a different key and returns the result on ctOut.
// Requires a switchinkey, which is computed from the key under which the ciphertext is currently encrypted,
// and the key under which the ciphertext will be re-encrypted.
func (evaluator *Evaluator) SwitchKeys(ct0 *Ciphertext, switchingKey *SwitchingKey, ctOut *Ciphertext) {

	if ct0.Degree() != 1 || ctOut.Degree() != 1 {
		panic("cannot switchkeys -> input and output ciphertext must be of degree 1")
	}

	level := utils.MinUint64(ct0.Level(), ctOut.Level())
	context := evaluator.ckksContext.contextQ

	context.CopyLvl(level, ct0.value[0], ctOut.value[0])
	context.CopyLvl(level, ct0.value[1], ctOut.value[1])

	evaluator.switchKeysInPlace(ct0.value[1], switchingKey, ctOut)
}

// RotateColumnsNew rotates the columns of ct0 by k position to the left, and returns the result on a newly created element.
// If the provided element is a ciphertext, a keyswitching operation is necessary and a rotation key for the specific rotation needs to be provided.
func (evaluator *Evaluator) RotateColumnsNew(ct0 *Ciphertext, k uint64, evakey *RotationKeys) (ctOut *Ciphertext) {
	ctOut = NewCiphertextFromParams(evaluator.params, ct0.Degree(), ct0.Level(), ct0.Scale())
	evaluator.RotateColumns(ct0, k, evakey, ctOut)
	return
}

// RotateColumns rotates the columns of ct0 by k position to the left and returns the result on the provided receiver.
// If the provided element is a ciphertext, a keyswitching operation is necessary and a rotation key for the specific rotation needs to be provided.
func (evaluator *Evaluator) RotateColumns(ct0 *Ciphertext, k uint64, evakey *RotationKeys, ctOut *Ciphertext) {

	if ct0.Degree() != 1 || ctOut.Degree() != 1 {
		panic("cannot rotate -> input and output ciphertext must be of degree 1")
	}

	k &= ((evaluator.ckksContext.n >> 1) - 1)

	if k == 0 {

		ctOut.Copy(ct0.Element())

	} else {

		ctOut.SetScale(ct0.Scale())

		// Looks in the RotationKeys if the corresponding rotation has been generated
		if evakey.evakeyRotColLeft[k] != nil {

			evaluator.permuteNTT(ct0, evakey.permuteNTTLeftIndex[k], evakey.evakeyRotColLeft[k], ctOut)

		} else {

			// If not looks if the left and right pow2 rotations have been generated
			hasPow2Rotations := true
			for i := uint64(1); i < evaluator.ckksContext.n>>1; i <<= 1 {
				if evakey.evakeyRotColLeft[i] == nil || evakey.evakeyRotColRight[i] == nil {
					hasPow2Rotations = false
					break
				}
			}

			// If yes, computes the least amount of rotation between left and right required to apply the demanded rotation
			if hasPow2Rotations {

				if utils.HammingWeight64(k) <= utils.HammingWeight64((evaluator.ckksContext.n>>1)-k) {
					evaluator.rotateColumnsLPow2(ct0, k, evakey, ctOut)
				} else {
					evaluator.rotateColumnsRPow2(ct0, (evaluator.ckksContext.n>>1)-k, evakey, ctOut)
				}

				// Else returns an error indicating that the keys have not been generated
			} else {
				panic("cannot rotate -> specific rotation and pow2 rotations have not been generated")
			}
		}
	}
}

// RotateHoisted takes an input ciphertext and a list of rotations and returns a map of ciphertext, where each element of the map is the input ciphertext
// rotation by on element of the list. Much faster than sequential calls of RotateColumns.
func (evaluator *Evaluator) RotateHoisted(ctIn *Ciphertext, rotations []uint64, rotkeys *RotationKeys) (cOut map[uint64]*Ciphertext) {

	// Pre-computation for rotations using hoisting
	contextQ := evaluator.ckksContext.contextQ
	contextP := evaluator.ckksContext.contextP

	c2NTT := ctIn.value[1]
	c2InvNTT := contextQ.NewPoly()
	contextQ.InvNTTLvl(ctIn.Level(), c2NTT, c2InvNTT)

	c2QiQDecomp := make([]*ring.Poly, evaluator.ckksContext.beta)
	c2QiPDecomp := make([]*ring.Poly, evaluator.ckksContext.beta)

	alpha := evaluator.ckksContext.alpha
	beta := uint64(math.Ceil(float64(ctIn.Level()+1) / float64(alpha)))

	for i := uint64(0); i < beta; i++ {
		c2QiQDecomp[i] = contextQ.NewPoly()
		c2QiPDecomp[i] = contextP.NewPoly()
		evaluator.decomposeAndSplitNTT(ctIn.Level(), i, c2NTT, c2InvNTT, c2QiQDecomp[i], c2QiPDecomp[i])
	}

	cOut = make(map[uint64]*Ciphertext)

	for _, i := range rotations {

		i &= ((evaluator.ckksContext.n >> 1) - 1)

		if i == 0 {
			cOut[i] = ctIn.CopyNew().Ciphertext()
		} else {
			cOut[i] = NewCiphertextFromParams(evaluator.params, 1, ctIn.Level(), ctIn.Scale())
			evaluator.switchKeyHoisted(ctIn, c2QiQDecomp, c2QiPDecomp, i, rotkeys, cOut[i])
		}
	}

	return
}

func (evaluator *Evaluator) switchKeyHoisted(ctIn *Ciphertext, c2QiQDecomp, c2QiPDecomp []*ring.Poly, k uint64, evakey *RotationKeys, ctOut *Ciphertext) {

	if ctIn.Degree() != 1 || ctOut.Degree() != 1 {
		panic("cannot rotate hoisted-> input and output ciphertext must be of degree 1")
	}

	k &= (1 << (evaluator.ckksContext.logN - 1)) - 1

	if evakey.permuteNTTLeftIndex[k] == nil {
		panic("cannot rotate hoisted -> specific rotation has not been generated")
	}

	ctOut.SetScale(ctIn.Scale())

	var level, reduce uint64

	level = ctOut.Level()

	contextQ := evaluator.ckksContext.contextQ
	contextP := evaluator.ckksContext.contextP

	if ctIn != ctOut {
		ring.PermuteNTTWithIndex(ctIn.value[0], evakey.permuteNTTLeftIndex[k], evaluator.ringpool[0])
		contextQ.CopyLvl(level, evaluator.ringpool[0], ctOut.value[0])
		ring.PermuteNTTWithIndex(ctIn.value[1], evakey.permuteNTTLeftIndex[k], evaluator.ringpool[0])
		contextQ.CopyLvl(level, evaluator.ringpool[0], ctOut.value[1])
	} else {
		ring.PermuteNTTWithIndex(ctIn.value[0], evakey.permuteNTTLeftIndex[k], ctOut.value[0])
		ring.PermuteNTTWithIndex(ctIn.value[1], evakey.permuteNTTLeftIndex[k], ctOut.value[1])
	}

	for i := range evaluator.poolQ {
		evaluator.poolQ[i].Zero()
	}

	for i := range evaluator.poolP {
		evaluator.poolP[i].Zero()
	}

	c2QiQPermute := evaluator.poolQ[0]
	c2QiPPermute := evaluator.poolP[0]

	pool2Q := evaluator.poolQ[1]
	pool2P := evaluator.poolP[1]

	pool3Q := evaluator.poolQ[2]
	pool3P := evaluator.poolP[2]

	reduce = 0

	alpha := evaluator.ckksContext.alpha
	beta := uint64(math.Ceil(float64(level+1) / float64(alpha)))

	// Key switching with crt decomposition for the Qi
	for i := uint64(0); i < beta; i++ {

		ring.PermuteNTTWithIndex(c2QiQDecomp[i], evakey.permuteNTTLeftIndex[k], c2QiQPermute)
		ring.PermuteNTTWithIndex(c2QiPDecomp[i], evakey.permuteNTTLeftIndex[k], c2QiPPermute)

		contextQ.MulCoeffsMontgomeryAndAddNoModLvl(level, evakey.evakeyRotColLeft[k].evakey[i][0], c2QiQPermute, pool2Q)
		contextQ.MulCoeffsMontgomeryAndAddNoModLvl(level, evakey.evakeyRotColLeft[k].evakey[i][1], c2QiQPermute, pool3Q)

		// We continue with the keyswitch primes.
		for j, keysindex := uint64(0), evaluator.ckksContext.levels; j < uint64(len(evaluator.ckksContext.specialprimes)); j, keysindex = j+1, keysindex+1 {

			pj := contextP.Modulus[j]
			mredParams := contextP.GetMredParams()[j]

			key0 := evakey.evakeyRotColLeft[k].evakey[i][0].Coeffs[keysindex]
			key1 := evakey.evakeyRotColLeft[k].evakey[i][1].Coeffs[keysindex]
			p2tmp := pool2P.Coeffs[j]
			p3tmp := pool3P.Coeffs[j]
			c2tmp := c2QiPPermute.Coeffs[j]

			for y := uint64(0); y < contextP.N; y++ {
				p2tmp[y] += ring.MRed(key0[y], c2tmp[y], pj, mredParams)
				p3tmp[y] += ring.MRed(key1[y], c2tmp[y], pj, mredParams)
			}
		}

		if reduce&7 == 1 {
			contextQ.ReduceLvl(level, pool2Q, pool2Q)
			contextQ.ReduceLvl(level, pool3Q, pool3Q)
			contextP.Reduce(pool2P, pool2P)
			contextP.Reduce(pool3P, pool3P)
		}

		reduce++
	}

	if (reduce-1)&7 != 1 {
		contextQ.ReduceLvl(level, pool2Q, pool2Q)
		contextQ.ReduceLvl(level, pool3Q, pool3Q)
		contextP.Reduce(pool2P, pool2P)
		contextP.Reduce(pool3P, pool3P)
	}

	//Independant of context (parameter : level)
	// Computes pool2Q = pool2Q/pool2P and pool3Q = pool3Q/pool3P
	evaluator.baseconverter.ModDownSplitedNTT(contextQ, contextP, evaluator.ckksContext.rescaleParamsKeys, level, pool2Q, pool2P, pool2Q, evaluator.keyswitchpool[0])
	evaluator.baseconverter.ModDownSplitedNTT(contextQ, contextP, evaluator.ckksContext.rescaleParamsKeys, level, pool3Q, pool3P, pool3Q, evaluator.keyswitchpool[0])

	//Independant of context (parameter : level)
	contextQ.AddLvl(level, ctOut.value[0], pool2Q, ctOut.value[0])
	contextQ.AddLvl(level, ctOut.value[1], pool3Q, ctOut.value[1])
}

func (evaluator *Evaluator) rotateColumnsLPow2(ct0 *Ciphertext, k uint64, evakey *RotationKeys, ctOut *Ciphertext) {
	evaluator.rotateColumnsPow2(ct0, k, evakey.permuteNTTLeftIndex, evakey.evakeyRotColLeft, ctOut)
}

func (evaluator *Evaluator) rotateColumnsRPow2(ct0 *Ciphertext, k uint64, evakey *RotationKeys, ctOut *Ciphertext) {
	evaluator.rotateColumnsPow2(ct0, k, evakey.permuteNTTRightIndex, evakey.evakeyRotColRight, ctOut)
}

func (evaluator *Evaluator) rotateColumnsPow2(ct0 *Ciphertext, k uint64, permuteNTTIndex map[uint64][]uint64, evakeyRotCol map[uint64]*SwitchingKey, ctOut *Ciphertext) {

	var evakeyIndex uint64

	evakeyIndex = 1

	level := utils.MinUint64(ct0.Level(), ctOut.Level())
	context := evaluator.ckksContext.contextQ

	context.CopyLvl(level, ct0.value[0], ctOut.value[0])
	context.CopyLvl(level, ct0.value[1], ctOut.value[1])

	for k > 0 {

		if k&1 == 1 {

			evaluator.permuteNTT(ctOut, permuteNTTIndex[evakeyIndex], evakeyRotCol[evakeyIndex], ctOut)
		}

		evakeyIndex <<= 1
		k >>= 1
	}
}

// ConjugateNew conjugates ct0 (which is equivalement to a row rotation) and returns the result on a newly
// created element. If the provided element is a ciphertext, a keyswitching operation is necessary and a rotation key
// for the row rotation needs to be provided.
func (evaluator *Evaluator) ConjugateNew(ct0 *Ciphertext, evakey *RotationKeys) (ctOut *Ciphertext) {
	ctOut = NewCiphertextFromParams(evaluator.params, ct0.Degree(), ct0.Level(), ct0.Scale())
	evaluator.Conjugate(ct0, evakey, ctOut)
	return
}

// Conjugate conjugates c0 (which is equivalement to a row rotation) and returns the result on c1.
// If the provided element is a ciphertext, a keyswitching operation is necessary and a rotation key for the row rotation needs to be provided.
func (evaluator *Evaluator) Conjugate(ct0 *Ciphertext, evakey *RotationKeys, ctOut *Ciphertext) {

	if ct0.Degree() != 1 || ctOut.Degree() != 1 {
		panic("cannot rotate -> input and output ciphertext must be of degree 1")
	}

	if evakey.evakeyConjugate == nil {
		panic("cannot rotate -> : rows rotation key not generated")
	}

	ctOut.SetScale(ct0.Scale())

	evaluator.permuteNTT(ct0, evakey.permuteNTTConjugateIndex, evakey.evakeyConjugate, ctOut)
}

func (evaluator *Evaluator) permuteNTT(ct0 *Ciphertext, index []uint64, evakey *SwitchingKey, ctOut *Ciphertext) {

	var el0, el1 *ring.Poly

	if ct0 != ctOut {
		el0, el1 = ctOut.value[0], ctOut.value[1]
	} else {
		el0, el1 = evaluator.ringpool[0], evaluator.ringpool[1]
	}

	ring.PermuteNTTWithIndex(ct0.value[0], index, el0)
	ring.PermuteNTTWithIndex(ct0.value[1], index, el1)

	level := utils.MinUint64(ct0.Level(), ctOut.Level())
	context := evaluator.ckksContext.contextQ

	context.CopyLvl(level, el0, ctOut.value[0])
	context.CopyLvl(level, el1, ctOut.value[1])

	evaluator.switchKeysInPlace(ctOut.value[1], evakey, ctOut)
}

func (evaluator *Evaluator) switchKeysInPlace(cx *ring.Poly, evakey *SwitchingKey, ctOut *Ciphertext) {
	// Applies the general keyswitching procedure of the form [c0 + cx*evakey[0], c1 + cx*evakey[1]]
	var level, reduce uint64

	level = ctOut.Level()

	contextQ := evaluator.ckksContext.contextQ
	contextP := evaluator.ckksContext.contextP

	for i := range evaluator.poolQ {
		evaluator.poolQ[i].Zero()
	}

	for i := range evaluator.poolP {
		evaluator.poolP[i].Zero()
	}

	c2QiQ := evaluator.poolQ[0]
	c2QiP := evaluator.poolP[0]

	//c2_tmp := contextkeys.NewPoly()
	c2 := evaluator.keyswitchpool[0]

	pool2Q := evaluator.poolQ[1]
	pool2P := evaluator.poolP[1]

	pool3Q := evaluator.poolQ[2]
	pool3P := evaluator.poolP[2]

	// We switch the element on which the switching key operation will be conducted out of the NTT domain

	//Independant of context (parameter : level)
	contextQ.InvNTTLvl(level, cx, c2)

	reduce = 0

	alpha := evaluator.ckksContext.alpha
	beta := uint64(math.Ceil(float64(level+1) / float64(alpha)))

	// Key switching with crt decomposition for the Qi
	for i := uint64(0); i < beta; i++ {

		evaluator.decomposeAndSplitNTT(level, i, cx, c2, c2QiQ, c2QiP)

		contextQ.MulCoeffsMontgomeryAndAddNoModLvl(level, evakey.evakey[i][0], c2QiQ, pool2Q)
		contextQ.MulCoeffsMontgomeryAndAddNoModLvl(level, evakey.evakey[i][1], c2QiQ, pool3Q)

		// We continue with the keyswitch primes.
		for j, keysindex := uint64(0), evaluator.ckksContext.levels; j < uint64(len(evaluator.ckksContext.specialprimes)); j, keysindex = j+1, keysindex+1 {

			pj := contextP.Modulus[j]
			mredParams := contextP.GetMredParams()[j]

			key0 := evakey.evakey[i][0].Coeffs[keysindex]
			key1 := evakey.evakey[i][1].Coeffs[keysindex]
			c2tmp := c2QiP.Coeffs[j]
			p2tmp := pool2P.Coeffs[j]
			p3tmp := pool3P.Coeffs[j]

			for y := uint64(0); y < contextP.N; y++ {
				p2tmp[y] += ring.MRed(key0[y], c2tmp[y], pj, mredParams)
				p3tmp[y] += ring.MRed(key1[y], c2tmp[y], pj, mredParams)
			}
		}

		if reduce&7 == 1 {
			contextQ.ReduceLvl(level, pool2Q, pool2Q)
			contextQ.ReduceLvl(level, pool3Q, pool3Q)
			contextP.Reduce(pool2P, pool2P)
			contextP.Reduce(pool3P, pool3P)
		}

		reduce++
	}

	//Independant of context (parameter : level)
	if (reduce-1)&7 != 1 {
		contextQ.ReduceLvl(level, pool2Q, pool2Q)
		contextQ.ReduceLvl(level, pool3Q, pool3Q)
		contextP.Reduce(pool2P, pool2P)
		contextP.Reduce(pool3P, pool3P)
	}

	//Independant of context (parameter : level)
	// Computes pool2Q = pool2Q/pool2P and pool3Q = pool3Q/pool3P
	evaluator.baseconverter.ModDownSplitedNTT(contextQ, contextP, evaluator.ckksContext.rescaleParamsKeys, level, pool2Q, pool2P, pool2Q, evaluator.keyswitchpool[0])
	evaluator.baseconverter.ModDownSplitedNTT(contextQ, contextP, evaluator.ckksContext.rescaleParamsKeys, level, pool3Q, pool3P, pool3Q, evaluator.keyswitchpool[0])

	//Independant of context (parameter : level)
	contextQ.AddLvl(level, ctOut.value[0], pool2Q, ctOut.value[0])
	contextQ.AddLvl(level, ctOut.value[1], pool3Q, ctOut.value[1])
}

func (evaluator *Evaluator) decomposeAndSplitNTT(level, beta uint64, c2NTT, c2InvNTT, c2QiQ, c2QiP *ring.Poly) {

	// Decomposes the input polynomial into the target CRT basis.

	contextQ := evaluator.ckksContext.contextQ
	contextP := evaluator.ckksContext.contextP

	evaluator.decomposer.DecomposeAndSplit(level, beta, c2InvNTT, c2QiQ, c2QiP)

	p0idxst := beta * evaluator.ckksContext.alpha
	p0idxed := p0idxst + evaluator.decomposer.Xalpha()[beta]

	// c2_qi = cx mod qi mod qi
	for x := uint64(0); x < level+1; x++ {

		qi := contextQ.Modulus[x]
		nttPsi := contextQ.GetNttPsi()[x]
		bredParams := contextQ.GetBredParams()[x]
		mredParams := contextQ.GetMredParams()[x]

		if p0idxst <= x && x < p0idxed {
			p0tmp := c2NTT.Coeffs[x]
			p1tmp := c2QiQ.Coeffs[x]
			for j := uint64(0); j < contextQ.N; j++ {
				p1tmp[j] = p0tmp[j]
			}
		} else {
			ring.NTT(c2QiQ.Coeffs[x], c2QiQ.Coeffs[x], contextQ.N, nttPsi, qi, mredParams, bredParams)
		}
	}
	// c2QiP = c2 mod qi mod pj
	contextP.NTT(c2QiP, c2QiP)
}
