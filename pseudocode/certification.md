# 3. Certification Protocol

This protocol runs in the background throughout the ABA algorithm, tracking faulty behavior across IVSS instances.

## Global State

```pseudo
GLOBAL STATE:
├─ FP : Set[{ProcessID, ProcessID}] ← ∅
│   // Faulty Pairs: unordered pairs {i,j} where ≥1 is Byzantine
│
└─ CoreInvocations : List[InstanceID] ← []
    // History of successfully completed IVSS instances
```

## Initialization

```pseudo
FUNCTION InitCertification()
│
├─ FP ← ∅
├─ CoreInvocations ← []
│
└─ START background monitoring

END FUNCTION
```

## Fault Detection Logic

Triggered during IVSS reconstruction when polynomial inconsistencies are detected.

```pseudo
FUNCTION CheckConsistency(instance_id, poly_i, poly_j, i, j)
│   // poly_i: polynomial from process i
│   // poly_j: polynomial from process j
│
└─ IF poly_i(j) ≠ poly_j(i) THEN
    │
    │   // Symmetry violation detected!
    │   // For symmetric polynomial F(x,y): F(i,j) must equal F(j,i)
    │   // If f_i(j) ≠ f_j(i), at least one is Byzantine
    │
    ├─ ADD {i, j} to FP              // Unordered pair
    │
    └─ LOG "Faulty pair detected: {" + i + ", " + j + "}"

END FUNCTION
```

## Certification Check

Used in IVSS-S to verify candidate set $M$ doesn't contain known faulty pairs.

```pseudo
FUNCTION VerifySet(M, round) → Boolean
│
├─ FOR EACH i, j ∈ M WHERE i ≠ j DO
│   │
│   └─ FOR EACH past_instance ∈ CoreInvocations DO
│       │
│       └─ IF {i, j} ∈ FP THEN
│           └─ RETURN false          // Reject: contains faulty pair
│
└─ RETURN true                        // All pairs verified

END FUNCTION
```

## Properties
- **Monotonicity**: Once added, pairs remain in FP (no false removal).
- **Soundness**: If $\{i, j\} \in FP$, then at least one is Byzantine.
- **Liveness**: Faulty pairs are eventually detected across rounds.
