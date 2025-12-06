package tests

import (
	"async-agreement-protocol-3/services"
	"async-agreement-protocol-3/utils"
	"encoding/json"
	"fmt"
	"math/big"
	"testing"
	"time"
)

func TestIVSS_Byzantine_Reconstruction_BadShare(t *testing.T) {
	n := 4
	f := 1
	network, servicesList, managers := setupIVSSWithDemux(t, n, f)
	defer func() {
		for i := 1; i <= n; i++ {
			managers[i].Stop()
		}
	}()

	secretVal := int64(42)
	secret := big.NewInt(secretVal)
	instanceID := "test-ivss-byzantine-1"
	registerInstanceListener(instanceID, n)

	// 1. Start Sharing (Normal)
	servicesList[1].StartSharing(instanceID, secret, managers[1])

	// Wait for Sharing Complete
	results := instanceResults[instanceID]
	waitForSharing(t, n, results, instanceID)
	t.Log("Sharing complete. Preparing for Byzantine Reconstruction...")

	// 2. Stop Node 4 (The Byzantine Node)
	managers[4].Stop()
	t.Log("Node 4 stopped.")

	// 3. Start Reconstruction on Honest Nodes (1, 2, 3)
	for i := 1; i <= 3; i++ {
		servicesList[i].StartReconstruction(instanceID, managers[i])
	}

	// 4. Inject Malicious Payload from Node 4
	// Create a random/bad polynomial
	// Honest poly for node 4 would be consistent with others.
	// We create a random one which will be inconsistent.
	coeffs := make([]*big.Int, f+1)
	coeffs[0] = big.NewInt(999) // Secret
	for i := 1; i <= f; i++ {
		coeffs[i] = big.NewInt(int64(i + 100)) // Random coeffs
	}
	badPoly := &utils.Polynomial{Coeffs: coeffs}

	payload := services.IVSSPayload{
		InstanceID:   instanceID,
		Type:         services.Payload_Reveal,
		RevealPoly:   badPoly,
		RevealSender: 4,
	}
	payloadBytes, _ := json.Marshal(payload)
	payloadStr := string(payloadBytes)

	// Create ACast VAL message
	uuid := fmt.Sprintf("%s-REVEAL-%d", instanceID, 4)

	acastMsg := services.ACastMessage[string]{
		From: 4,
		Type: services.MSG,
		Val:  payloadStr,
		UUID: uuid,
	}

	wrapper := services.IVSSMessage{
		Type:     services.IVSS_ACast,
		ACastMsg: &acastMsg,
	}

	// Broadcast to all nodes
	// We need to wait a bit to ensure honest nodes have started reconstruction and are listening
	time.Sleep(100 * time.Millisecond)
	network.Broadcast(wrapper)
	t.Log("Malicious payload broadcasted from Node 4")

	// 5. Wait for Reconstruction Complete on Honest Nodes
	// They should detect inconsistency and exclude Node 4, reconstructing the correct secret.
	waitForReconstructionSubset(t, []int{1, 2, 3}, results, instanceID, secret)
	t.Log("IVSS Protocol tolerated Byzantine node and reconstructed correct secret!")
}
