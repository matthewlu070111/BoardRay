package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxResponseBytes = 2 << 20

func jsonRequest(ctx context.Context, client *http.Client, method, endpoint, token string, input, output any, expected ...int) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	accepted := false
	for _, status := range expected {
		accepted = accepted || response.StatusCode == status
	}
	if !accepted {
		var payload struct {
			Error string `json:"error"`
		}
		data, _ := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
		_ = json.Unmarshal(data, &payload)
		message := strings.TrimSpace(payload.Error)
		if message == "" || (token != "" && strings.Contains(message, token)) {
			message = response.Status
		}
		return &statusError{Status: response.StatusCode, Message: message}
	}
	if output == nil || response.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes)).Decode(output); err != nil {
		return fmt.Errorf("decode %s: %w", endpoint, err)
	}
	return nil
}

type statusError struct {
	Status  int
	Message string
}

func (e *statusError) Error() string { return e.Message }
