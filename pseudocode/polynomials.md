# 2. Data Structures and Polynomials

For IVSS, the system requires support for polynomials over a finite field.

## Finite Field

```pseudo
TYPE FieldElement
│   // Elements in Z_p for large prime p
│   // All arithmetic operations are mod p
└─  p : Prime (e.g., 2^256 - 189)
```

## Symmetric Bivariate Polynomials

### Definition

A bivariate polynomial $F(x, y)$ of degree $t$ is **symmetric** if:

$$F(x, y) = F(y, x) \quad \forall x, y$$

### Properties

```pseudo
CLASS SymmetricBivariatePolynomial
│
├─ degree : Integer = t
│
├─ coefficients : Matrix[t+1][t+1]     // a_{ij} where i+j ≤ t
│   WHERE a_{ij} = a_{ji}              // Symmetry constraint
│
├─ FUNCTION Evaluate(x, y) → FieldElement
│   │   RETURN Σ_{i,j: i+j≤t} a_{ij} · x^i · y^j
│   │
│
└─ FUNCTION GetUnivariate(x_fixed) → UnivariatePolynomial
    │   // Fix first variable, return f(y) = F(x_fixed, y)
    └─  RETURN polynomial in y of degree t

END CLASS
```

## Univariate Polynomial Slice

```pseudo
CLASS UnivariatePolynomial
│
├─ degree : Integer = t
│
├─ coefficients : Array[t+1]           // [c_0, c_1, ..., c_t]
│
├─ FUNCTION Evaluate(y) → FieldElement
│   └─  RETURN Σ_{i=0}^{t} c_i · y^i
│
└─ FUNCTION InterpolateFrom(points) → UnivariatePolynomial
    │   // Lagrange interpolation from t+1 points
    │   // Given {(x_1, y_1), ..., (x_{t+1}, y_{t+1})}
    └─  RETURN unique polynomial of degree ≤ t

END CLASS
```

## Consistency Verification

### Symmetry Check

Honest processes verify polynomial consistency using the symmetry property:

```pseudo
FUNCTION VerifySymmetry(f_i, f_j, i, j) → Boolean
│   // f_i = F(i, y) and f_j = F(j, y)
│   // Should have: F(i, j) = F(j, i)
│
├─ point_from_i ← f_i(j)               // f_i evaluated at j
├─ point_from_j ← f_j(i)               // f_j evaluated at i
│
└─ RETURN (point_from_i = point_from_j)

END FUNCTION
```

### Interpolation from Slices

```pseudo
FUNCTION ReconstructBivariate(slices) → SymmetricBivariatePolynomial
│   // slices: {f_1, f_2, ..., f_k} where k ≥ t+1
│   // Each f_i(y) = F(i, y)
│
├─ FOR each pair (i, j) in slices DO
│   └─ ASSERT f_i(j) = f_j(i)          // Check consistency
│
├─ F(x, y) ← InterpolateBivariate(
│       {(i, j, f_i(j)) | i,j in slices})
│
└─ RETURN F

END FUNCTION
```

## Fault Detection Property

**Key Insight**: The symmetry property enables fault detection:

- If process $i$ claims $f_i(j) = v_1$
- And process $j$ claims $f_j(i) = v_2$  
- Where $v_1 \neq v_2$

Then **at least one** of $\{i, j\}$ is Byzantine, because an honest dealer would distribute slices from a symmetric $F(x,y)$ where $F(i,j) = F(j,i)$.
