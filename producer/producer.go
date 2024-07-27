package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/IBM/sarama"
)

const (
	binanceKlineAPI = "https://api.binance.com/api/v3/klines"
	kafkaTopic      = "btcusdt-1m-candles"
	maxRetries      = 5
	retryDelay      = 5 * time.Second
	fetchInterval   = 1 * time.Minute
	candleLimit     = 300
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

func fetchBTCCandleData() ([]CandleData, error) {
	url := fmt.Sprintf("%s?symbol=BTCUSDT&interval=1m&limit=%d", binanceKlineAPI, candleLimit)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var klines [][]interface{}
	err = json.Unmarshal(body, &klines)
	if err != nil {
		return nil, err
	}

	candles := make([]CandleData, len(klines))
	for i, kline := range klines {
		candles[i] = CandleData{
			OpenTime:  int64(kline[0].(float64)),
			Open:      kline[1].(string),
			High:      kline[2].(string),
			Low:       kline[3].(string),
			Close:     kline[4].(string),
			Volume:    kline[5].(string),
			CloseTime: int64(kline[6].(float64)),
		}
	}

	return candles, nil
}

func produceToKafka(producer sarama.SyncProducer, candles []CandleData) error {
	jsonData, err := json.Marshal(candles)
	if err != nil {
		return err
	}

	msg := &sarama.ProducerMessage{
		Topic: kafkaTopic,
		Value: sarama.StringEncoder(jsonData),
	}

	_, _, err = producer.SendMessage(msg)
	return err
}

func connectProducer(brokers []string) (sarama.SyncProducer, error) {
	config := sarama.NewConfig()
	config.Producer.Return.Successes = true

	for i := 0; i < maxRetries; i++ {
		producer, err := sarama.NewSyncProducer(brokers, config)
		if err == nil {
			return producer, nil
		}
		fmt.Printf("Failed to connect to Kafka, retrying in %v... (attempt %d/%d)\n", retryDelay, i+1, maxRetries)
		time.Sleep(retryDelay)
	}
	return nil, fmt.Errorf("failed to connect to Kafka after %d attempts", maxRetries)
}

func utcToLocal(utcTime time.Time) time.Time {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		fmt.Printf("Error loading location: %v\n", err)
		return utcTime
	}
	return utcTime.In(loc)
}

func main() {
	producer, err := connectProducer([]string{kafkaBroker})
	if err != nil {
		panic(err)
	}
	defer producer.Close()

	ticker := time.NewTicker(fetchInterval)
	defer ticker.Stop()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)

	for {
		select {
		case <-ticker.C:
			candles, err := fetchBTCCandleData()
			if err != nil {
				fmt.Printf("Error fetching candle data: %v\n", err)
				continue
			}

			err = produceToKafka(producer, candles)
			if err != nil {
				fmt.Printf("Error producing to Kafka: %v\n", err)
			} else {
				fmt.Printf("Successfully sent %d candle data to Kafka\n", len(candles))
				if len(candles) > 0 {
					firstCandle := candles[0]
					lastCandle := candles[len(candles)-1]
					firstTime := utcToLocal(time.Unix(firstCandle.OpenTime/1000, 0))
					lastTime := utcToLocal(time.Unix(lastCandle.CloseTime/1000, 0))
					fmt.Printf("Data range (Local Time): %v to %v\n",
						firstTime.Format("2006-01-02 15:04:05"),
						lastTime.Format("2006-01-02 15:04:05"))
				}
			}

		case <-signals:
			fmt.Println("Interrupt received, shutting down...")
			return
		}
	}
}
