package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/IBM/sarama"
	"github.com/joho/godotenv"
)

const (
	kafkaTopic = "btcusdt-1m-candles"
	maxRetries = 5
	retryDelay = 5 * time.Second
)

type CandleData struct {
	OpenTime                 int64
	Open, High, Low, Close   string
	Volume                   string
	CloseTime                int64
	QuoteAssetVolume         string
	NumberOfTrades           int
	TakerBuyBaseAssetVolume  string
	TakerBuyQuoteAssetVolume string
}

type TechnicalIndicators struct {
	EMA200       float64
	MACD         float64
	Signal       float64
	ParabolicSAR float64
}

type SignalConditions struct {
	Long  [3]bool
	Short [3]bool
}

var (
	kafkaBroker string
	httpClient  = &http.Client{Timeout: 10 * time.Second}
)

var (
	discordWebhookURL string
	slackWebhookURL   string
)

func init() {
	kafkaBroker = os.Getenv("KAFKA_BROKER")
	if kafkaBroker == "" {
		kafkaBroker = "localhost:29092"
	}
}

// / consumer와 연결함수
func connectConsumer(brokers []string) (sarama.Consumer, error) {
	config := sarama.NewConfig()
	config.Consumer.Return.Errors = true

	for i := 0; i < maxRetries; i++ {
		consumer, err := sarama.NewConsumer(brokers, config)
		if err == nil {
			return consumer, nil
		}
		fmt.Printf("Failed to connect to Kafka, retrying in %v... (attempt %d/%d)\n", retryDelay, i+1, maxRetries)
		time.Sleep(retryDelay)
	}
	return nil, fmt.Errorf("failed to connect to Kafka after %d attempts", maxRetries)
}

// / EMA200 계산
func calculateEMA(prices []float64, period int) float64 {
	k := 2.0 / float64(period+1)
	ema := prices[0]
	for i := 1; i < len(prices); i++ {
		ema = prices[i]*k + ema*(1-k)
	}
	return ema
}

// / MACD 계산
func calculateMACD(prices []float64) (float64, float64) {
	ema12 := calculateEMA(prices, 12)
	ema26 := calculateEMA(prices, 26)
	macd := ema12 - ema26
	signal := calculateEMA([]float64{macd}, 9)
	return macd, signal
}

// / Parabolic SAR 계산
func calculateParabolicSAR(highs, lows []float64) float64 {
	af := 0.02
	maxAf := 0.2
	sar := lows[0]
	ep := highs[0]
	isLong := true

	for i := 1; i < len(highs); i++ {
		if isLong {
			sar = sar + af*(ep-sar)
			if highs[i] > ep {
				ep = highs[i]
				af = math.Min(af+0.02, maxAf)
			}
			if sar > lows[i] {
				isLong = false
				sar = ep
				ep = lows[i]
				af = 0.02
			}
		} else {
			sar = sar - af*(sar-ep)
			if lows[i] < ep {
				ep = lows[i]
				af = math.Min(af+0.02, maxAf)
			}
			if sar < highs[i] {
				isLong = true
				sar = ep
				ep = highs[i]
				af = 0.02
			}
		}
	}
	return sar
}

// / 보조 지표 계산
func calculateIndicators(candles []CandleData) (TechnicalIndicators, error) {
	if len(candles) < 300 {
		return TechnicalIndicators{}, fmt.Errorf("insufficient data: need at least 300 candles, got %d", len(candles))
	}

	prices := make([]float64, len(candles))
	highs := make([]float64, len(candles))
	lows := make([]float64, len(candles))

	for i, candle := range candles {
		price, err := strconv.ParseFloat(candle.Close, 64)
		if err != nil {
			return TechnicalIndicators{}, fmt.Errorf("error parsing close price: %v", err)
		}
		prices[i] = price

		high, err := strconv.ParseFloat(candle.High, 64)
		if err != nil {
			return TechnicalIndicators{}, fmt.Errorf("error parsing high price: %v", err)
		}
		highs[i] = high

		low, err := strconv.ParseFloat(candle.Low, 64)
		if err != nil {
			return TechnicalIndicators{}, fmt.Errorf("error parsing low price: %v", err)
		}
		lows[i] = low
	}

	ema200 := calculateEMA(prices, 200)
	macd, signal := calculateMACD(prices)
	parabolicSAR := calculateParabolicSAR(highs, lows)

	return TechnicalIndicators{
		EMA200:       ema200,
		MACD:         macd,
		Signal:       signal,
		ParabolicSAR: parabolicSAR,
	}, nil
}

// signal 생성 함수
func generateSignal(candles []CandleData, indicators TechnicalIndicators) (string, SignalConditions) {
	lastPrice, _ := strconv.ParseFloat(candles[len(candles)-1].Close, 64)
	lastHigh, _ := strconv.ParseFloat(candles[len(candles)-1].High, 64)
	lastLow, _ := strconv.ParseFloat(candles[len(candles)-1].Low, 64)

	conditions := SignalConditions{
		Long: [3]bool{
			lastPrice > indicators.EMA200,
			indicators.MACD > indicators.Signal,
			indicators.ParabolicSAR < lastLow,
		},
		Short: [3]bool{
			lastPrice < indicators.EMA200,
			indicators.MACD < indicators.Signal,
			indicators.ParabolicSAR > lastHigh,
		},
	}

	if conditions.Long[0] && conditions.Long[1] && conditions.Long[2] {
		return "LONG", conditions
	} else if conditions.Short[0] && conditions.Short[1] && conditions.Short[2] {
		return "SHORT", conditions
	}
	return "NO SIGNAL", conditions
}

// Discord에 알림 보내는 함수
func sendDiscordAlert(message string) error {
	payload := map[string]string{"content": message}
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", discordWebhookURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

// Slack에 알림 보내는 함수
func sendSlackAlert(message string) error {
	payload := map[string]string{"text": message}
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", slackWebhookURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

func main() {
	err := godotenv.Load()
	if err != nil {
		fmt.Printf("Error loading .env file")
		panic(err)

	}

	discordWebhookURL = os.Getenv("DISCORD_WEBHOOK_URL")
	if discordWebhookURL == "" {
		fmt.Printf("DISCORD_WEBHOOK_URL is not set in .env file")
		panic(err)
	}

	slackWebhookURL = os.Getenv("SLACK_WEBHOOK_URL")
	if slackWebhookURL == "" {
		fmt.Printf("SLACK_WEBHOOK_URL is not set in .env file")
		panic(err)
	}

	consumer, err := connectConsumer([]string{kafkaBroker})
	if err != nil {
		panic(err)
	}
	defer consumer.Close()

	partitionConsumer, err := consumer.ConsumePartition(kafkaTopic, 0, sarama.OffsetNewest)
	if err != nil {
		panic(err)
	}
	defer partitionConsumer.Close()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)

	for {
		select {
		case msg := <-partitionConsumer.Messages():
			var candles []CandleData
			if err := json.Unmarshal(msg.Value, &candles); err != nil {
				fmt.Printf("Error unmarshalling message: %v\n", err)
				continue
			}

			if len(candles) == 300 {
				indicators, err := calculateIndicators(candles)
				if err != nil {
					fmt.Printf("Error calculating indicators: %v\n", err)
					continue
				}

				signal, conditions := generateSignal(candles, indicators)
				lastCandle := candles[len(candles)-1]

				if signal != "NO SIGNAL" {
					alertMessage := fmt.Sprintf("New signal: %s for BTCUSDT at %v", signal, time.Unix(lastCandle.OpenTime/1000, 0))

					if err := sendDiscordAlert(alertMessage); err != nil {
						fmt.Printf("Error sending Discord alert: %v\n", err)
					}

					// if err := sendSlackAlert(alertMessage); err != nil {
					// 	fmt.Printf("Error sending Slack alert: %v\n", err)
					// }
				}

				fmt.Printf("Signal: %s\n", signal)
				fmt.Printf("Latest candle - Open Time: %v, Close: %s\n",
					time.Unix(lastCandle.OpenTime/1000, 0), lastCandle.Close)
				fmt.Printf("LONG  - CASE 1: %t, CASE 2: %t, CASE 3: %t\n",
					conditions.Long[0], conditions.Long[1], conditions.Long[2])
				fmt.Printf("SHORT - CASE 1: %t, CASE 2: %t, CASE 3: %t\n",
					conditions.Short[0], conditions.Short[1], conditions.Short[2])
				fmt.Println("------------------------")
			}

		case err := <-partitionConsumer.Errors():
			fmt.Printf("Error: %v\n", err)

		case <-signals:
			return
		}
	}
}
