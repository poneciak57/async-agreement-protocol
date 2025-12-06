package main

import (
	"async-agreement-protocol-3/services"
	"async-agreement-protocol-3/utils"
	"flag"
	"fmt"
	"sync"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	silent := flag.Bool("silent", false, "Disable logs and print only result")
	flag.Parse()

	utils.SetupLogger()

	// Set log level
	logLevel := zerolog.InfoLevel
	if *silent {
		logLevel = zerolog.Disabled
		zerolog.SetGlobalLevel(zerolog.Disabled)
	}

	var n, t int
	// Read N and T
	if _, err := fmt.Scan(&n, &t); err != nil {
		log.Fatal().Err(err).Msg("Failed to read N and T")
	}

	log.Info().Str("layer", "MAIN").Int("n", n).Int("t", t).Msg("Start ABA Simulation")

	// Read inputs for honest nodes
	honestCount := n - t
	inputs := make([]int, honestCount)
	for i := 0; i < honestCount; i++ {
		if _, err := fmt.Scan(&inputs[i]); err != nil {
			// If input missing, default to 0
			log.Warn().Msgf("Input for node %d missing, defaulting to 0", i+1)
			inputs[i] = 0
		}
	}

	// Create Network
	network := services.NewNetwork[services.ABAMessage]()

	// Create Nodes
	nodes := make([]*Node, honestCount)
	for i := 0; i < honestCount; i++ {
		id := i + 1
		cp := services.NewCertificationProtocol() // Local instance for each node
		nodes[i] = NewNode(id, n, t, inputs[i], network, cp, logLevel)

		// Register in Network
		network.Register(id, nodes[i].Inbox())
	}

	// Start Nodes
	var wg sync.WaitGroup
	wg.Add(honestCount)

	res := make([]int, honestCount)
	for i := 0; i < honestCount; i++ {
		go func(node *Node) {
			defer wg.Done()
			node.Start()

			// Wait for result
			res[i] = <-node.Result()
			log.Info().Int("node_id", node.ID).Int("result", res[i]).Msg("Node Decided")
		}(nodes[i])
	}

	// Wait for all honest nodes to decide
	wg.Wait()
	if !*silent {
		log.Info().Msg("All honest nodes decided. Simulation finished.")
	}

	fmt.Print("RESULTS:")
	for i := 0; i < honestCount; i++ {
		fmt.Printf(" %d", res[i])
		if !*silent {
			log.Info().Int("node_id", nodes[i].ID).Int("result", res[i]).Msg("Node Decided")
		}
	}
	fmt.Println()
}
