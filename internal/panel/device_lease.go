package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// DeviceLeaseReply is returned by the opt-in XBoard global admission endpoint.
// An HTTP 200 with an invalid or missing body MUST NOT grant access.
type DeviceLeaseReply struct {
	Allowed     bool `json:"allowed"`
	DistinctIPs int  `json:"distinct_ips"`
	LeaseTTLMS  int  `json:"lease_ttl_ms"`
}

// DeviceLease calls the experimental XBoard API with machine credentials.
// This method is intentionally not wired to connection admission yet.
// The caller must provide a bounded context (e.g. two seconds).
func (c *Client) DeviceLease(ctx context.Context, action string, userID int, leaseID, ip string) (DeviceLeaseReply, error) {
	var denied DeviceLeaseReply
	if c == nil || c.machineID <= 0 || c.nodeID <= 0 {
		return denied, errors.New("global device admission requires machine node credentials")
	}
	if action != "acquire" && action != "renew" && action != "release" {
		return denied, errors.New("invalid device lease action")
	}
	if userID <= 0 || leaseID == "" || (action == "acquire" && ip == "") {
		return denied, errors.New("invalid device lease request")
	}
	payload := map[string]interface{}{
		"action": action, "user_id": userID, "lease_id": leaseID,
	}
	if action == "acquire" {
		payload["ip"] = ip
	}
	c.injectAuth(payload)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return denied, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v2/server/device-lease", bytes.NewReader(encoded))
	if err != nil {
		return denied, errors.New("device lease request creation failed")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return denied, errors.New("device lease transport unavailable")
	}
	defer drainAndClose(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return denied, fmt.Errorf("device lease HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return denied, errors.New("device lease response read failure")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return denied, errors.New("device lease response malformed")
	}
	for _, name := range []string{"allowed", "distinct_ips", "lease_ttl_ms"} {
		if _, ok := fields[name]; !ok {
			return denied, errors.New("device lease response missing required field")
		}
	}
	var result DeviceLeaseReply
	if err := json.Unmarshal(raw, &result); err != nil || result.DistinctIPs < 0 || result.LeaseTTLMS < 0 {
		return denied, errors.New("device lease response invalid")
	}
	// A granted acquisition or renewal must carry an unexpired lease.
	if result.Allowed && action != "release" && result.LeaseTTLMS <= 0 {
		return denied, errors.New("device lease grant missing positive TTL")
	}
	return result, nil
}
