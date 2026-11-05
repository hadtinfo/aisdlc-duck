package harness

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"aisdlc/internal/harness/harnesstest"
	"aisdlc/internal/spec"
)

const kafkaTestImage = "apache/kafka:3.8.0"

var (
	kafkaOnce    sync.Once
	kafkaBrokers string
	kafkaStop    func(context.Context) error
	kafkaErr     error
)

// sharedKafka khởi động lười một Kafka dùng chung cho cả package; dọn trong TestMain.
func sharedKafka(t *testing.T) string {
	t.Helper()
	harnesstest.RequireDocker(t)
	kafkaOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		kafkaBrokers, kafkaStop, kafkaErr = StartKafka(ctx, kafkaTestImage, []string{"transfer.created", "other"}, "harness-test")
	})
	if kafkaErr != nil {
		t.Fatalf("không khởi động được Kafka: %v", kafkaErr)
	}
	return kafkaBrokers
}

func produce(t *testing.T, brokers, topic, value string) {
	t.Helper()
	cl, err := kgo.NewClient(kgo.SeedBrokers(brokers), kgo.AllowAutoTopicCreation())
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	if err := cl.ProduceSync(context.Background(), &kgo.Record{Topic: topic, Value: []byte(value)}).FirstErr(); err != nil {
		t.Fatal(err)
	}
}

func want(topic string, value any) []spec.MessageExpect {
	return []spec.MessageExpect{{Topic: topic, Value: value}}
}

func newProbe(t *testing.T, brokers string, timeout time.Duration) *KafkaProbe {
	t.Helper()
	p := NewKafkaProbe(brokers, []string{"transfer.created", "other"}, timeout)
	t.Cleanup(p.Close)
	return p
}

func TestKafkaProbeMatchesProducedMessage(t *testing.T) {
	b := sharedKafka(t)
	p := newProbe(t, b, 10*time.Second)
	ctx := context.Background()
	if err := p.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	produce(t, b, "transfer.created", `{"id":7,"amount":100,"extra":"x"}`)
	msg, err := p.Check(ctx, want("transfer.created", map[string]any{"id": float64(7), "amount": float64(100)}))
	if err != nil || msg != "" {
		t.Fatalf("msg=%q err=%v", msg, err)
	}
}

func TestKafkaProbeIgnoresMessagesBeforeReset(t *testing.T) {
	b := sharedKafka(t)
	p := newProbe(t, b, 2*time.Second)
	ctx := context.Background()
	produce(t, b, "other", `{"old":true}`)
	if err := p.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	msg, err := p.Check(ctx, want("other", map[string]any{"old": true}))
	if err != nil || !strings.HasPrefix(msg, "messages[0]: không nhận được message khớp trên topic other") {
		t.Fatalf("msg=%q err=%v", msg, err)
	}
}

func TestKafkaProbeTimeoutMessage(t *testing.T) {
	b := sharedKafka(t)
	p := newProbe(t, b, time.Second)
	ctx := context.Background()
	if err := p.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	produce(t, b, "transfer.created", `{"id":1}`)
	start := time.Now()
	msg, err := p.Check(ctx, want("transfer.created", map[string]any{"id": float64(2)}))
	if err != nil {
		t.Fatal(err)
	}
	if msg != "messages[0]: không nhận được message khớp trên topic transfer.created trong 1s" {
		t.Fatalf("msg=%q", msg)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("chờ quá lâu: %v", time.Since(start))
	}
}

func TestKafkaProbeCheckBeforeReset(t *testing.T) {
	p := NewKafkaProbe("127.0.0.1:1", []string{"a"}, time.Second)
	defer p.Close()
	_, err := p.Check(context.Background(), want("a", 1))
	if err == nil || err.Error() != "Kafka: chưa reset probe hoặc topic không nằm trong bộ case" {
		t.Fatalf("err=%v", err)
	}
	if msg, err := p.Check(context.Background(), nil); msg != "" || err != nil {
		t.Fatalf("msg=%q err=%v", msg, err)
	}
}

func TestStartKafkaCreatesTopics(t *testing.T) {
	b := sharedKafka(t)
	cl, err := kgo.NewClient(kgo.SeedBrokers(b), kgo.AllowAutoTopicCreation())
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	ctx := context.Background()
	ad := kadm.NewClient(cl)
	topics, err := ad.ListTopics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"transfer.created", "other"} {
		if !topics.Has(name) {
			t.Errorf("thiếu topic %s", name)
		}
	}
	// auto-create bật: produce vào topic chưa khai báo vẫn thành công.
	if err := cl.ProduceSync(ctx, &kgo.Record{Topic: "auto.created", Value: []byte("x")}).FirstErr(); err != nil {
		t.Fatalf("auto-create tắt: %v", err)
	}
}
