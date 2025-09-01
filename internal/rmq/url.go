package rmq

import (
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/henok3878/distributed-task-queue/internal/config"
)

func URLFromEnv() (string, error) {

	if raw := strings.TrimSpace(os.Getenv("RMQ_URL")); raw != "" {
		return raw, nil
	}

	user, err := config.GetFromEnv("RMQ_USER")
	if err != nil {
		return "", err
	}
	pass, err := config.GetFromEnv("RMQ_PASS")
	if err != nil {
		return "", err
	}
	host, err := config.GetFromEnv("RMQ_HOST")
	if err != nil {
		return "", err
	}
	port, err := config.GetFromEnv("RMQ_PORT")
	if err != nil {
		return "", err
	}
	vhost, err := config.GetFromEnv("RMQ_VHOST")
	if err != nil {
		return "", err
	}

	u := &url.URL{
		Scheme: "amqp",
		Host:   net.JoinHostPort(host, port),
		Path:   "/",
	}
	if vhost != "/" {
		u.Path = "/" + url.PathEscape(strings.TrimPrefix(vhost, "/"))
	}
	u.User = url.UserPassword(user, pass)

	return u.String(), nil
}
