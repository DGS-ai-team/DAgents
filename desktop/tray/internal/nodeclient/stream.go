package nodeclient

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// StreamEvent 为一条 SSE 业务事件。
type StreamEvent struct {
	Type      string
	SessionID string
	AgentID   string
	Seq       int
	Data      map[string]any
}

// StreamEvents 订阅 GET /v1/streams?live=1（全局）；handler 返回 false 时结束。
func (c *Client) StreamEvents(ctx context.Context, handler func(StreamEvent) bool) error {
	if c == nil || c.base == "" {
		return fmt.Errorf("node client: empty base URL")
	}
	q := url.Values{}
	q.Set("live", "1")
	streamURL := c.base + "/v1/streams?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	c.setAuth(req)

	streamClient := &http.Client{Timeout: 0}
	resp, err := streamClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /v1/streams: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("GET /v1/streams: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return parseSSE(ctx, resp.Body, handler)
}

func parseSSE(ctx context.Context, r io.Reader, handler func(StreamEvent) bool) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var eventType, eventID, dataLine string
	flush := func() error {
		if dataLine == "" {
			eventType, eventID, dataLine = "", "", ""
			return nil
		}
		ev, err := decodeStreamEvent(eventType, eventID, dataLine)
		if err != nil {
			return err
		}
		if !handler(ev) {
			return io.EOF
		}
		eventType, eventID, dataLine = "", "", ""
		return nil
	}

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				if err == io.EOF {
					return nil
				}
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "id:") {
			eventID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		} else if strings.HasPrefix(line, "data:") {
			dataLine = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flush()
}

func decodeStreamEvent(eventType, eventID, dataLine string) (StreamEvent, error) {
	var envelope struct {
		AgentID string         `json:"agent_id"`
		Type    string         `json:"type"`
		Seq     int            `json:"seq"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(dataLine), &envelope); err != nil {
		return StreamEvent{}, fmt.Errorf("decode sse data: %w", err)
	}
	typ := eventType
	if typ == "" {
		typ = envelope.Type
	}
	seq := envelope.Seq
	if seq == 0 && eventID != "" {
		seq, _ = strconv.Atoi(eventID)
	}
	data := envelope.Data
	if data == nil {
		data = map[string]any{}
	}
	aid := strings.TrimSpace(envelope.AgentID)
	return StreamEvent{
		Type:      typ,
		SessionID: aid, // 与 AgentID 同源（待办键历史字段）
		AgentID:   aid,
		Seq:       seq,
		Data:      data,
	}, nil
}
