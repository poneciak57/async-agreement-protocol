# 6. Vote Protocol

This protocol determines if there is a majority of processes voting for a specific value.

## Algorithm

```pseudo
FUNCTION Vote(round r, input_bit x_i) → (value, confidence)
│
├─ PHASE 1: Input Exchange
│   │
│   ├─ A-Cast "INPUT: (i, x_i)"
│   │
│   ├─ WAIT FOR (n - t) INPUT messages
│   │   Let A_i = {senders of received INPUT messages}
│   │
│   ├─ my_vote_1 ← MAJORITY_BIT(A_i)      // 0 if tie
│   │
│   └─ A-Cast "VOTE1: (i, A_i, my_vote_1)"
│
├─ PHASE 2: First Vote
│   │
│   ├─ WAIT FOR (n - t) VOTE1 messages
│   │   Let B_i = {j | received VOTE1 from j AND A_j ⊆ A_i}
│   │
│   ├─ my_vote_2 ← MAJORITY_BIT(votes in B_i)
│   │
│   └─ A-Cast "REVOTE: (i, B_i, my_vote_2)"
│
├─ PHASE 3: Revote Collection
│   │
│   └─ WAIT FOR (n - t) REVOTE messages
│       Let C_i = {j | received REVOTE from j AND B_j ⊆ B_i}
│
└─ DECISION LOGIC
    │
    ├─ IF all votes in B_i equal σ THEN
    │   └─ RETURN (σ, 2)                  // Strong Majority
    │
    ├─ ELSE IF all revotes in C_i equal σ THEN
    │   └─ RETURN (σ, 1)                  // Weak Majority
    │
    └─ ELSE
        └─ RETURN (⊥, 0)                  // No Majority

END FUNCTION
```

## Output
- **Value**: The majority bit (0, 1, or ⊥ for null)
- **Confidence Level**:
  - `2`: Strong majority (all agree in first vote)
  - `1`: Weak majority (all agree in revote)
  - `0`: No majority

## Helper Function

```pseudo
FUNCTION MAJORITY_BIT(set S) → {0, 1}
│
├─ count_0 ← |{j ∈ S | bit[j] = 0}|
├─ count_1 ← |{j ∈ S | bit[j] = 1}|
│
└─ RETURN (count_1 > count_0) ? 1 : 0     // Default to 0 on tie

END FUNCTION
```
