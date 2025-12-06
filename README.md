
# Asynchronous Agreement Protocol
This repository contains an implementation of an Asynchronous Byzantine Agreement (ABA) protocol in Go. The protocol allows a set of distributed processes to agree on a binary value (0 or 1) even in the presence of faulty or malicious nodes.

## Brief
Given a system with n>3t+1 processes, where t is the tolerated number of faulty ones, we present a fast asynchronous Byzantine agreement protocol that can reach agreement in O(t) expected running time. This improves the O(n2) expected running time of Abraham, Dolev, and Halpern [PODC 2008]. Furthermore, if n=(3+ε)t for any ε>0, our protocol can reach agreement in O(1/ε) expected running time. This improves the result of Feldman and Micali [STOC 1988] (with constant expected running time when n>4t). (From Cheng Wang "Asynchronous Byzantine Agreement with Optimal Resilience and Linear Complexity")

## References
The implementation is based on this paper:
- Cheng Wang "Asynchronous Byzantine Agreement with Optimal Resilience and Linear Complexity" (https://arxiv.org/abs/1507.06165)

# Testing
To run the tests, execute the `test.sh` script located in the root directory of the project. This script compiles the necessary binaries, generates test inputs, and verifies the correctness of the asynchronous agreement protocol implementation.

```bash
./test.sh
```

The scripts run 100 small tests. If you want to run unit tests run:

```bash
go test -v ./tests/...
```