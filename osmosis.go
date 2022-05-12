package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func OsmosisHandler(w http.ResponseWriter, r *http.Request) {
	requestStart := time.Now()

	sublogger := log.With().
		Str("request_id", uuid.New().String()).
		Logger()

	poolId := r.URL.Query().Get("pool_id")
	priceDenoms := r.URL.Query().Get("price_denoms")

	// Get osmosis data
	client := newRestClient("lcd-osmosis.blockapsis.com")

	wg := new(sync.WaitGroup)

	osmosisPoolRes := poolResponse{}
	osmosisTotalLiquidityRes := totalLiquidityResponse{}

	wg.Add(1)
	go func() {
		defer wg.Done()
		res, err := client.getPool(poolId)
		if err != nil {
			sublogger.Error().
				Err(err).
				Msg("Issue retreiving the pool")
		}
		osmosisPoolRes = res
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		res, err := client.getTotalLiquidity(poolId)
		if err != nil {
			sublogger.Error().
				Err(err).
				Msg("Could not retrieve pools total liquidity")
		}
		osmosisTotalLiquidityRes = res
	}()

	wg.Wait()

	// Create and register metrics
	osmosisSwapFee := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name:        "osmosis_swap_fee",
			Help:        "",
			ConstLabels: ConstLabels,
		},
	)

	osmosisExitFee := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name:        "osmosis_exit_fee",
			Help:        "",
			ConstLabels: ConstLabels,
		},
	)

	osmosisPoolWeight := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name:        "osmosis_pool_weight",
			Help:        "",
			ConstLabels: ConstLabels,
		},
	)

	osmosisAssetWeight := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        "osmosis_pool_asset_weight",
			Help:        "",
			ConstLabels: ConstLabels,
		},
		[]string{"denom"},
	)

	osmosisAssetAmount := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        "osmosis_pool_asset_amount",
			Help:        "",
			ConstLabels: ConstLabels,
		},
		[]string{"denom"},
	)

	osmosisTotalPoolShares := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:        "osmosis_total_pool_shares",
			Help:        "",
			ConstLabels: ConstLabels,
		},
		[]string{"denom"},
	)

	registry := prometheus.NewRegistry()
	registry.MustRegister(osmosisSwapFee)
	registry.MustRegister(osmosisExitFee)
	registry.MustRegister(osmosisPoolWeight)
	registry.MustRegister(osmosisAssetWeight)
	registry.MustRegister(osmosisAssetAmount)
	registry.MustRegister(osmosisTotalPoolShares)

	// Set metric values
	swapFee, err := strconv.ParseFloat(osmosisPoolRes.Pool.PoolParams.SwapFee, 64)
	if err != nil {
		sublogger.Error().
			Err(err).
			Str("pool_id", poolId).
			Float64("swap_fee", swapFee).
			Msg("Could not set the osmosis swap fee")
	}
	osmosisSwapFee.Set(swapFee)

	exitFee, err := strconv.ParseFloat(osmosisPoolRes.Pool.PoolParams.ExitFee, 64)
	if err != nil {
		sublogger.Error().
			Err(err).
			Str("pool_id", poolId).
			Float64("exit_fee", exitFee).
			Msg("Could not set the osmosis exit fee")
	}
	osmosisExitFee.Set(exitFee)

	poolWeight, err := strconv.ParseFloat(osmosisPoolRes.Pool.TotalWeight, 64)
	if err != nil {
		sublogger.Error().
			Err(err).
			Str("pool_id", poolId).
			Float64("pool_weight", poolWeight).
			Msg("Could not set the osmosis pool weight")
	}
	osmosisPoolWeight.Set(poolWeight)

	wg.Add(1)
	go func() {
		defer wg.Done()
		for _, liquidity := range osmosisTotalLiquidityRes.Liquidity {
			if strings.Contains(priceDenoms, liquidity.Denom) || priceDenoms == "" {
				totalShares, err := strconv.ParseFloat(liquidity.Amount, 64)
				if err != nil {
					sublogger.Error().
						Err(err).
						Str("pool_id", poolId).
						Float64("total_shares", totalShares).
						Msg("Could not set the osmosis total shares")
				}
				osmosisTotalPoolShares.With(prometheus.Labels{"denom": liquidity.Denom}).Set(totalShares)
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for _, asset := range osmosisPoolRes.Pool.PoolAssets {
			assetWeight, err := strconv.ParseFloat(asset.Weight, 64)
			if err != nil {
				sublogger.Error().
					Err(err).
					Str("pool_id", poolId).
					Str("denom", asset.Token.Denom).
					Float64("asset_weight", assetWeight).
					Msg("Could not set the osmosis asset weight")
			}
			osmosisAssetWeight.With(prometheus.Labels{"denom": asset.Token.Denom}).Set(assetWeight)

			assetAmount, err := strconv.ParseFloat(asset.Token.Amount, 64)
			if err != nil {
				sublogger.Error().
					Err(err).
					Str("pool_id", poolId).
					Str("denom", asset.Token.Denom).
					Float64("asset_amount", assetAmount).
					Msg("Could not set the osmosis asset amount")
			}
			osmosisAssetWeight.With(prometheus.Labels{"denom": asset.Token.Denom}).Set(assetWeight)
		}
	}()

	wg.Wait()
	// Serve response
	h := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	h.ServeHTTP(w, r)
	sublogger.Info().
		Str("method", "GET").
		Str("endpoint", "/metrics/osmosis").
		Float64("request_time", time.Since(requestStart).Seconds()).
		Msg("Request processed")
}

type restClient struct {
	url        url.URL
	httpClient *http.Client
}

func newRestClient(host string) *restClient {
	var c restClient
	c.url = url.URL{Host: host, Scheme: "https"}
	c.httpClient = &http.Client{}
	return &c
}

// request makes http request with specified path and optional query
func (client *restClient) request(path string, query string) ([]byte, error) {
	// avoid race condition with concurrent overwrites: work with copy of restClient's url object for each request!
	ref := client.url
	ref.Path = path
	ref.RawQuery = query
	url := ref.ResolveReference(&ref).String()

	req, err := http.NewRequest("GET", url, nil) // will slow down exit while waiting for timeouts, but using http.NewRequestWithContext would more likely create inconsistencies when interrupted with context.Canceled
	if err != nil {
		return nil, fmt.Errorf("error creating request %s: %v", url, err)
	}

	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", "application/json")

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making request %s: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("error making request %s: %s: %s", url, resp.Status, strings.ReplaceAll(strings.ReplaceAll(string(body), "\n", ""), "  ", " "))
	}

	return io.ReadAll(resp.Body)
}

func (client *restClient) getPool(id string) (poolResponse, error) {
	pool := poolResponse{}

	res, err := client.request("/osmosis/gamm/v1beta1/pools/"+id, "")
	if err != nil {
		return pool, err
	}

	err = json.Unmarshal(res, &pool)
	if err != nil {
		return pool, err
	}

	return pool, nil
}

func (client *restClient) getTotalLiquidity(id string) (totalLiquidityResponse, error) {
	totalLiquidity := totalLiquidityResponse{}

	res, err := client.request("/osmosis/gamm/v1beta1/total_liquidity", "")

	if err != nil {
		return totalLiquidity, err
	}

	err = json.Unmarshal(res, &totalLiquidity)
	if err != nil {
		return totalLiquidity, err
	}

	return totalLiquidity, nil
}

type poolResponse struct {
	Pool struct {
		Type       string `json:"@type"`
		Address    string `json:"address"`
		ID         string `json:"id"`
		PoolParams struct {
			SwapFee                  string      `json:"swapFee"`
			ExitFee                  string      `json:"exitFee"`
			SmoothWeightChangeParams interface{} `json:"smoothWeightChangeParams"`
		} `json:"poolParams"`
		FuturePoolGovernor string `json:"future_pool_governor"`
		TotalShares        struct {
			Denom  string `json:"denom"`
			Amount string `json:"amount"`
		} `json:"totalShares"`
		PoolAssets []struct {
			Token struct {
				Denom  string `json:"denom"`
				Amount string `json:"amount"`
			} `json:"token"`
			Weight string `json:"weight"`
		} `json:"poolAssets"`
		TotalWeight string `json:"totalWeight"`
	} `json:"pool"`
}

type totalLiquidityResponse struct {
	Liquidity []struct {
		Denom  string `json:"denom"`
		Amount string `json:"amount"`
	} `json:"liquidity"`
}
