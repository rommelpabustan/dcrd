// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package rpctest

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrutil"
)

const (
	numMatureOutputs = 25
)

func testSendOutputs(r *Harness, t *testing.T) {
	genSpend := func(amt dcrutil.Amount) *chainhash.Hash {
		// Grab a fresh address from the wallet.
		addr, err := r.NewAddress()
		if err != nil {
			t.Fatalf("unable to get new address: %v", err)
		}

		// Next, send amt to this address, spending from one of our
		// mature coinbase outputs.
		addrScript, err := txscript.PayToAddrScript(addr)
		if err != nil {
			t.Fatalf("unable to generate pkscript to addr: %v", err)
		}
		output := wire.NewTxOut(int64(amt), addrScript)
		txid, err := r.SendOutputs([]*wire.TxOut{output}, 10)
		if err != nil {
			t.Fatalf("coinbase spend failed: %v", err)
		}
		return txid
	}

	assertTxMined := func(txid *chainhash.Hash, blockHash *chainhash.Hash) {
		block, err := r.Node.GetBlock(blockHash)
		if err != nil {
			t.Fatalf("unable to get block: %v", err)
		}

		numBlockTxns := len(block.Transactions())
		if numBlockTxns < 2 {
			t.Fatalf("crafted transaction wasn't mined, block should have "+
				"at least %v transactions instead has %v", 2, numBlockTxns)
		}

		minedTx := block.Transactions()[1]
		txHash := minedTx.Hash()
		if *txHash != *txid {
			t.Fatalf("txid's don't match, %v vs %v", txHash, txid)
		}
	}

	// First, generate a small spend which will require only a single
	// input.
	txid := genSpend(dcrutil.Amount(5 * dcrutil.AtomsPerCoin))

	// Generate a single block, the transaction the wallet created should
	// be found in this block.
	blockHashes, err := r.Node.Generate(1)
	if err != nil {
		t.Fatalf("unable to generate single block: %v", err)
	}
	assertTxMined(txid, blockHashes[0])

	// Next, generate a spend much greater than the block reward. This
	// transaction should also have been mined properly.
	txid = genSpend(dcrutil.Amount(5000 * dcrutil.AtomsPerCoin))
	blockHashes, err = r.Node.Generate(1)
	if err != nil {
		t.Fatalf("unable to generate single block: %v", err)
	}
	assertTxMined(txid, blockHashes[0])
}

func assertConnectedTo(t *testing.T, nodeA *Harness, nodeB *Harness) {
	nodePort := defaultP2pPort + (2 * nodeB.nodeNum)
	nodeAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(nodePort))

	nodeAPeers, err := nodeA.Node.GetPeerInfo()
	if err != nil {
		t.Fatalf("unable to get nodeA's peer info")
	}

	addrFound := false
	for _, peerInfo := range nodeAPeers {
		if peerInfo.Addr == nodeAddr {
			addrFound = true
			break
		}
	}

	if !addrFound {
		t.Fatal("nodeA not connected to nodeB")
	}
}

func testConnectNode(r *Harness, t *testing.T) {
	// Create a fresh test harnesses.
	harness, err := New(&chaincfg.SimNetParams, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.SetUp(true, 0); err != nil {
		t.Fatalf("unable to complete rpctest setup: %v", err)
	}
	defer harness.TearDown()

	// Establish a p2p connection the main harness to our new local
	// harness.
	if err := ConnectNode(r, harness); err != nil {
		t.Fatalf("unable to connect harness1 to harness2: %v", err)
	}

	// The main harness should show up in our loca harness' peer's list,
	// and vice verse.
	assertConnectedTo(t, r, harness)
}

func testTearDownAll(t *testing.T) {
	// Grab a local copy of the currently active harnesses before
	// attempting to tear them all down.
	initialActiveHarnesses := ActiveHarnesses()

	// Tear down all currently active harnesses.
	if err := TearDownAll(); err != nil {
		t.Fatalf("unable to teardown all harnesses: %v", err)
	}

	// The global testInstances map should now be fully purged with no
	// active test harnesses remaining.
	if len(ActiveHarnesses()) != 0 {
		t.Fatalf("test harnesses still active after TearDownAll")
	}

	for _, harness := range initialActiveHarnesses {
		// Ensure all test directories have been deleted.
		if _, err := os.Stat(harness.testNodeDir); err == nil {
			t.Errorf("created test datadir was not deleted.")
		}
	}
}

func testActiveHarnesses(r *Harness, t *testing.T) {
	numInitialHarnesses := len(ActiveHarnesses())

	// Create a single test harness.
	harness1, err := New(&chaincfg.SimNetParams, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer harness1.TearDown()

	// With the harness created above, a single harness should be detected
	// as active.
	numActiveHarnesses := len(ActiveHarnesses())
	if !(numActiveHarnesses > numInitialHarnesses) {
		t.Fatalf("ActiveHarnesses not updated, should have an " +
			"additional test harness listed.")
	}
}

func testJoinMempools(r *Harness, t *testing.T) {
	// Create a new local test harnesses, starting at the same height.
	harness, err := New(&chaincfg.SimNetParams, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.SetUp(true, numMatureOutputs); err != nil {
		t.Fatalf("unable to complete rpctest setup: %v", err)
	}
	defer harness.TearDown()

	nodeSlice := []*Harness{r, harness}

	// Both mempools should be considered synced as they are empty.
	// Therefore, this should return instantly.
	if err := JoinNodes(nodeSlice, Mempools); err != nil {
		t.Fatalf("unable to join node on block height: %v", err)
	}

	// Generate a coinbase spend to a new address within harness1's
	// mempool.
	addr, err := harness.NewAddress()
	addrScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		t.Fatalf("unable to generate pkscript to addr: %v", err)
	}
	output := wire.NewTxOut(5e8, addrScript)
	if _, err = harness.SendOutputs([]*wire.TxOut{output}, 10); err != nil {
		t.Fatalf("coinbase spend failed: %v", err)
	}

	poolsSynced := make(chan struct{})
	go func() {
		if err := JoinNodes(nodeSlice, Mempools); err != nil {
			t.Fatalf("unable to join node on node mempools: %v", err)
		}
		poolsSynced <- struct{}{}
	}()

	// This select case should fall through to the default as the goroutine
	// should be blocked on the JoinNodes calls.
	select {
	case <-poolsSynced:
		t.Fatalf("mempools detected as synced yet harness1 has a new tx")
	default:
	}

	// Establish an outbound connection from harness1 to harness2. After
	// the initial handshake both nodes should exchange inventory resulting
	// in a synced mempool.
	if err := ConnectNode(r, harness); err != nil {
		t.Fatalf("unable to connect harnesses: %v", err)
	}

	// Select once again with a special timeout case after 1 minute. The
	// goroutine above should now be blocked on sending into the unbuffered
	// channel. The send should immediately succeed. In order to avoid the
	// test hanging indefinitely, a 1 minute timeout is in place.
	select {
	case <-poolsSynced:
		// fall through
	case <-time.After(time.Minute):
		t.Fatalf("block heights never detected as synced")
	}

}

func testJoinBlocks(r *Harness, t *testing.T) {
	// Create two test harnesses, with one being 5 blocks ahead of the other
	// with respect to block height.
	harness1, err := New(&chaincfg.SimNetParams, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := harness1.SetUp(true, numMatureOutputs+5); err != nil {
		t.Fatalf("unable to complete rpctest setup: %v", err)
	}
	defer harness1.TearDown()
	harness2, err := New(&chaincfg.SimNetParams, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := harness2.SetUp(true, numMatureOutputs); err != nil {
		t.Fatalf("unable to complete rpctest setup: %v", err)
	}
	defer harness2.TearDown()

	nodeSlice := []*Harness{harness1, harness2}
	blocksSynced := make(chan struct{})
	go func() {
		if err := JoinNodes(nodeSlice, Blocks); err != nil {
			t.Fatalf("unable to join node on block height: %v", err)
		}
		blocksSynced <- struct{}{}
	}()

	// This select case should fall through to the default as the goroutine
	// should be blocked on the JoinNodes calls.
	select {
	case <-blocksSynced:
		t.Fatalf("blocks detected as synced yet harness2 is 5 blocks behind")
	default:
	}

	// Extend harness2's chain by 5 blocks, this should cause JoinNodes to
	// finally unblock and return.
	if _, err := harness2.Node.Generate(5); err != nil {
		t.Fatalf("unable to generate blocks: %v", err)
	}

	// Select once again with a special timeout case after 1 minute. The
	// goroutine above should now be blocked on sending into the unbuffered
	// channel. The send should immediately succeed. In order to avoid the
	// test hanging indefinitely, a 1 minute timeout is in place.
	select {
	case <-blocksSynced:
		// fall through
	case <-time.After(time.Minute):
		t.Fatalf("block heights never detected as synced")
	}
}

func testMemWalletReorg(r *Harness, t *testing.T) {
	// Create a fresh harness, we'll be using the main harness to force a
	// re-org on this local harness.
	harness, err := New(&chaincfg.SimNetParams, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.SetUp(true, 5); err != nil {
		t.Fatalf("unable to complete rpctest setup: %v", err)
	}
	defer harness.TearDown()

	// Ensure the internal wallet has the expected balance.
	expectedBalance := dcrutil.Amount(5 * 300 * dcrutil.AtomsPerCoin)
	walletBalance := harness.ConfirmedBalance()
	if expectedBalance != walletBalance {
		t.Fatalf("wallet balance incorrect: expected %v, got %v",
			expectedBalance, walletBalance)
	}

	// Now connect this local harness to the main harness then wait for
	// their chains to synchronize.
	if err := ConnectNode(r, harness); err != nil {
		t.Fatalf("unable to connect harnesses: %v", err)
	}
	nodeSlice := []*Harness{r, harness}
	if err := JoinNodes(nodeSlice, Blocks); err != nil {
		t.Fatalf("unable to join node on block height: %v", err)
	}

	// The original wallet should now have a balance of 0 Coin as its entire
	// chain should have been decimated in favor of the main harness'
	// chain.
	expectedBalance = dcrutil.Amount(0)
	walletBalance = harness.ConfirmedBalance()
	if expectedBalance != walletBalance {
		t.Fatalf("wallet balance incorrect: expected %v, got %v",
			expectedBalance, walletBalance)
	}
}

func testMemWalletLockedOutputs(r *Harness, t *testing.T) {
	// Obtain the initial balance of the wallet at this point.
	startingBalance := r.ConfirmedBalance()

	// First, create a signed transaction spending some outputs.
	addr, err := r.NewAddress()
	if err != nil {
		t.Fatalf("unable to generate new address: %v", err)
	}
	pkScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		t.Fatalf("unable to create script: %v", err)
	}
	outputAmt := dcrutil.Amount(50 * dcrutil.AtomsPerCoin)
	output := wire.NewTxOut(int64(outputAmt), pkScript)
	tx, err := r.CreateTransaction([]*wire.TxOut{output}, 10)
	if err != nil {
		t.Fatalf("unable to create transaction: %v", err)
	}

	// The current wallet balance should now be at least 50 Coin less
	// (accounting for fees) than the period balance
	currentBalance := r.ConfirmedBalance()
	if !(currentBalance <= startingBalance-outputAmt) {
		t.Fatalf("spent outputs not locked: previous balance %v, "+
			"current balance %v", startingBalance, currentBalance)
	}

	// Now unlocked all the spent inputs within the unbroadcast signed
	// transaction. The current balance should now be exactly that of the
	// starting balance.
	r.UnlockOutputs(tx.TxIn)
	currentBalance = r.ConfirmedBalance()
	if currentBalance != startingBalance {
		t.Fatalf("current and starting balance should now match: "+
			"expected %v, got %v", startingBalance, currentBalance)
	}
}

var harnessTestCases = []HarnessTestCase{
	testSendOutputs,
	testConnectNode,
	testActiveHarnesses,
	testJoinMempools,
	testJoinBlocks,
	testMemWalletReorg,
	testMemWalletLockedOutputs,
}

var mainHarness *Harness

func TestMain(m *testing.M) {
	var err error
	mainHarness, err = New(&chaincfg.SimNetParams, nil, nil)
	if err != nil {
		fmt.Println("unable to create main harness: ", err)
		os.Exit(1)
	}

	// Initialize the main mining node with a chain of length 42, providing
	// 25 mature coinbases to allow spending from for testing purposes.
	if err = mainHarness.SetUp(true, numMatureOutputs); err != nil {
		fmt.Println("unable to setup test chain: ", err)
		os.Exit(1)
	}

	exitCode := m.Run()

	// Clean up any active harnesses that are still currently running.
	if len(ActiveHarnesses()) > 0 {
		if err := TearDownAll(); err != nil {
			fmt.Println("unable to tear down chain: ", err)
			os.Exit(1)
		}
	}

	os.Exit(exitCode)
}

func TestHarness(t *testing.T) {
	// We should have the expected amount of mature unspent outputs.
	expectedBalance := dcrutil.Amount(numMatureOutputs * 300 * dcrutil.AtomsPerCoin)
	harnessBalance := mainHarness.ConfirmedBalance()
	if harnessBalance != expectedBalance {
		t.Fatalf("expected wallet balance of %v instead have %v",
			expectedBalance, harnessBalance)
	}

	// Current tip should be at a height of numMatureOutputs plus the
	// required number of blocks for coinbase maturity plus an additional
	// block for the premine block.
	nodeInfo, err := mainHarness.Node.GetInfo()
	if err != nil {
		t.Fatalf("unable to execute getinfo on node: %v", err)
	}
	coinbaseMaturity := uint32(mainHarness.ActiveNet.CoinbaseMaturity)
	expectedChainHeight := numMatureOutputs + coinbaseMaturity + 1
	if uint32(nodeInfo.Blocks) != expectedChainHeight {
		t.Errorf("Chain height is %v, should be %v",
			nodeInfo.Blocks, expectedChainHeight)
	}

	for _, testCase := range harnessTestCases {
		testCase(mainHarness, t)
	}

	testTearDownAll(t)
}
