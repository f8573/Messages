package bus

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
)

type IngressProducer interface {
	PublishIngress(ctx context.Context, conversationID string, payload any) error
}

type KafkaProducer struct {
	ingress *kafka.Writer
}

func NewKafkaProducer(brokersCSV, clientID, ingressTopic string) *KafkaProducer {
	brokers := splitCSV(brokersCSV)
	if len(brokers) == 0 {
		brokers = []string{"localhost:9092"}
	}
	return &KafkaProducer{
		ingress: &kafka.Writer{
			Addr:         kafka.TCP(brokers...),
			Topic:        ingressTopic,
			Balancer:     &kafka.Hash{},
			RequiredAcks: kafka.RequireAll,
			BatchTimeout: 10 * time.Millisecond,
			Async:        false,
			Transport: &kafka.Transport{
				ClientID: clientID,
			},
		},
	}
}

func (p *KafkaProducer) PublishIngress(ctx context.Context, conversationID string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return p.ingress.WriteMessages(ctx, kafka.Message{
		Key:   []byte(conversationID),
		Value: body,
		Time:  time.Now().UTC(),
	})
}

func (p *KafkaProducer) Close() error {
	if p == nil || p.ingress == nil {
		return nil
	}
	return p.ingress.Close()
}

func splitCSV(v string) []string {
	raw := strings.Split(v, ",")
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		trim := strings.TrimSpace(item)
		if trim == "" {
			continue
		}
		out = append(out, trim)
	}
	return out
}
