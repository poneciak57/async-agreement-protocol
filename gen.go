//go:build ignore

package main

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"time"
)

func main() {
	rand.Seed(time.Now().UnixNano())

	var n, t int

	if len(os.Args) < 3 {
		// Random generation
		n = rand.Intn(10) + 4 // 4 to 13

		// Max t such that 3t < n => t <= (n-1)/3
		maxT := (n - 1) / 3
		if maxT < 0 {
			maxT = 0
		}

		// Pick random t from 0 to maxT
		// We want to bias towards higher t to stress test
		if maxT > 0 {
			t = rand.Intn(maxT + 1)
		} else {
			t = 0
		}

	} else {
		// Manual generation
		n, _ = strconv.Atoi(os.Args[1])
		t, _ = strconv.Atoi(os.Args[2])
	}

	fmt.Printf("%d %d\n", n, t)

	honest := n - t
	for i := 0; i < honest; i++ {
		fmt.Printf("%d ", rand.Intn(2))
	}
	fmt.Println()
}
