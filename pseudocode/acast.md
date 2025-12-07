# Asynchronous Broadcast (A-Cast)

A reliable broadcast primitive (Bracha's Broadcast) that ensures all correct processes deliver the same message, even if the sender is faulty.

## Local State

For each A-Cast instance (identified by `⟨Sender, MessageID⟩`):

```pseudo
CLASS ACastInstance
│
├─ received_echo : Set[ProcessID]      // Processes that sent ECHO
├─ received_ready : Set[ProcessID]     // Processes that sent READY
├─ sent_echo : Boolean ← false
├─ sent_ready : Boolean ← false
├─ delivered : Boolean ← false
└─ output_value : Value ← null

END CLASS
```

## Protocol

### Sender

```pseudo
FUNCTION Broadcast(value)
│
└─ SEND MSG(value) to all processes

END FUNCTION
```

### Receiver (Process $i$)

```pseudo
UPON RECEIVE MSG(val) FROM sender
│
└─ IF NOT sent_echo THEN
    │   SEND ECHO(val) to all processes
    └─  sent_echo ← true

END UPON

─────────────────────────────────────────────────────────

UPON RECEIVE ECHO(val) FROM process j
│
├─ ADD j to received_echo[val]
│
└─ IF |received_echo[val]| ≥ n - t AND NOT sent_ready THEN
    │                                    // Threshold: n - t
    │   SEND READY(val) to all processes
    └─  sent_ready ← true

END UPON

─────────────────────────────────────────────────────────

UPON RECEIVE READY(val) FROM process j
│
├─ ADD j to received_ready[val]
│
├─ IF |received_ready[val]| ≥ t + 1 AND NOT sent_ready THEN
│   │                                    // Amplification: t + 1
│   │   SEND READY(val) to all processes
│   └─  sent_ready ← true
│
└─ IF |received_ready[val]| ≥ 2t + 1 AND NOT delivered THEN
    │                                    // Delivery: 2t + 1
    │   output_value ← val
    │   delivered ← true
    └─  TRIGGER A-Cast-Complete(val)

END UPON
```

## Properties
- **Validity**: If sender is correct and broadcasts $v$, all correct processes eventually deliver $v$.
- **Agreement**: If a correct process delivers $v$, all correct processes eventually deliver $v$.
- **Integrity**: Every correct process delivers at most one value, and only if it was broadcast.