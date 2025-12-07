# 4. Inferable Verifiable Secret Sharing (IVSS)

This is the core component of the system. It is divided into two phases: Sharing (S) and Reconstruction (R). Each round of ABA uses new instances of IVSS.

## IVSS-S (Sharing Phase)

**Goal:** Dealer $d$ shares a secret $s$ among $n$ processes.

### Dealer's Protocol

```pseudo
FUNCTION IVSS_Share(secret s)
│
├─ SELECT random symmetric bivariate polynomial F(x, y)
│   WHERE degree(F) = t AND F(0, 0) = s
│
├─ FOR k = 1 TO n DO
│   │   f_k(y) ← F(k, y)              // Univariate polynomial
│   └─  SEND f_k to process k         // Via A-Cast (safer)
│
├─ WAIT FOR candidate set M conditions
│   │
│   ├─ M must satisfy:
│   │   1. |M| ≥ n - t
│   │   2. ∀i,j ∈ M: received A-Cast "EQUAL:(i,j)"
│   │   3. ∀i,j ∈ M: {i,j} ∉ FP (not a faulty pair)
│   │
│   └─ A-Cast "CANDIDATE_SET: M"
│
└─ COMPLETE sharing phase

END FUNCTION
```

### Receiver's Protocol (Process $k$)

```pseudo
LOCAL STATE:
├─ received_poly : Polynomial ← null
└─ consistent_peers : Set[ProcessID] ← ∅

─────────────────────────────────────────────────────────

UPON RECEIVE polynomial f_k FROM dealer
│
├─ received_poly ← f_k
│
└─ FOR j = 1 TO n DO
    │   point ← f_k(j)                // Evaluate polynomial at j
    └─  SEND point to process j       // Direct message

END UPON

─────────────────────────────────────────────────────────

UPON RECEIVE point p_j FROM process j
│
└─ IF received_poly(j) = p_j THEN     // Consistency check
    │                                  // f_k(j) should equal f_j(k)
    └─ A-Cast "EQUAL:(k, j)"

END UPON

─────────────────────────────────────────────────────────

UPON RECEIVE "CANDIDATE_SET: M" FROM dealer
│
├─ VERIFY:
│   ├─ |M| ≥ n - t
│   ├─ ∀i,j ∈ M: received "EQUAL:(i,j)" via A-Cast
│   └─ ∀i,j ∈ M: {i,j} ∉ FP (check against history)
│
└─ IF verification passes THEN
    │   OUTPUT "Sharing Complete"
    └─  STORE (M, received_poly)

END UPON
```

---

## IVSS-R (Reconstruction Phase)

**Pre-condition:** Sharing phase completed successfully (have stored $M$ and `received_poly`).

```pseudo
FUNCTION IVSS_Reconstruct(instance_id) → secret
│
├─ PHASE 1: Broadcast Polynomials
│   │
│   └─ IF I am in set M THEN
│       └─ A-Cast my_polynomial f_k
│
├─ PHASE 2: Collect & Interpolate
│   │
│   ├─ WAIT FOR polynomials from set IS where:
│   │   • |IS| ≥ n - 2t
│   │   • IS ⊆ M
│   │   • All polynomials in IS are consistent:
│   │     ∀i,j ∈ IS: f_i(j) = f_j(i)  (symmetry check)
│   │
│   ├─ INTERPOLATE symmetric bivariate polynomial F
│   │   from {f_i | i ∈ IS}
│   │
│   └─ reconstructed_secret ← F(0, 0)
│
├─ PHASE 3: Commit to History
│   │
│   ├─ A-Cast "READY_TO_COMPLETE"
│   │
│   └─ ADD instance_id to CoreInvocations
│       // For future certification checks
│
└─ PHASE 4: Finalize
    │
    ├─ WAIT FOR (n - t) "READY_TO_COMPLETE" messages
    │
    └─ RETURN reconstructed_secret

END FUNCTION
```

## Fault Detection

During reconstruction, if $f_i(j) \neq f_j(i)$ for processes $i, j \in IS$:
- Add pair $\{i, j\}$ to Faulty Pairs (FP)
- At least one of $i$ or $j$ is Byzantine
