package main

import (
	"context"
	"net/http"
	"sync"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
)

func GravityBridgeWalletHandler(w http.ResponseWriter, r *http.Request, grpcConn *grpc.ClientConn) {
	requestStart := time.Now()

	sublogger := log.With().
		Str("request_id", uuid.New().String()).
		Logger()

	cudosOrchestratorAddressParam := r.URL.Query().Get("cudos_orchestrator_address")
	cudosOrchestratorAddress, err := sdk.AccAddressFromBech32(cudosOrchestratorAddressParam)
	if err != nil {
		sublogger.Error().
			Str("cudos_orchestrator_address", cudosOrchestratorAddressParam).
			Err(err).
			Msg("Could not get cosmos address")
		return
	}

	ethConn, err := ethclient.Dial(EthRPC)
	if err != nil {
		log.Fatal().Err(err).Msg("Could not connect to Ethereum node")
	}

	ethOrchestratorAddressParam := r.URL.Query().Get("ethereum_orchestrator_address")
	ethOrchestratorAddress := common.HexToAddress(ethOrchestratorAddressParam)

	if err != nil {
		log.Fatal().Err(err).Msg("Could not retreive the token contract")
	}

	gravCudoOrchBalanceGauge := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        "gravity_cudos_orchestrator_balance",
			Help:        "Balance of the cudos orchestrator wallet",
			ConstLabels: ConstLabels,
		},
		[]string{"cudos_orchestrator_address", "ethereum_orchestrator_address"},
	)

	gravEthOrchBalanceGauge := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        "gravity_ethereum_orchestrator_balance",
			Help:        "Balance of the ethereum orchestrator wallet",
			ConstLabels: ConstLabels,
		},
		[]string{"cudos_orchestrator_address", "ethereum_orchestrator_address"},
	)

	registry := prometheus.NewRegistry()
	registry.MustRegister(gravCudoOrchBalanceGauge)
	registry.MustRegister(gravEthOrchBalanceGauge)

	var wg sync.WaitGroup

	go func() {
		defer wg.Done()
		sublogger.Debug().
			Str("cudos_orchestrator_address", cudosOrchestratorAddress.String()).
			Msg("Started querying orchestrator wallet balance")
		queryStart := time.Now()

		bankClient := banktypes.NewQueryClient(grpcConn)
		bankRes, err := bankClient.AllBalances(
			context.Background(),
			&banktypes.QueryAllBalancesRequest{Address: cudosOrchestratorAddress.String()},
		)
		if err != nil {
			sublogger.Error().
				Str("cudos_orchestrator_address", cudosOrchestratorAddress.String()).
				Err(err).
				Msg("Could not get orchestrator balance")
			return
		}

		sublogger.Debug().
			Str("cudos_orchestrator_address", cudosOrchestratorAddress.String()).
			Float64("request_time", time.Since(queryStart).Seconds()).
			Msg("Finished querying orchestrator balance")

		for _, balance := range bankRes.Balances {
			tokensRatio, _ := ToNativeBalance(balance.Amount.BigInt())
			gravCudoOrchBalanceGauge.With(prometheus.Labels{
				"cudos_orchestrator_address":    cudosOrchestratorAddress.String(),
				"ethereum_orchestrator_address": ethOrchestratorAddress.String(),
			}).Set(tokensRatio)

		}
	}()
	wg.Add(1)

	go func() {
		defer wg.Done()
		sublogger.Debug().
			Str("ethereum_orchestrator_address", ethOrchestratorAddress.String()).
			Msg("Started querying ethereum wallet balance")
		queryStart := time.Now()

		ethBal, err := ethConn.BalanceAt(context.Background(), ethOrchestratorAddress, nil)
		if err != nil {
			sublogger.Error().
				Str("ethereum_orchestrator_address", ethOrchestratorAddress.String()).
				Err(err).
				Msg("Could not get ethereum balance")
		}

		sublogger.Debug().
			Str("ethereum_orchestrator_address", ethOrchestratorAddress.String()).
			Float64("request_time", time.Since(queryStart).Seconds()).
			Uint64("balance", ethBal.Uint64()).
			Msg("Finished querying balance")

		tokensRatio, _ := ToNativeBalance(ethBal)

		gravEthOrchBalanceGauge.With(prometheus.Labels{
			"cudos_orchestrator_address":    cudosOrchestratorAddress.String(),
			"ethereum_orchestrator_address": ethOrchestratorAddress.String(),
		}).Set(tokensRatio)
	}()
	wg.Add(1)

	wg.Wait()

	h := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	h.ServeHTTP(w, r)
	sublogger.Info().
		Str("method", "GET").
		Str("endpoint", "/metrics/gravity-bridge/wallet?cudos_orchestrator_address="+cudosOrchestratorAddress.String()+"&ethereum_orchestrator_address="+ethOrchestratorAddress.String()).
		Float64("request_time", time.Since(requestStart).Seconds()).
		Msg("Request processed")
}

func GravityBridgeContractHandler(w http.ResponseWriter, r *http.Request, grpcConn *grpc.ClientConn) {
	requestStart := time.Now()

	sublogger := log.With().
		Str("request_id", uuid.New().String()).
		Logger()

	ethConn, err := ethclient.Dial(EthRPC)
	if err != nil {
		log.Fatal().Err(err).Msg("Could not connect to Ethereum node")
	}

	ethTokenAddress := common.HexToAddress(ethTokenContract)
	instance, err := NewMain(ethTokenAddress, ethConn)

	if err != nil {
		log.Fatal().Err(err).Msg("Could not retreive the token contract")
	}

	gravEthContractBalanceGauge := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        "gravity_ethereum_contract_balance",
			Help:        "Balance of the ethereum gravity contract",
			ConstLabels: ConstLabels,
		},
		[]string{},
	)

	registry := prometheus.NewRegistry()
	registry.MustRegister(gravEthContractBalanceGauge)

	sublogger.Debug().
		Str("ethereum_gravity_contract", ethTokenAddress.String()).
		Msg("Started querying gravity ethereum gravity contract balance")
	queryStart := time.Now()
	gravityAddress := common.HexToAddress(ethGravityContract)
	ethBal, err := instance.BalanceOf(&bind.CallOpts{}, gravityAddress)
	if err != nil {
		sublogger.Error().
			Str("ethereum_token_address", ethTokenAddress.String()).
			Err(err).
			Msg("Could not get ethereum token balance")
	}

	sublogger.Debug().
		Str("ethereum_gravity_contract", ethTokenAddress.String()).
		Float64("request_time", time.Since(queryStart).Seconds()).
		Msg("Finished querying gravity ethereum contract token balance")

	tokensRatio, _ := ToNativeBalance(ethBal)
	gravEthContractBalanceGauge.With(nil).Set(tokensRatio)

	h := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	h.ServeHTTP(w, r)
	sublogger.Info().
		Str("method", "GET").
		Str("endpoint", "/metrics/gravity-bridge/contract").
		Float64("request_time", time.Since(requestStart).Seconds()).
		Msg("Request processed")
}
