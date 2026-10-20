//go:build resilience

// Package resilience deliberately restarts only this project's local containers.
package resilience

import (
	"context"
	"example.com/marketplace/internal/demo"
	"fmt"
	"net/http"
	"os/exec"
	"testing"
	"time"
)

func compose(t *testing.T, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", append([]string{"compose", "--env-file", ".local/.env"}, args...)...)
	cmd.Dir = "../.."
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compose %v: %v\n%s", args, err, output)
	}
}

func TestSearchCatchesUpAfterRestartAndOutboxDrainsAfterBrokerRestart(t *testing.T) {
	base := "http://127.0.0.1:8080"
	client := demo.New(base)
	suffix := fmt.Sprint(time.Now().UnixNano())
	if err := client.Account("Проверка восстановления", "recovery-"+suffix+"@example.com", "recovery-test-password"); err != nil {
		t.Fatal(err)
	}
	l, _, err := client.Do("POST", "/listings", map[string]any{"title": "Восстановление " + suffix, "description": "Сохраняется при недоступном брокере", "category_id": "other", "price_minor": 100})
	if err != nil {
		t.Fatal(err)
	}
	id := l["id"].(string)
	if err = client.Upload(id, demo.Photo(0)); err != nil {
		t.Fatal(err)
	}
	l, _, err = client.Do("GET", "/listings/"+id, nil)
	if err != nil {
		t.Fatal(err)
	}
	compose(t, "stop", "-t", "3", "search", "kafka")
	t.Cleanup(func() { compose(t, "start", "kafka", "search") })
	if _, _, err = client.Do("POST", "/listings/"+id+"/publish", map[string]any{"expected_version": l["version"]}); err != nil {
		t.Fatal("publishing must work while Kafka is unavailable:", err)
	}
	if _, status, err := client.Do("GET", "/listings/"+id, nil); err != nil || status != http.StatusOK {
		t.Fatal("listing reads unavailable", status, err)
	}
	compose(t, "start", "kafka")
	// Leave Search stopped until the broker and publisher have recovered.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for {
		cmd := exec.CommandContext(ctx, "docker", "compose", "--env-file", ".local/.env", "exec", "-T", "kafka", "/opt/kafka/bin/kafka-topics.sh", "--bootstrap-server", "localhost:9092", "--list")
		cmd.Dir = "../.."
		if cmd.Run() == nil {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("Kafka did not restart")
		}
		time.Sleep(time.Second)
	}
	compose(t, "start", "search")
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		result, _, err := client.Do("GET", "/search?q="+suffix, nil)
		if err == nil {
			if items, ok := result["items"].([]any); ok && len(items) == 1 && items[0].(map[string]any)["id"] == id {
				return
			}
		}
		time.Sleep(time.Second)
	}
	t.Fatal("outbox event did not reach the search projection after restart")
}
