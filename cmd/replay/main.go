package main

import (
	"context"
	"encoding/json"
	"example.com/marketplace/internal/platform/config"
	"example.com/marketplace/internal/platform/kafka"
	"flag"
	"fmt"
	"github.com/twmb/franz-go/pkg/kgo"
	"os"
	"strings"
	"time"
)

func main() {
	topic := flag.String("topic", "", "DLQ topic")
	partition := flag.Int("partition", -1, "partition")
	offset := flag.Int64("offset", -1, "offset")
	flag.Parse()
	if !strings.HasSuffix(*topic, ".dlq") || *partition < 0 || *offset < 0 {
		fmt.Fprintln(os.Stderr, "Provide --topic <*.dlq> --partition N --offset N")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	brokers := strings.Split(config.Env("KAFKA_BROKERS", "127.0.0.1:19092"), ",")
	opts := append(kafka.Options(brokers), kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{*topic: {int32(*partition): kgo.NewOffset().At(*offset)}}))
	client, e := kgo.NewClient(opts...)
	if e != nil {
		panic(e)
	}
	defer client.Close()
	fetch := client.PollRecords(ctx, 1)
	records := fetch.Records()
	if len(records) != 1 || records[0].Offset != *offset {
		fmt.Fprintln(os.Stderr, "Requested record not available")
		os.Exit(1)
	}
	var record struct{ Topic, Key, Value string }
	if e = json.Unmarshal(records[0].Value, &record); e != nil {
		panic(e)
	}
	if record.Topic != "marketplace.listings.v1" && record.Topic != "marketplace.messages.v1" {
		fmt.Fprintln(os.Stderr, "Not a marketplace source topic")
		os.Exit(1)
	}
	var event kafka.Envelope
	if e = json.Unmarshal([]byte(record.Value), &event); e != nil {
		panic(e)
	}
	if e = event.Validate(); e != nil {
		fmt.Fprintln(os.Stderr, "Invalid event; repair the contract before replay")
		os.Exit(1)
	}
	producer, e := kafka.NewProducer(brokers)
	if e != nil {
		panic(e)
	}
	defer producer.Close()
	if e = producer.Publish(ctx, record.Topic, record.Key, event); e != nil {
		panic(e)
	}
	fmt.Println("Replayed event", event.EventID)
}
