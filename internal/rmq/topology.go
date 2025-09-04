package rmq

import (
	"strings"

	"github.com/henok3878/distributed-task-queue/internal/config"
)

type Topology struct {
	Namespace    string   // ex: "tasks"
	MainExchange string   // "<ns>.direct"
	MainKind     string   // "direct"
	DLXExchange  string   // "<ns>.dlx"
	DLXKind      string   // "direct"
	QueuePrefix  string   // "<ns>"
	DLQName      string   // "<ns>.dlq"
	RoutingKeys  []string // e.g. ["default","high"]
}

func Load() (Topology, error) {
	ns, err := config.GetFromEnv("RMQ_NAMESPACE")
	if err != nil {
		return Topology{}, err
	}

	queuesCSV, err := config.GetFromEnv("QUEUES") // ex: "default,high"
	if err != nil {
		return Topology{}, err
	}

	var rks []string
	for _, s := range strings.Split(queuesCSV, ",") {
		if v := strings.TrimSpace(s); v != "" {
			rks = append(rks, v)
		}
	}

	t := Topology{
		Namespace:    ns,
		MainExchange: ns + ".direct",
		MainKind:     "direct",
		DLXExchange:  ns + ".dlx",
		DLXKind:      "direct",
		QueuePrefix:  ns,
		DLQName:      ns + ".dlq",
		RoutingKeys:  rks,
	}
	return t, nil
}

func (t Topology) FullQueueName(rk string) string {
	return t.QueuePrefix + "." + rk
}

func (t Topology) RetryQueueName(rk string) string {
	return t.Namespace + ".retry." + rk
}
