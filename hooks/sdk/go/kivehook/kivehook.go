// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

// Package kivehook is the client SDK for kive hooks compiled as standalone
// Go binaries (RuntimeBinary). It mirrors the kive.py / kive.ts / kive.rb /
// kive.sh helpers: allocation env accessors plus a client for the hook
// runtime HTTP API (KV store, secrets, demands, semaphores).
//
// A compiled hook has no interpreter, so this package cannot be embedded
// into the job workspace the way the scripted SDKs are. It has zero
// third-party dependencies (stdlib only) so it can either be imported as a
// module (see the package README for a `replace` example) or copied
// directly into a hook's own source tree.
package kivehook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const defaultRuntimeAPIPort = "8080"

const (
	routeStoreKeys        = "/kv"
	routeStoreKeysList    = "/kv/keys"
	routeStoreSecret      = "/kv/secret"
	routeDemands          = "/demands"
	routeSemaphoreAcquire = "/semaphore/acquire"
	routeSemaphoreRelease = "/semaphore/release"
	routeSemaphoreStatus  = "/semaphore/status"
	routeHTTP             = "/http"
)

// AllocationID returns ALLOCATION_ID from the hook exec environment.
func AllocationID() string { return os.Getenv("ALLOCATION_ID") }

// AllocationIP returns ALLOCATION_IP from the hook exec environment.
func AllocationIP() string { return os.Getenv("ALLOCATION_IP") }

// AllocationIndex returns ALLOCATION_INDEX from the hook exec environment.
func AllocationIndex() string { return os.Getenv("ALLOCATION_INDEX") }

// IsAllocationDisabled reports whether DISABLED=1 is set for this allocation.
func IsAllocationDisabled() bool { return os.Getenv("DISABLED") == "1" }

// HookEvent returns the executed_on event that triggered this run (EVENT).
func HookEvent() string { return os.Getenv("EVENT") }

// HookName returns the current hook name (HOOK).
func HookName() string { return os.Getenv("HOOK") }

// JobName returns the current job name (JOB).
func JobName() string { return os.Getenv("JOB") }

// IsOneShot reports whether this run is the first allocation of the first
// batch. Prefer BATCH_ALLOCATIONS[0] over ALLOCATION_INDEX=0: rollout order
// can put catalog index 0 in a later batch, which would otherwise make no
// worker one-shot. Use for logic that must run exactly once per invocation.
func IsOneShot() bool {
	if v := os.Getenv("BATCH_INDEX"); v != "" && v != "0" {
		return false
	}
	if batch := strings.TrimSpace(os.Getenv("BATCH_ALLOCATIONS")); batch != "" {
		first := strings.TrimSpace(strings.SplitN(batch, ",", 2)[0])
		return AllocationIP() == first
	}
	index := os.Getenv("ALLOCATION_INDEX")
	return index == "" || index == "0"
}

// HookDemand is one dependent hook returned by ListHookDemands.
type HookDemand struct {
	Job          string         `json:"job"`
	Hook         string         `json:"hook"`
	DemandConfig map[string]any `json:"demand_config"`
}

// SemaphoreAcquireResult is returned by AcquireSemaphore.
type SemaphoreAcquireResult struct {
	Name         string `json:"name"`
	AllocationID string `json:"allocation_id"`
	Capacity     int    `json:"capacity"`
	Acquired     bool   `json:"acquired"`
}

// SemaphoreStatus describes current holders for a named semaphore.
type SemaphoreStatus struct {
	Name      string   `json:"name"`
	Capacity  int      `json:"capacity"`
	Holders   []string `json:"holders"`
	Waiting   int      `json:"waiting"`
	Available int      `json:"available"`
}

type storeKeyPayload struct {
	Namespace string `json:"namespace"`
	Key       string `json:"key"`
	Value     string `json:"value,omitempty"`
	TTL       int    `json:"ttl,omitempty"`
}

type listKeysResponse struct {
	Namespaces map[string][]string `json:"namespaces"`
}

// Client talks to the kive hook runtime API (HOOK_API_HOST/PORT/TOKEN).
// Construct with NewClient, which reads those from the environment kive
// injects into every hook process.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewClient builds a Client from the HOOK_API_HOST / HOOK_API_PORT /
// HOOK_API_TOKEN environment variables kive sets on every hook invocation.
func NewClient() *Client {
	host := os.Getenv("HOOK_API_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("HOOK_API_PORT")
	if port == "" {
		port = defaultRuntimeAPIPort
	}
	return &Client{
		baseURL: fmt.Sprintf("http://%s:%s", host, port),
		token:   os.Getenv("HOOK_API_TOKEN"),
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) do(method, route string, query url.Values, body any, timeout time.Duration) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}

	target := c.baseURL + route
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	req, err := http.NewRequest(method, target, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-ALLOCATION-ID", AllocationID())
	req.Header.Set("HOOK", HookName())
	req.Header.Set("EVENT", HookEvent())
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	client := c.http
	if timeout > 0 {
		clientCopy := *c.http
		clientCopy.Timeout = timeout
		client = &clientCopy
	}
	return client.Do(req)
}

func decodeJSON(resp *http.Response, out any) error {
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		payload, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("kivehook: %s %s: %s", resp.Request.Method, resp.Request.URL.Path, strings.TrimSpace(string(payload)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// GetStoreValue reads a key from an allowed namespace (GET /kv).
func (c *Client) GetStoreValue(namespace, key string) (string, error) {
	resp, err := c.do(http.MethodGet, routeStoreKeys, nil, storeKeyPayload{Namespace: namespace, Key: key}, 0)
	if err != nil {
		return "", err
	}
	var out storeKeyPayload
	if err := decodeJSON(resp, &out); err != nil {
		return "", err
	}
	return out.Value, nil
}

// PutJobVariable writes a key under vars/job/<current job> (PUT /kv).
// ttlSeconds is optional; 0 or omitted means no expiry.
func (c *Client) PutJobVariable(key, value string, ttlSeconds ...int) error {
	payload := storeKeyPayload{Namespace: "vars/job/" + JobName(), Key: key, Value: value, TTL: firstOrZero(ttlSeconds)}
	resp, err := c.do(http.MethodPut, routeStoreKeys, nil, payload, 0)
	if err != nil {
		return err
	}
	return decodeJSON(resp, nil)
}

// PutJobSecret writes an encrypted key under secrets/job/<current job>
// (PUT /kv/secret). ttlSeconds is optional; 0 or omitted means no expiry.
func (c *Client) PutJobSecret(key, value string, ttlSeconds ...int) error {
	payload := storeKeyPayload{Namespace: "secrets/job/" + JobName(), Key: key, Value: value, TTL: firstOrZero(ttlSeconds)}
	resp, err := c.do(http.MethodPut, routeStoreSecret, nil, payload, 0)
	if err != nil {
		return err
	}
	return decodeJSON(resp, nil)
}

// DeleteJobVariable removes a key under vars/job/<current job> (DELETE /kv).
func (c *Client) DeleteJobVariable(key string) error {
	payload := storeKeyPayload{Namespace: "vars/job/" + JobName(), Key: key}
	resp, err := c.do(http.MethodDelete, routeStoreKeys, nil, payload, 0)
	if err != nil {
		return err
	}
	return decodeJSON(resp, nil)
}

// DeleteJobSecret removes an encrypted key under secrets/job/<current job>
// (DELETE /kv/secret).
func (c *Client) DeleteJobSecret(key string) error {
	payload := storeKeyPayload{Namespace: "secrets/job/" + JobName(), Key: key}
	resp, err := c.do(http.MethodDelete, routeStoreSecret, nil, payload, 0)
	if err != nil {
		return err
	}
	return decodeJSON(resp, nil)
}

// ListJobKeys lists keys under vars/job/<job> and secrets/job/<job>
// (GET /kv/keys). Pass "" to list both namespaces.
func (c *Client) ListJobKeys(namespace string) (map[string][]string, error) {
	payload := storeKeyPayload{Namespace: namespace}
	resp, err := c.do(http.MethodGet, routeStoreKeysList, nil, payload, 0)
	if err != nil {
		return nil, err
	}
	var out listKeysResponse
	if err := decodeJSON(resp, &out); err != nil {
		return nil, err
	}
	return out.Namespaces, nil
}

// PutRolloutOrder sets the rollout order for the current job
// (pre_deploy or cli events only). order is a list of worker IPs.
func (c *Client) PutRolloutOrder(order []string) error {
	payload := storeKeyPayload{
		Namespace: "kive/job/" + JobName(),
		Key:       "rollout_order",
		Value:     strings.Join(order, ","),
	}
	resp, err := c.do(http.MethodPut, routeStoreKeys, nil, payload, 0)
	if err != nil {
		return err
	}
	return decodeJSON(resp, nil)
}

// GetRolloutOrder returns the rollout order for the current job.
func (c *Client) GetRolloutOrder() (string, error) {
	return c.GetStoreValue("kive/job/"+JobName(), "rollout_order")
}

// ListHookDemands lists hooks that depend on the current hook (GET /demands).
func (c *Client) ListHookDemands() ([]HookDemand, error) {
	resp, err := c.do(http.MethodGet, routeDemands, nil, nil, 0)
	if err != nil {
		return nil, err
	}
	var out []HookDemand
	if err := decodeJSON(resp, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AcquireSemaphore blocks until this allocation holds a slot in the named
// semaphore (POST /semaphore/acquire). Use capacity=1 for a leader/mutex
// (e.g. deploy one allocation before the rest). Scoped per job and hook
// event. capacity defaults to 1 and timeoutSeconds defaults to 600 when 0.
func (c *Client) AcquireSemaphore(name string, capacity, timeoutSeconds int) (*SemaphoreAcquireResult, error) {
	if capacity <= 0 {
		capacity = 1
	}
	if timeoutSeconds <= 0 {
		timeoutSeconds = 600
	}
	payload := map[string]any{"name": name, "capacity": capacity, "timeout_seconds": timeoutSeconds}
	resp, err := c.do(http.MethodPost, routeSemaphoreAcquire, nil, payload, time.Duration(timeoutSeconds+30)*time.Second)
	if err != nil {
		return nil, err
	}
	var out SemaphoreAcquireResult
	if err := decodeJSON(resp, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReleaseSemaphore releases a slot held by this allocation
// (POST /semaphore/release).
func (c *Client) ReleaseSemaphore(name string) error {
	resp, err := c.do(http.MethodPost, routeSemaphoreRelease, nil, map[string]any{"name": name}, 0)
	if err != nil {
		return err
	}
	return decodeJSON(resp, nil)
}

// SemaphoreStatus inspects holders and waiters for a named semaphore
// (GET /semaphore/status).
func (c *Client) SemaphoreStatus(name string) (*SemaphoreStatus, error) {
	resp, err := c.do(http.MethodGet, routeSemaphoreStatus, url.Values{"name": {name}}, nil, 0)
	if err != nil {
		return nil, err
	}
	var out SemaphoreStatus
	if err := decodeJSON(resp, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// HTTPProxyTLS configures upstream TLS for HTTPRequest.
type HTTPProxyTLS struct {
	CA                 string `json:"ca,omitempty"`
	ServerName         string `json:"server_name,omitempty"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify,omitempty"`
}

// HTTPProxyResult is returned by HTTPRequest (POST /http).
type HTTPProxyResult struct {
	StatusCode int                 `json:"status_code"`
	Headers    map[string][]string `json:"headers,omitempty"`
	Body       string              `json:"body"`
	Truncated  bool                `json:"truncated"`
	Error      string              `json:"error,omitempty"`
}

// HTTPRequest performs an outbound HTTP(S) call via the runtime API proxy
// (POST /http). Destination host must be the calling allocation's worker IP.
func (c *Client) HTTPRequest(method, rawURL string, headers map[string]string, body string, timeoutSeconds int, tlsOpts *HTTPProxyTLS) (*HTTPProxyResult, error) {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 30
	}
	if headers == nil {
		headers = map[string]string{}
	}
	tlsPayload := HTTPProxyTLS{}
	if tlsOpts != nil {
		tlsPayload = *tlsOpts
	}
	payload := map[string]any{
		"method":          method,
		"url":             rawURL,
		"headers":         headers,
		"body":            body,
		"timeout_seconds": timeoutSeconds,
		"tls":             tlsPayload,
	}
	resp, err := c.do(http.MethodPost, routeHTTP, nil, payload, time.Duration(timeoutSeconds+30)*time.Second)
	if err != nil {
		return nil, err
	}
	var out HTTPProxyResult
	if err := decodeJSON(resp, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func firstOrZero(ttlSeconds []int) int {
	if len(ttlSeconds) == 0 {
		return 0
	}
	return ttlSeconds[0]
}
