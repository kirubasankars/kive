// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package hooks

// HTTP routes and headers for the command runtime API.
//
// Python and JS hooks run inside the kive container and call this API on the
// host process to read/write the in-memory KV store and query command demands.
const (
	// RuntimeAPIListenHost is the loopback host for StartRuntimeAPI.
	RuntimeAPIListenHost = "127.0.0.1"
	// RuntimeAPIDefaultPort is used by embedded helpers when HOOK_API_PORT is unset
	// (manual local debugging only; kive always injects the live port).
	RuntimeAPIDefaultPort = 8080

	RouteStoreKeys         = "/kv"
	RouteStoreKeysList     = "/kv/keys"
	RouteStoreSecret       = "/kv/secret"
	RouteDemands           = "/demands"
	RouteSemaphoreAcquire  = "/semaphore/acquire"
	RouteSemaphoreRelease  = "/semaphore/release"
	RouteSemaphoreStatus   = "/semaphore/status"
	RouteHTTP              = "/http"

	// EnvHookAPIHost is set on the hook exec env so kive.py / kive.ts can reach the API.
	EnvHookAPIHost = "HOOK_API_HOST"
	// EnvHookAPIPort is the ephemeral listen port assigned for this runtime API session.
	EnvHookAPIPort = "HOOK_API_PORT"
	// EnvHookAPIToken is the per-session secret required by the runtime API.
	EnvHookAPIToken = "HOOK_API_TOKEN"

	HeaderAllocationID = "X-ALLOCATION-ID"
	HeaderHookAPIToken = "X-HOOK-API-TOKEN"
	HeaderHookEvent    = "EVENT"
	HeaderHookName     = "HOOK"
)

// listKeysResponse is returned by GET /kv/keys.
type listKeysResponse struct {
	Namespaces map[string][]string `json:"namespaces"`
}

// storeKeyPayload is the JSON body for GET/PUT/DELETE /kv.
type storeKeyPayload struct {
	Namespace string `json:"namespace"`
	Key       string `json:"key"`
	Value     string `json:"value,omitempty"`
	// TTL is optional key lifetime in seconds; 0 or omitted means no expiry.
	TTL int `json:"ttl,omitempty"`
}

// hookDemandPayload is one dependent hook returned by GET /demands.
type hookDemandPayload struct {
	Job          string         `json:"job"`
	Hook         string         `json:"hook"`
	DemandConfig map[string]any `json:"demand_config"`
}

// semaphoreAcquirePayload is the JSON body for POST /semaphore/acquire.
type semaphoreAcquirePayload struct {
	Name           string `json:"name"`
	Capacity       int    `json:"capacity,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

// semaphoreReleasePayload is the JSON body for POST /semaphore/release.
type semaphoreReleasePayload struct {
	Name string `json:"name"`
}

// semaphoreStatusPayload describes current holders for a job/event semaphore.
type semaphoreStatusPayload struct {
	Name      string   `json:"name"`
	Capacity  int      `json:"capacity"`
	Holders   []string `json:"holders"`
	Waiting   int      `json:"waiting"`
	Available int      `json:"available"`
}

// semaphoreAcquireResponse is returned when an allocation acquires a slot.
type semaphoreAcquireResponse struct {
	Name         string `json:"name"`
	AllocationID string `json:"allocation_id"`
	Capacity     int    `json:"capacity"`
	Acquired     bool   `json:"acquired"`
}

// httpProxyTLSOptions configures upstream TLS for POST /http.
type httpProxyTLSOptions struct {
	// CA is "bucket" (default) to trust system roots plus kive/worker certs/ca-trust.crt.
	CA string `json:"ca,omitempty"`
	// ServerName overrides the TLS hostname check (job leaf CN when URL host is an IP).
	ServerName string `json:"server_name,omitempty"`
	// InsecureSkipVerify disables certificate verification (escape hatch).
	InsecureSkipVerify bool `json:"insecure_skip_verify,omitempty"`
}

// httpProxyRequest is the JSON body for POST /http.
type httpProxyRequest struct {
	Method         string              `json:"method"`
	URL            string              `json:"url"`
	Headers        map[string]string   `json:"headers,omitempty"`
	Body           string              `json:"body,omitempty"`
	TimeoutSeconds int                 `json:"timeout_seconds,omitempty"`
	TLS            httpProxyTLSOptions `json:"tls"`
}

// httpProxyResponse is returned by POST /http for completed upstream exchanges.
type httpProxyResponse struct {
	StatusCode int                 `json:"status_code"`
	Headers    map[string][]string `json:"headers,omitempty"`
	Body       string              `json:"body"`
	Truncated  bool                `json:"truncated"`
	Error      string              `json:"error,omitempty"`
}
