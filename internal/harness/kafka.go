package harness

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"

	"aisdlc/internal/i18n"
	"aisdlc/internal/spec"
)

const (
	kafkaPort       = "9092/tcp"
	kafkaStarter    = "/tmp/aisdlc_start.sh"
	defaultKafkaTTL = 5 * time.Second
	kafkaPoll       = 200 * time.Millisecond
	// kafkaAssignTimeout là hạn Reset chờ broker trả offset cuối của các topic (độc lập với timeout của Check).
	kafkaAssignTimeout = 30 * time.Second
)

// kafkaStarterScript đặt advertised listener theo cổng đã map ra host rồi chạy entrypoint của apache/kafka (KRaft).
const kafkaStarterScript = `#!/bin/bash
export KAFKA_ADVERTISED_LISTENERS=PLAINTEXT://%s,BROKER://%s:9094
exec /etc/kafka/docker/run
`

// StartKafka khởi động Kafka KRaft một node (image apache/kafka) gắn nhãn aisdlc.run=<runID>, bật auto-create topic
// và tạo sẵn các topic khai trong topics. stop dừng và xóa container.
// Module testcontainers kafka chỉ hỗ trợ confluent-local nên dùng container tổng quát.
func StartKafka(ctx context.Context, image string, topics []string, runID string) (string, func(context.Context) error, error) {
	c, err := testcontainers.Run(ctx, image,
		testcontainers.WithExposedPorts(kafkaPort),
		testcontainers.WithLabels(map[string]string{"aisdlc.run": runID}),
		testcontainers.WithEnv(map[string]string{
			"KAFKA_NODE_ID":                                  "1",
			"KAFKA_PROCESS_ROLES":                            "broker,controller",
			"KAFKA_LISTENERS":                                "PLAINTEXT://0.0.0.0:9092,BROKER://0.0.0.0:9094,CONTROLLER://0.0.0.0:9093",
			"KAFKA_LISTENER_SECURITY_PROTOCOL_MAP":           "PLAINTEXT:PLAINTEXT,BROKER:PLAINTEXT,CONTROLLER:PLAINTEXT",
			"KAFKA_INTER_BROKER_LISTENER_NAME":               "BROKER",
			"KAFKA_CONTROLLER_LISTENER_NAMES":                "CONTROLLER",
			"KAFKA_CONTROLLER_QUORUM_VOTERS":                 "1@localhost:9093",
			"KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR":         "1",
			"KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR": "1",
			"KAFKA_TRANSACTION_STATE_LOG_MIN_ISR":            "1",
			"KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS":         "0",
			"KAFKA_AUTO_CREATE_TOPICS_ENABLE":                "true",
			"KAFKA_NUM_PARTITIONS":                           "1",
		}),
		testcontainers.WithEntrypoint("sh"),
		testcontainers.WithCmd("-c", "while [ ! -f "+kafkaStarter+" ]; do sleep 0.1; done; bash "+kafkaStarter),
		testcontainers.WithLifecycleHooks(testcontainers.ContainerLifecycleHooks{
			PostStarts: []testcontainers.ContainerHook{func(ctx context.Context, c testcontainers.Container) error {
				if err := wait.ForMappedPort(kafkaPort).WaitUntilReady(ctx, c); err != nil {
					return err
				}
				ep, err := c.PortEndpoint(ctx, kafkaPort, "")
				if err != nil {
					return err
				}
				info, err := c.Inspect(ctx)
				if err != nil {
					return err
				}
				script := fmt.Sprintf(kafkaStarterScript, ep, info.Config.Hostname)
				if err := c.CopyToContainer(ctx, []byte(script), kafkaStarter, 0o755); err != nil {
					return err
				}
				return wait.ForLog("Kafka Server started").WithStartupTimeout(120*time.Second).WaitUntilReady(ctx, c)
			}},
		}),
	)
	stop := func(ctx context.Context) error { return testcontainers.TerminateContainer(c) }
	fail := func(err error) (string, func(context.Context) error, error) {
		_ = stop(context.WithoutCancel(ctx))
		return "", nil, err
	}
	if err != nil {
		return fail(err)
	}
	brokers, err := c.PortEndpoint(ctx, kafkaPort, "")
	if err != nil {
		return fail(err)
	}
	if err := createTopics(ctx, brokers, topics); err != nil {
		return fail(err)
	}
	return brokers, stop, nil
}

func createTopics(ctx context.Context, brokers string, topics []string) error {
	if len(topics) == 0 {
		return nil
	}
	cl, err := kgo.NewClient(kgo.SeedBrokers(brokers))
	if err != nil {
		return err
	}
	defer cl.Close()
	res, err := kadm.NewClient(cl).CreateTopics(ctx, 1, 1, nil, topics...)
	if err != nil {
		return err
	}
	for _, r := range res {
		if r.Err != nil && !errors.Is(r.Err, kerr.TopicAlreadyExists) {
			return fmt.Errorf(i18n.T("tạo topic %s: %w"), r.Topic, r.Err)
		}
	}
	return nil
}

// KafkaProbe cài MessageProbe bằng franz-go.
type KafkaProbe struct {
	brokers string
	topics  []string
	timeout time.Duration

	cl       *kgo.Client
	received []*kgo.Record
}

var _ MessageProbe = (*KafkaProbe)(nil)

// NewKafkaProbe tạo probe; timeout <= 0 nghĩa là 5s.
func NewKafkaProbe(brokers string, topics []string, timeout time.Duration) *KafkaProbe {
	if timeout <= 0 {
		timeout = defaultKafkaTTL
	}
	return &KafkaProbe{brokers: brokers, topics: topics, timeout: timeout}
}

// Close đóng consumer.
func (p *KafkaProbe) Close() {
	if p.cl != nil {
		p.cl.Close()
		p.cl = nil
	}
}

// Reset chốt vị trí cuối log hiện tại của từng partition (ListEndOffsets) rồi tạo consumer đọc trực tiếp các partition
// đó từ đúng offset ấy (ConsumePartitions, không dùng consumer group): message phát sau Reset luôn được thấy, message
// trước Reset thì không. Topic chưa có thì được tạo (broker bật auto-create). Việc chờ broker sẵn sàng có hạn riêng
// kafkaAssignTimeout, độc lập với timeout của Check, vì lần Reset đầu tiên thường gặp broker vừa khởi động.
func (p *KafkaProbe) Reset(ctx context.Context) error {
	p.Close()
	p.received = nil
	if len(p.topics) == 0 {
		return nil
	}
	ends, err := p.endOffsets(ctx)
	if err != nil {
		return err
	}
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(p.brokers),
		kgo.ClientID(fmt.Sprintf("aisdlc-probe-%016x", uint64(randUint32())<<32|uint64(randUint32()))),
		kgo.ConsumePartitions(ends.KOffsets()),
	)
	if err != nil {
		return err
	}
	p.cl = cl
	return nil
}

// endOffsets lặp ListEndOffsets (tạo topic còn thiếu) tới khi mọi topic có offset cuối cho mọi partition, tối đa
// kafkaAssignTimeout; lỗi cuối cùng được đưa vào thông điệp.
func (p *KafkaProbe) endOffsets(ctx context.Context) (kadm.ListedOffsets, error) {
	adm, err := kgo.NewClient(kgo.SeedBrokers(p.brokers))
	if err != nil {
		return nil, err
	}
	defer adm.Close()
	ad := kadm.NewClient(adm)
	wctx, cancel := context.WithTimeout(ctx, kafkaAssignTimeout)
	defer cancel()
	var last error
	for {
		ends, err := ad.ListEndOffsets(wctx, p.topics...)
		if err == nil {
			var missing []string
			for _, t := range p.topics {
				ps, ok := ends[t]
				if _, unknown := ps[-1]; !ok || unknown {
					missing = append(missing, t)
				}
			}
			switch {
			case len(missing) > 0:
				last = fmt.Errorf(i18n.T("chưa có topic %v"), missing)
				if res, cerr := ad.CreateTopics(wctx, -1, -1, nil, missing...); cerr != nil {
					last = cerr
				} else if rerr := res.Error(); rerr != nil && !errors.Is(rerr, kerr.TopicAlreadyExists) {
					last = rerr
				}
			case ends.Error() != nil:
				last = ends.Error()
			default:
				return ends, nil
			}
		} else {
			last = err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-wctx.Done():
			return nil, fmt.Errorf(i18n.T("Kafka: không được gán partition cho %v trong %ds: %v"), p.topics, int(kafkaAssignTimeout.Seconds()), last)
		case <-time.After(kafkaPoll):
		}
	}
}

// Check poll tới khi mỗi kỳ vọng có một message khớp (value so lenient bằng CompareJSON) trên đúng topic.
func (p *KafkaProbe) Check(ctx context.Context, want []spec.MessageExpect) (string, error) {
	if len(want) == 0 {
		return "", nil
	}
	if p.cl == nil {
		return "", errors.New(i18n.T("Kafka: chưa reset probe hoặc topic không nằm trong bộ case"))
	}
	deadline := time.Now().Add(p.timeout)
	for {
		pctx, cancel := context.WithTimeout(ctx, kafkaPoll)
		fetches := p.cl.PollFetches(pctx)
		cancel()
		if err := ctx.Err(); err != nil {
			return "", err
		}
		fetches.EachRecord(func(r *kgo.Record) { p.received = append(p.received, r) })
		missing := p.firstUnmatched(want)
		if missing == "" {
			return "", nil
		}
		if time.Now().After(deadline) {
			return missing, nil
		}
	}
}

func (p *KafkaProbe) firstUnmatched(want []spec.MessageExpect) string {
	for i, e := range want {
		found := false
		for _, r := range p.received {
			if r.Topic == e.Topic && r.Value != nil && CompareJSON(e.Value, string(r.Value)) == "" {
				found = true
				break
			}
		}
		if !found {
			return i18n.Tf("messages[%d]: không nhận được message khớp trên topic %s trong %ds", i, e.Topic, int(p.timeout.Seconds()))
		}
	}
	return ""
}
