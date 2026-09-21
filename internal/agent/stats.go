package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type statsResponse struct {
	Stats []struct {
		Name  string          `json:"name"`
		Value json.RawMessage `json:"value"`
	} `json:"stat"`
}

func collectStats(ctx context.Context, runtime RuntimeConfig, run func(context.Context, string, ...string) ([]byte, error)) (map[string]Counter, error) {
	output, err := run(ctx, runtime.XrayBinary, "api", "statsquery", "--server="+runtime.StatsAddress, "-pattern", "user>>>")
	if err != nil {
		return nil, fmt.Errorf("query Xray stats: %w: %s", err, truncate(output))
	}
	return parseStats(output)
}

func parseStats(data []byte) (map[string]Counter, error) {
	var response statsResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, err
	}
	values := map[string]Counter{}
	seen := map[string]bool{}
	for _, item := range response.Stats {
		if !strings.HasPrefix(item.Name, "user>>>") {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(item.Name, "user>>>"), ">>>traffic>>>")
		if len(parts) != 2 || parts[0] == "" || (parts[1] != "uplink" && parts[1] != "downlink") {
			return nil, errors.New("Xray returned invalid user counter name")
		}
		key := parts[0] + ":" + parts[1]
		if seen[key] {
			return nil, errors.New("Xray returned duplicate user counter")
		}
		seen[key] = true
		parsed, err := strconv.ParseInt(strings.Trim(string(item.Value), `"`), 10, 64)
		if err != nil || parsed < 0 {
			return nil, errors.New("Xray returned invalid byte counter")
		}
		counter := values[parts[0]]
		if parts[1] == "uplink" {
			counter.Uplink = parsed
		} else {
			counter.Downlink = parsed
		}
		values[parts[0]] = counter
	}
	return values, nil
}

func updateBoardLessUsage(state *persistentState, snapshot *BoardLessSnapshot, current map[string]Counter, now time.Time) {
	if snapshot == nil {
		return
	}
	entries := make([]UsageEntry, 0)
	for statsID, value := range current {
		userID, ok := boardLessUserID(statsID)
		if !ok {
			continue
		}
		if value.Uplink != 0 || value.Downlink != 0 {
			entries = append(entries, UsageEntry{UserID: userID, UpBytes: value.Uplink, DownBytes: value.Downlink})
		}
	}
	for len(entries) > 0 {
		count := 40
		if len(entries) < count {
			count = len(entries)
		}
		state.Pending = append(state.Pending, PendingUsage{ReportID: reportID(snapshot.Node.ID, now), Entries: append([]UsageEntry(nil), entries[:count]...)})
		entries = entries[count:]
		now = now.Add(time.Nanosecond)
	}
}

func counterDeltas(state *persistentState, current map[string]Counter) map[string]Counter {
	deltas := make(map[string]Counter, len(current))
	for statsID, value := range current {
		previous := state.Counters[statsID]
		deltas[statsID] = Counter{
			Uplink:   counterDelta(value.Uplink, previous.Uplink),
			Downlink: counterDelta(value.Downlink, previous.Downlink),
		}
		state.Counters[statsID] = value
	}
	return deltas
}

func countersWithPrefix(current map[string]Counter, prefix string) map[string]Counter {
	filtered := map[string]Counter{}
	for statsID, value := range current {
		if strings.HasPrefix(statsID, prefix) {
			filtered[statsID] = value
		}
	}
	return filtered
}

func rewindCounterDeltas(state *persistentState, current, deltas map[string]Counter, prefix string) {
	for statsID, delta := range deltas {
		if !strings.HasPrefix(statsID, prefix) {
			continue
		}
		value := current[statsID]
		state.Counters[statsID] = Counter{Uplink: value.Uplink - delta.Uplink, Downlink: value.Downlink - delta.Downlink}
	}
}

func counterDelta(current, previous int64) int64 {
	if current >= previous {
		return current - previous
	}
	return current
}
