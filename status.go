package main

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
)

type StatusResponse struct {
	Jsonrpc string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Result  struct {
		NodeInfo struct {
			ProtocolVersion struct {
				P2P   string `json:"p2p"`
				Block string `json:"block"`
				App   string `json:"app"`
			} `json:"protocol_version"`
			ID         string `json:"id"`
			ListenAddr string `json:"listen_addr"`
			Network    string `json:"network"`
			Version    string `json:"version"`
			Channels   string `json:"channels"`
			Moniker    string `json:"moniker"`
			Other      struct {
				TxIndex    string `json:"tx_index"`
				RPCAddress string `json:"rpc_address"`
			} `json:"other"`
		} `json:"node_info"`
		SyncInfo struct {
			LatestBlockHash     string    `json:"latest_block_hash"`
			LatestAppHash       string    `json:"latest_app_hash"`
			LatestBlockHeight   string    `json:"latest_block_height"`
			LatestBlockTime     time.Time `json:"latest_block_time"`
			EarliestBlockHash   string    `json:"earliest_block_hash"`
			EarliestAppHash     string    `json:"earliest_app_hash"`
			EarliestBlockHeight string    `json:"earliest_block_height"`
			EarliestBlockTime   time.Time `json:"earliest_block_time"`
			CatchingUp          bool      `json:"catching_up"`
		} `json:"sync_info"`
		ValidatorInfo struct {
			Address string `json:"address"`
			PubKey  struct {
				Type  string `json:"type"`
				Value string `json:"value"`
			} `json:"pub_key"`
			VotingPower string `json:"voting_power"`
		} `json:"validator_info"`
	} `json:"result"`
}

type ConsensusStateResponse struct {
	Jsonrpc string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Result  struct {
		RoundState struct {
			HeightRoundStep   string    `json:"height/round/step"`
			StartTime         time.Time `json:"start_time"`
			ProposalBlockHash string    `json:"proposal_block_hash"`
			LockedBlockHash   string    `json:"locked_block_hash"`
			ValidBlockHash    string    `json:"valid_block_hash"`
			HeightVoteSet     []struct {
				Round              int      `json:"round"`
				Prevotes           []string `json:"prevotes"`
				PrevotesBitArray   string   `json:"prevotes_bit_array"`
				Precommits         []string `json:"precommits"`
				PrecommitsBitArray string   `json:"precommits_bit_array"`
			} `json:"height_vote_set"`
			Proposer struct {
				Address string `json:"address"`
				Index   int    `json:"index"`
			} `json:"proposer"`
		} `json:"round_state"`
	} `json:"result"`
}

func StatusHandler(w http.ResponseWriter, r *http.Request, grpcConn *grpc.ClientConn) {
	requestStart := time.Now()

	sublogger := log.With().
		Str("request_id", uuid.New().String()).
		Logger()

	blockAgeGauge := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name:        "block_age",
			Help:        "Age of the latest block in seconds",
			ConstLabels: ConstLabels,
		},
	)

	missingValidatorsGauge := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name:        "missing_validators",
			Help:        "Number of missing validators for the latest block",
			ConstLabels: ConstLabels,
		},
	)

	registry := prometheus.NewRegistry()
	registry.MustRegister(blockAgeGauge)
	registry.MustRegister(missingValidatorsGauge)

	// Set the metric values
	wg := sync.WaitGroup{}

	wg.Add(1)
	go func() {
		defer wg.Done()
		err := setBlockAge(&blockAgeGauge, &sublogger)
		if err != nil {
			sublogger.Error().Err(err).Msg("Failed to set block age")
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		err := setMissingValidators(&missingValidatorsGauge, &sublogger)
		if err != nil {
			sublogger.Error().Err(err).Msg("Failed to set missing validators")
		}
	}()

	wg.Wait()

	h := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	h.ServeHTTP(w, r)
	sublogger.Info().
		Str("method", "GET").
		Str("endpoint", "/metrics/status").
		Float64("request_time", time.Since(requestStart).Seconds()).
		Msg("Request processed")
}

func setBlockAge(gaugePtr *prometheus.Gauge, sublogger *zerolog.Logger) error {
	// /status endpoint
	resp, err := http.Get(TendermintRPC + "/status")
	if err != nil {
		sublogger.Error().
			Err(err).
			Msg("Error getting the status")
	}

	if resp.Body != nil {
		defer resp.Body.Close()
	}

	body, readErr := ioutil.ReadAll(resp.Body)
	if readErr != nil {
		sublogger.Error().
			Err(readErr).
			Msg("Error reading the status")
	}

	statusResponse := StatusResponse{}
	err = json.Unmarshal(body, &statusResponse)
	if err != nil {
		sublogger.Error().
			Err(err).
			Msg("Error unmarshalling the status json response")
	}
	gauge := *gaugePtr

	gauge.Set(time.Since(statusResponse.Result.SyncInfo.LatestBlockTime).Seconds())
	return nil
}

func setMissingValidators(gaugePtr *prometheus.Gauge, sublogger *zerolog.Logger) error {
	resp, err := http.Get(TendermintRPC + "/consensus_state")
	if err != nil {
		sublogger.Error().
			Err(err).
			Msg("Error getting the consensus_state")
	}

	if resp.Body != nil {
		defer resp.Body.Close()
	}

	body, readErr := ioutil.ReadAll(resp.Body)
	if readErr != nil {
		sublogger.Error().
			Err(readErr).
			Msg("Error reading the consensus_state")
	}

	consensusStateResponse := ConsensusStateResponse{}
	err = json.Unmarshal(body, &consensusStateResponse)
	if err != nil {
		sublogger.Error().
			Err(err).
			Msg("Error unmarshalling the consensus_state json response")
	}

	summaryLine := consensusStateResponse.Result.RoundState.HeightVoteSet[0].PrecommitsBitArray
	validatorsSignedCount := strings.Count(strings.ToLower(summaryLine), "x")
	r, _ := regexp.Compile("{[0-9]+:")
	validatorsTotal, err := strconv.Atoi(r.FindString(summaryLine)[1 : len(r.FindString(summaryLine))-1])
	if err != nil {
		sublogger.Error().
			Err(err).
			Msg("Error getting the validators total")
	}
	gauge := *gaugePtr
	gauge.Set(float64(validatorsTotal - validatorsSignedCount))
	return nil
}
