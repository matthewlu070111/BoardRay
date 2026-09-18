package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"
)

func runVPSWebSocket(ctx context.Context, client *vpsPanelClient, changed chan<- struct{}) {
	delay := time.Second
	for ctx.Err() == nil {
		if err := connectVPSWebSocket(ctx, client, changed); err != nil && ctx.Err() == nil {
			// Tokens are deliberately excluded from this error path.
			fmt.Fprintf(os.Stderr, "vps-panel websocket: %v\n", err)
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if delay < 30*time.Second {
			delay *= 2
		}
	}
}

func connectVPSWebSocket(ctx context.Context, client *vpsPanelClient, changed chan<- struct{}) error {
	endpoint, err := url.Parse(strings.TrimRight(client.config.PanelURL, "/") + "/api/agent/ws")
	if err != nil {
		return err
	}
	if endpoint.Scheme == "https" {
		endpoint.Scheme = "wss"
	} else {
		endpoint.Scheme = "ws"
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+client.config.AgentToken)
	header.Set("X-VPS-Panel-Agent-Version", client.config.AnnouncedVersion)
	connection, response, err := websocket.Dial(ctx, endpoint.String(), &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		if response != nil {
			response.Body.Close()
		}
		return err
	}
	defer connection.CloseNow()
	connection.SetReadLimit(8 << 10)
	if err := writeWS(ctx, connection, systemInfoMessage(ctx)); err != nil {
		return err
	}
	readErr := make(chan error, 1)
	go func() { readErr <- readVPSMessages(ctx, connection, client, changed) }()
	heartbeat := time.NewTicker(10 * time.Second)
	metrics := time.NewTicker(5 * time.Second)
	defer heartbeat.Stop()
	defer metrics.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = connection.Close(websocket.StatusNormalClosure, "Agent stopped")
			return nil
		case err := <-readErr:
			return err
		case <-heartbeat.C:
			if err := writeWS(ctx, connection, map[string]any{"type": "heartbeat"}); err != nil {
				return err
			}
		case <-metrics.C:
			if err := writeWS(ctx, connection, metricsMessage()); err != nil {
				return err
			}
		}
	}
}

func readVPSMessages(ctx context.Context, connection *websocket.Conn, client *vpsPanelClient, changed chan<- struct{}) error {
	for {
		kind, data, err := connection.Read(ctx)
		if err != nil {
			return err
		}
		var message struct {
			Type    string          `json:"type"`
			Version json.RawMessage `json:"version"`
		}
		if kind != websocket.MessageText || json.Unmarshal(data, &message) != nil {
			return fmt.Errorf("invalid vps-panel websocket message")
		}
		switch message.Type {
		case "config_changed":
			select {
			case changed <- struct{}{}:
			default:
			}
		case "agent_upgrade":
			var version string
			if json.Unmarshal(message.Version, &version) != nil || version == "" {
				return fmt.Errorf("invalid vps-panel upgrade message")
			}
			reportContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_ = client.reportUpgradeUnsupported(reportContext, version)
			cancel()
		default:
			return fmt.Errorf("unknown vps-panel websocket message")
		}
	}
}

func writeWS(ctx context.Context, connection *websocket.Conn, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	writeContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return connection.Write(writeContext, websocket.MessageText, data)
}
