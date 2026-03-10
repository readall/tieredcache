package multitiercache

import (
	"context"
	"log/slog"

	"github.com/IBM/sarama"
)

// KafkaTier implements Tier for real-time streaming (sarama async producer).
type KafkaTier struct {
	producer sarama.AsyncProducer
	topic    string
	logger   *slog.Logger
}

func NewKafkaTier(brokers []string, topic string) (Tier, error) {
	config := sarama.NewConfig()
	config.Producer.Return.Successes = false
	config.Producer.RequiredAcks = sarama.WaitForLocal
	producer, err := sarama.NewAsyncProducer(brokers, config)
	if err != nil {
		return nil, err
	}
	return &KafkaTier{producer: producer, topic: topic, logger: slog.Default()}, nil
}

func (k *KafkaTier) Name() string { return "kafka" }

func (k *KafkaTier) PutBatch(ctx context.Context, items []TierItem) error {
	for _, item := range items {
		select {
		case k.producer.Input() <- &sarama.ProducerMessage{
			Topic: k.topic,
			Key:   sarama.ByteEncoder(item.Key),
			Value: sarama.ByteEncoder(item.Value),
		}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Get is not supported for Kafka (append-only log).
// Returns nil, nil to indicate key not found (promotion will skip this tier).
func (k *KafkaTier) Get(ctx context.Context, key []byte) ([]byte, error) {
	return nil, nil
}

// Delete is not supported for Kafka (append-only log).
func (k *KafkaTier) Delete(ctx context.Context, key []byte) error {
	return nil
}
