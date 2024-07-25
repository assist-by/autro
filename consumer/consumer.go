package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
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

	for i := 0; i < 5; i++ {
		consumer, err := sarama.NewConsumer(brokers, config)
		if err == nil {
			return consumer, nil
		}
		fmt.Printf("Failed to connect to Kafka, retrying in 5 seconds... (attempt %d/5)\n", i+1)
		time.Sleep(5 * time.Second)
	}
	return nil, fmt.Errorf("failed to connect to Kafka after 5 attempts")
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

	for {
		select {
		case msg := <-partitionConsumer.Messages():
			var candle CandleData
			err := json.Unmarshal(msg.Value, &candle)
			if err != nil {
				fmt.Printf("Error unmarshalling message: %v\n", err)
				continue
			}
			fmt.Printf("Received BTCUSDT 1-minute candle:\n")
			fmt.Printf("Open Time: %v\n", time.Unix(candle.OpenTime/1000, 0))
			fmt.Printf("Open:  %s\n", candle.Open)
			fmt.Printf("High:  %s\n", candle.High)
			fmt.Printf("Low:   %s\n", candle.Low)
			fmt.Printf("Close: %s\n", candle.Close)
			fmt.Println("------------------------")
		case <-signals:
			return
		}
	}
}
