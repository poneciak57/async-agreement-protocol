# 7. Asynchronous Byzantine Agreement (ABA) Main Loop

This is the main entry point of the protocol invoked by the application.

## Algorithm

```pseudo
FUNCTION ABA(input_bit x_i)
│
├─ INITIALIZE
│   round ← 0
│   estimate ← x_i
│   START CertificationProtocol() in background
│
└─ LOOP forever
    │
    ├─ round ← round + 1
    │
    ├─ PHASE 1: Vote
    │   (vote_val, vote_strength) ← Vote(round, estimate)
    │
    ├─ PHASE 2: Common Coin
    │   coin_val ← ICC(round)
    │
    ├─ PHASE 3: Decision Logic (Canetti & Rabin)
    │   │
    │   IF vote_strength = 2 THEN          // Strong Majority
    │   │   estimate ← vote_val
    │   │   A-Cast "COMPLETE: estimate"
    │   │   // Note: Continue to next round to help other nodes
    │   │
    │   ELSE IF vote_strength = 1 THEN     // Weak Majority
    │   │   estimate ← vote_val
    │   │
    │   ELSE                                // No Majority
    │       estimate ← coin_val
    │   END IF
    │
    └─ PHASE 4: Termination Check
        │
        UPON receiving (t + 1) "COMPLETE: v" messages
        │   OUTPUT v
        └─  TERMINATE

END FUNCTION
```

## Properties
- **Termination**: With probability 1, all correct processes eventually decide.
- **Agreement**: All correct processes decide on the same value.
- **Validity**: If all correct processes have the same input, they decide on that input.
