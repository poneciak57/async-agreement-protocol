# 5. Inferable Common Coin (ICC)

Generates a common random bit for all processes to use when no majority is reached.

## Algorithm

```pseudo
FUNCTION GetCommonCoin(round r) → {0, 1}
│
├─ PHASE 1: Secret Sharing
│   │
│   │   // Each process acts as dealer for n IVSS instances
│   └─ FOR j = 1 TO n DO
│       └─ IVSS[r, j]-S.Start(RANDOM_VALUE())
│
├─ PHASE 2: Collect Attachment Sets
│   │
│   ├─ WAIT UNTIL (t + 1) IVSS-S instances complete for me
│   │   Let T_i = {dealers of completed secrets}
│   │
│   └─ A-Cast "ATTACH: T_i"
│
├─ PHASE 3: Acceptance
│   │
│   ├─ A_i ← ∅
│   │
│   ├─ FOR EACH "ATTACH: T_j" received FROM j DO
│   │   └─ IF T_j ⊆ T_i THEN
│   │       └─ ADD j to A_i
│   │
│   ├─ WAIT UNTIL |A_i| ≥ n - t
│   │
│   └─ A-Cast "ACCEPT: A_i"
│
├─ PHASE 4: Enable Reconstruction
│   │
│   ├─ WAIT FOR (n - t) "ACCEPT: A_j" messages
│   │   Let S_i = {senders}
│   │
│   └─ A-Cast "RECONSTRUCT_ENABLED: (H_i = A_i, S_i)"
│
├─ PHASE 5: Reconstruct Secrets
│   │
│   └─ FOR EACH j ∈ A_i DO
│       └─ FOR EACH k ∈ T_j DO
│           └─ RUN IVSS[r, k]-R
│
└─ PHASE 6: Compute Coin
    │
    ├─ WAIT UNTIL all reconstructions complete
    │
    ├─ u ← ⌈0.87 × n⌉
    │
    ├─ FOR EACH k ∈ H_i DO
    │   └─ value_k ← reconstructed_secret[k] mod u
    │
    ├─ IF EXISTS k ∈ H_i : value_k = 0 THEN
    │   └─ RETURN 0
    │
    └─ ELSE
        └─ RETURN 1

END FUNCTION
```

## Properties
- **Agreement**: All correct processes return the same coin value.
- **Unpredictability**: The adversary cannot predict the coin with probability > 1/2 + ε.
- **Termination**: Terminates with probability 1.
