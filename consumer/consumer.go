package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/IBM/sarama"
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

var kafkaBroker string

func init() {
	kafkaBroker = os.Getenv("KAFKA_BROKER")
	if kafkaBroker == "" {
		kafkaBroker = "localhost:29092" // 기본값 설정
	}
}

func connectConsumer(brokers []string) (sarama.Consumer, error) {
	config := sarama.NewConfig()

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

func calculateEMA(prices []float64, period int) float64 {
	k := 2.0 / float64(period+1)
	ema := prices[0]
	for i := 1; i < len(prices); i++ {
		ema = prices[i]*k + ema*(1-k)
	}
	return ema
}

func calculateMACD(prices []float64) (float64, float64) {
	ema12 := calculateEMA(prices, 12)
	ema26 := calculateEMA(prices, 26)
	macd := ema12 - ema26
	signal := calculateEMA([]float64{macd}, 9)
	return macd, signal
}

func generateSignal(candles []CandleData) string {
	if len(candles) < 300 {
		return "Insufficient data"
	}

	prices := make([]float64, len(candles))
	for i, candle := range candles {
		price, err := strconv.ParseFloat(candle.Close, 64)
		if err != nil {
			fmt.Printf("Error parsing close price: %v\n", err)
			return "Error parsing data"
		}
		prices[i] = price
	}

	ema200 := calculateEMA(prices, 200)
	macd, signal := calculateMACD(prices)

	lastPrice, _ := strconv.ParseFloat(candles[len(candles)-1].Close, 64)
	prevMACD, prevSignal := calculateMACD(prices[:len(prices)-1])

	longCondition := lastPrice > ema200 &&
		macd > signal &&
		prevMACD <= prevSignal

	shortCondition := lastPrice < ema200 &&
		macd < signal &&
		prevMACD >= prevSignal

	if longCondition {
		return "LONG"
	} else if shortCondition {
		return "SHORT"
	}
	return "NO SIGNAL"
}

func main() {
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

	var candles []CandleData

	for {
		select {
		case msg := <-partitionConsumer.Messages():
			var candle CandleData
			err := json.Unmarshal(msg.Value, &candle)
			if err != nil {
				fmt.Printf("Error unmarshalling message: %v\n", err)
				continue
			}

			candles = append(candles, candle)
			if len(candles) > 300 {
				candles = candles[1:] // 가장 오래된 캔들 제거
			}

			if len(candles) == 300 {
				signal := generateSignal(candles)
				fmt.Printf("Signal: %s\n", signal)
				fmt.Printf("Latest candle - Open Time: %v, Close: %s\n",
					time.Unix(candle.OpenTime/1000, 0), candle.Close)
				fmt.Println("------------------------")
			}

		case <-signals:
			return
		}
	}
}
