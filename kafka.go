
require (
	. multitiercache
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