////////////////////////////////////////////////////////////////////////////////
// Copyright © 2018 Privategrity Corporation                                   /
//                                                                             /
// All rights reserved.                                                        /
////////////////////////////////////////////////////////////////////////////////

// Package node contains the initialization and main loop of a cMix server.
package cmd

import (
	jww "github.com/spf13/jwalterweatherman"
	"github.com/spf13/viper"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"gitlab.com/privategrity/comms/mixserver"
	"gitlab.com/privategrity/crypto/cyclic"
	"gitlab.com/privategrity/server/cryptops/realtime"
	"gitlab.com/privategrity/server/globals"
	"gitlab.com/privategrity/server/io"
)

// RunRealtime controls when realtime is kicked off and which
// messages are sent through the realtime phase. It reads up to batchSize
// messages from the MessageCh, then reads a round and kicks off realtime
// with those messages.
func RunRealTime(batchSize uint64, MessageCh chan *realtime.RealtimeSlot,
	RoundCh chan *string, realtimeSignal *sync.Cond) {
	msgCount := uint64(0)
	msgList := make([]*realtime.RealtimeSlot, batchSize)
	for msg := range MessageCh {
		jww.DEBUG.Printf("Adding message ("+
			"%d/%d) from SenderID %d to Recipient %s...",
			msgCount+1, batchSize, msg.CurrentID,
			msg.EncryptedRecipient.Text(10))
		msg.Slot = msgCount
		msgList[msgCount] = msg
		msgCount = msgCount + 1

		if msgCount == batchSize {
			msgCount = uint64(0)
			// Pass the batch queue into Realtime and begin
			startTime := time.Now()
			roundId := *(<-RoundCh)
			jww.INFO.Printf("Realtime phase with Round ID %s started at %s\n",
				roundId, startTime.Format(time.RFC3339))
			io.KickoffDecryptHandler(roundId, batchSize, msgList)

			// Signal the precomputation thread to run
			realtimeSignal.L.Lock()
			realtimeSignal.Signal()
			realtimeSignal.L.Unlock()

			// Wait for the realtime phase to complete and record the elapsed time
			go func(roundId string, startTime time.Time) {
				round := globals.GlobalRoundMap.GetRound(roundId)
				round.WaitUntilPhase(globals.REAL_COMPLETE)
				endTime := time.Now()
				jww.INFO.Printf("Realtime phase with Round ID %s finished at %s!\n",
					roundId, endTime.Format(time.RFC3339))
				jww.INFO.Printf("Realtime phase completed in %d ms",
					int64(endTime.Sub(startTime)/time.Millisecond))
			}(roundId, startTime)
		}
	}
}

// RunPrecomputation controls when precomputation is kicked off. It monitors
// the length of the RoundCh and creates new rounds and kicks of precomputation
// whenever it falls below a threshold.
func RunPrecomputation(RoundCh chan *string, realtimeSignal *sync.Cond) {
	for {
		if len(RoundCh) < 10 {
			// Begin the round on all nodes
			startTime := time.Now()
			roundId := globals.PeekNextRoundID()

			jww.INFO.Printf("Precomputation phase with Round ID %s started at %s\n",
				roundId, startTime.Format(time.RFC3339))
			io.BeginNewRound(io.Servers, roundId)
			// Wait for round to be in the PRECOMP_COMPLETE state before
			// adding it to the round map
			// go func(RoundCh chan *string, roundId string) {
			round := globals.GlobalRoundMap.GetRound(roundId)

			// If a round takes more than 5 minutes to compute, fail it
			roundTimeout := time.NewTimer(5 * time.Minute)
			go func() {
				<-roundTimeout.C
				if round.GetPhase() < globals.PRECOMP_COMPLETE {
					round.SetPhase(globals.ERROR)
				}
			}()

			// Wait until the round completes to continue
			round.WaitUntilPhase(globals.PRECOMP_COMPLETE)
			roundTimeout.Stop()
			if round.GetPhase() == globals.ERROR {
				jww.FATAL.Panicf("Fatal error occurred during precomputation of "+
					"round %s", roundId)
			}
			endTime := time.Now()
			jww.INFO.Printf("Precomputation phase with Round ID %s finished at %s!\n",
				roundId, endTime.Format(time.RFC3339))
			jww.INFO.Printf("Precomputation phase completed in %d ms",
				int64(endTime.Sub(startTime)/time.Millisecond))
			RoundCh <- &roundId
			// }(RoundCh, roundId)
			// Wait at least a second before kicking off another precomputation
			time.Sleep(1000 * time.Millisecond)
		} else {
			// Since we are full, wait until the realtime thread signals us to run
			// again
			realtimeSignal.L.Lock()
			realtimeSignal.Wait()
			realtimeSignal.L.Unlock()
		}
	}
}

// StartServer reads configuration options and starts the cMix server
func StartServer(serverIndex int, batchSize uint64) {
	viper.Debug()
	jww.INFO.Printf("Log Filename: %v\n", viper.GetString("logPath"))
	jww.INFO.Printf("Config Filename: %v\n", viper.ConfigFileUsed())

	// Set global batch size
	globals.BatchSize = batchSize
	jww.INFO.Printf("Batch Size: %v\n", globals.BatchSize)

	// Initialize the backend
	globals.Users = globals.NewUserRegistry(
		viper.GetString("dbUsername"),
		viper.GetString("dbPassword"),
		viper.GetString("dbName"),
		viper.GetStringSlice("dbAddresses")[serverIndex],
	)
	globals.PopulateDummyUsers()

	// Get all servers
	io.Servers = getServers(serverIndex)

	// TODO Generate globals.Grp somewhere intelligent
	primeString := "FFFFFFFFFFFFFFFFC90FDAA22168C234C4C6628B80DC1CD1" +
		"29024E088A67CC74020BBEA63B139B22514A08798E3404DD" +
		"EF9519B3CD3A431B302B0A6DF25F14374FE1356D6D51C245" +
		"E485B576625E7EC6F44C42E9A637ED6B0BFF5CB6F406B7ED" +
		"EE386BFB5A899FA5AE9F24117C4B1FE649286651ECE45B3D" +
		"C2007CB8A163BF0598DA48361C55D39A69163FA8FD24CF5F" +
		"83655D23DCA3AD961C62F356208552BB9ED529077096966D" +
		"670C354E4ABC9804F1746C08CA18217C32905E462E36CE3B" +
		"E39E772C180E86039B2783A2EC07A28FB5C55DF06F4C52C9" +
		"DE2BCBF6955817183995497CEA956AE515D2261898FA0510" +
		"15728E5A8AAAC42DAD33170D04507A33A85521ABDF1CBA64" +
		"ECFB850458DBEF0A8AEA71575D060C7DB3970F85A6E1E4C7" +
		"ABF5AE8CDB0933D71E8C94E04A25619DCEE3D2261AD2EE6B" +
		"F12FFA06D98A0864D87602733EC86A64521F2B18177B200C" +
		"BBE117577A615D6C770988C0BAD946E208E24FA074E5AB31" +
		"43DB5BFCE0FD108E4B82D120A92108011A723C12A787E6D7" +
		"88719A10BDBA5B2699C327186AF4E23C1A946834B6150BDA" +
		"2583E9CA2AD44CE8DBBBC2DB04DE8EF92E8EFC141FBECAA6" +
		"287C59474E6BC05D99B2964FA090C3A2233BA186515BE7ED" +
		"1F612970CEE2D7AFB81BDD762170481CD0069127D5B05AA9" +
		"93B4EA988D8FDDC186FFB7DC90A6C08F4DF435C934063199" +
		"FFFFFFFFFFFFFFFF"
	prime := cyclic.NewInt(0)
	prime.SetString(primeString, 16)
	// one := cyclic.NewInt(1)
	rngmax := cyclic.NewIntFromUInt(math.MaxUint64)
	rngmax.Mul(rngmax, prime)
	rng := cyclic.NewRandom(cyclic.NewInt(0), rngmax)
	grp := cyclic.NewGroup(prime, cyclic.NewInt(2), cyclic.NewInt(4), rng)
	globals.Grp = &grp

	// Start mix servers on localServer
	localServer := io.Servers[serverIndex]
	jww.INFO.Printf("Starting server on %v\n", localServer)
	// Initialize GlobalRoundMap and waiting rounds queue
	globals.GlobalRoundMap = globals.NewRoundMap()

	// ensure that the Node ID is populated
	globals.NodeID(uint64(serverIndex))

	// Kick off Comms server
	go mixserver.StartServer(localServer, io.ServerImpl{
		Rounds: &globals.GlobalRoundMap,
	})

	// TODO Replace these concepts with a better system
	io.IsLastNode = serverIndex == len(io.Servers)-1
	io.NextServer = io.Servers[(serverIndex+1)%len(io.Servers)]

	// Block until we can reach every server
	io.VerifyServersOnline(io.Servers)

	if io.IsLastNode {
		realtimeSignal := &sync.Cond{L: &sync.Mutex{}}
		io.RoundCh = make(chan *string, 10)
		io.MessageCh = make(chan *realtime.RealtimeSlot)
		// Last Node handles when realtime and precomp get run
		go RunRealTime(batchSize, io.MessageCh, io.RoundCh, realtimeSignal)
		go RunPrecomputation(io.RoundCh, realtimeSignal)
	}

	// Main loop
	run()
}

// Main server loop
func run() {
	// Blocks forever as a keepalive
	select {}
}

// getServers pulls a string slice of server ports from the config file and
// verifies that the ports are valid.
func getServers(serverIndex int) []string {
	servers := viper.GetStringSlice("servers")
	if servers == nil {
		jww.FATAL.Panicf("No servers listed in config file!")
	}
	for i := range servers {
		// Split address and port
		s := strings.Split(servers[i], ":")
		// Convert port to an int
		temp, err := strconv.Atoi(s[1])
		// catch non-int ports
		if err != nil {
			jww.FATAL.Panicf("Non-integer server ports in config file!")
		}
		// Catch invalid ports
		if temp > 65535 || temp < 0 {
			jww.FATAL.Panicf("Port %v listed in the config file is not a "+
				"valid port!", temp)
		}
		// Catch reserved ports
		if temp < 1024 {
			jww.WARN.Printf("Port %v is a reserved port, superuser privilege"+
				" may be required!", temp)
		}
		if i == serverIndex {
			// Remove the IP from the local server
			// in order to listen on the relevant port
			servers[i] = "0.0.0.0:" + s[1]
		}
	}
	return servers
}