package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/IBM/sarama"
)

const (
	binanceKlineAPI = "https://api.binance.com/api/v3/klines"
	kafkaTopic      = "btcusdt-1m-candles"
	maxRetries      = 5
	retryDelay      = 5 * time.Second
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

func fetchBTCCandleData() (*CandleData, error) {
	url := fmt.Sprintf("%s?symbol=BTCUSDT&interval=1m&limit=1", binanceKlineAPI)
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

	if len(klines) == 0 {
		return nil, fmt.Errorf("no kline data received")
	}

	kline := klines[0]
	candle := &CandleData{
		OpenTime: int64(kline[0].(float64)),
		Open:     kline[1].(string),
		High:     kline[2].(string),
		Low:      kline[3].(string),
		Close:    kline[4].(string),
	}

	return candle, nil
}

func produceToKafka(producer sarama.SyncProducer, candle *CandleData) error {
	jsonData, err := json.Marshal(candle)
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

func main() {
	producer, err := connectProducer([]string{kafkaBroker})
	if err != nil {
		panic(err)
	}
	defer producer.Close()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		candle, err := fetchBTCCandleData()
		if err != nil {
			fmt.Printf("Error fetching candle data: %v\n", err)
		} else {
			err = produceToKafka(producer, candle)
			if err != nil {
				fmt.Printf("Error producing to Kafka: %v\n", err)
			} else {
				fmt.Println("Successfully sent candle data to Kafka")
			}
		}

		<-ticker.C
	}
}
