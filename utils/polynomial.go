package utils

import (
	"crypto/rand"
	"math/big"
)

// Prime field modulus. Using a large prime for security (Secp256k1 order).
// In a real system, this should be configurable.
var Prime, _ = new(big.Int).SetString("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEFFFFFC2F", 16)

// Polynomial represents a univariate polynomial over a finite field.
// Coefficients are in increasing order of degree: a_0 + a_1*x + ... + a_t*x^t
type Polynomial struct {
	Coeffs []*big.Int
}

// Evaluate evaluates the polynomial at x.
func (p *Polynomial) Evaluate(x *big.Int) *big.Int {
	result := big.NewInt(0)
	// Horner's method
	for i := len(p.Coeffs) - 1; i >= 0; i-- {
		result.Mul(result, x)
		result.Add(result, p.Coeffs[i])
		result.Mod(result, Prime)
	}
	return result
}

// SymmetricPolynomial represents a symmetric bivariate polynomial F(x, y).
// F(x, y) = sum_{i,j} C_{ij} * x^i * y^j where C_{ij} = C_{ji}.
type SymmetricPolynomial struct {
	Coeffs [][]*big.Int // Matrix of coefficients
	Degree int
}

// NewRandomSymmetricPolynomial creates a random symmetric polynomial of degree t with F(0,0) = secret.
func NewRandomSymmetricPolynomial(degree int, secret *big.Int) (*SymmetricPolynomial, error) {
	coeffs := make([][]*big.Int, degree+1)
	for i := range coeffs {
		coeffs[i] = make([]*big.Int, degree+1)
	}

	// Set F(0,0) = secret, which corresponds to C_{00}
	coeffs[0][0] = new(big.Int).Set(secret)

	for i := 0; i <= degree; i++ {
		for j := 0; j <= i; j++ { // Fill lower triangle and diagonal
			if i == 0 && j == 0 {
				continue
			}
			randVal, err := rand.Int(rand.Reader, Prime)
			if err != nil {
				return nil, err
			}
			coeffs[i][j] = randVal
			coeffs[j][i] = randVal // Symmetry
		}
	}

	return &SymmetricPolynomial{
		Coeffs: coeffs,
		Degree: degree,
	}, nil
}

// GetUnivariatePolynomial returns f_k(y) = F(k, y).
// This is the polynomial sent to process k.
func (sp *SymmetricPolynomial) GetUnivariatePolynomial(k *big.Int) *Polynomial {
	// f_k(y) = sum_{j=0}^t ( sum_{i=0}^t C_{ij} * k^i ) * y^j
	// The coefficient for y^j is sum_{i=0}^t C_{ij} * k^i

	polyCoeffs := make([]*big.Int, sp.Degree+1)

	for j := 0; j <= sp.Degree; j++ {
		coeffJ := big.NewInt(0)
		for i := 0; i <= sp.Degree; i++ {
			// term = C_{ij} * k^i
			term := new(big.Int).Set(sp.Coeffs[i][j])

			// k^i
			kPowI := new(big.Int).Exp(k, big.NewInt(int64(i)), Prime)

			term.Mul(term, kPowI)
			term.Mod(term, Prime)

			coeffJ.Add(coeffJ, term)
			coeffJ.Mod(coeffJ, Prime)
		}
		polyCoeffs[j] = coeffJ
	}

	return &Polynomial{Coeffs: polyCoeffs}
}

// InterpolateAtZero computes L(0) for the polynomial L passing through (x_i, y_i)
func InterpolateAtZero(xs, ys []*big.Int) *big.Int {
	result := big.NewInt(0)
	k := len(xs)

	for j := 0; j < k; j++ {
		// Compute Lagrange basis polynomial l_j(0)
		// l_j(0) = product_{m!=j} (0 - x_m) / (x_j - x_m)
		//        = product_{m!=j} (-x_m) / (x_j - x_m)

		num := big.NewInt(1)
		den := big.NewInt(1)

		for m := 0; m < k; m++ {
			if m == j {
				continue
			}

			// num *= -x_m
			negXm := new(big.Int).Neg(xs[m])
			num.Mul(num, negXm)
			num.Mod(num, Prime)

			// den *= (x_j - x_m)
			diff := new(big.Int).Sub(xs[j], xs[m])
			den.Mul(den, diff)
			den.Mod(den, Prime)
		}

		// term = y_j * num * den^-1
		term := new(big.Int).Set(ys[j])
		term.Mul(term, num)
		term.Mod(term, Prime)

		denInv := new(big.Int).ModInverse(den, Prime)
		term.Mul(term, denInv)
		term.Mod(term, Prime)

		result.Add(result, term)
		result.Mod(result, Prime)
	}

	// Handle negative result
	if result.Sign() < 0 {
		result.Add(result, Prime)
	}

	return result
}
