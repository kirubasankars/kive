// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package hooks

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"kive/certs"
	"kive/kv"
)

const (
	httpProxyDefaultTimeoutSeconds = 30
	httpProxyMaxTimeoutSeconds     = 120
	httpProxyMaxRequestBodyBytes   = 1 << 20  // 1 MiB
	httpProxyMaxResponseBodyBytes  = 4 << 20  // 4 MiB
	httpProxyMaxRedirects          = 5
)

var httpProxyAllowedMethods = map[string]struct{}{
	http.MethodGet:    {},
	http.MethodHead:   {},
	http.MethodPost:   {},
	http.MethodPut:    {},
	http.MethodPatch:  {},
	http.MethodDelete: {},
}

var httpProxyHopByHopHeaders = map[string]struct{}{
	"connection":          {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"te":                  {},
	"trailer":             {},
	"transfer-encoding":   {},
	"upgrade":             {},
	"proxy-connection":    {},
}

func serveHTTPProxy(apiCtx *runtimeAPIContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if strings.TrimSpace(r.Header.Get(HeaderHookEvent)) == "" ||
			strings.TrimSpace(r.Header.Get(HeaderHookName)) == "" {
			runtimeAPIErrors.missingHookHeaders.write(w)
			return
		}

		_, workerIP, _, body, err := resolveAllocationFromRequestWithID(w, r, apiCtx.gate)
		if err != nil {
			return
		}
		defer func() {
			_ = body.Close()
		}()

		limited := http.MaxBytesReader(w, body, httpProxyMaxRequestBodyBytes+1)
		var payload httpProxyRequest
		if err := json.NewDecoder(limited).Decode(&payload); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				http.Error(w, "request body too large", http.StatusBadRequest)
				return
			}
			writeJSONDecodeError(w, err)
			return
		}

		if err := validateHTTPProxyRequest(&payload, workerIP); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		timeout := payload.TimeoutSeconds
		if timeout <= 0 {
			timeout = httpProxyDefaultTimeoutSeconds
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Duration(timeout)*time.Second)
		defer cancel()

		client, err := newHTTPProxyClient(apiCtx.gate, payload.TLS, workerIP)
		if err != nil {
			writeJSONResponse(w, http.StatusBadGateway, httpProxyResponse{Error: err.Error()})
			return
		}

		upstreamReq, err := http.NewRequestWithContext(ctx, strings.ToUpper(payload.Method), payload.URL, strings.NewReader(payload.Body))
		if err != nil {
			http.Error(w, "invalid upstream request", http.StatusBadRequest)
			return
		}
		for k, v := range payload.Headers {
			if isHTTPProxyHopByHopHeader(k) {
				continue
			}
			upstreamReq.Header.Set(k, v)
		}

		resp, err := client.Do(upstreamReq)
		if err != nil {
			writeJSONResponse(w, http.StatusBadGateway, httpProxyResponse{Error: err.Error()})
			return
		}
		defer func() {
			_ = resp.Body.Close()
		}()

		limitedBody := io.LimitReader(resp.Body, httpProxyMaxResponseBodyBytes+1)
		raw, err := io.ReadAll(limitedBody)
		if err != nil {
			writeJSONResponse(w, http.StatusBadGateway, httpProxyResponse{Error: err.Error()})
			return
		}
		truncated := false
		if len(raw) > httpProxyMaxResponseBodyBytes {
			raw = raw[:httpProxyMaxResponseBodyBytes]
			truncated = true
		}

		outHeaders := make(map[string][]string)
		for k, vals := range resp.Header {
			if isHTTPProxyHopByHopHeader(k) {
				continue
			}
			outHeaders[k] = append([]string(nil), vals...)
		}

		writeJSONResponse(w, http.StatusOK, httpProxyResponse{
			StatusCode: resp.StatusCode,
			Headers:    outHeaders,
			Body:       string(raw),
			Truncated:  truncated,
		})
	}
}

func validateHTTPProxyRequest(payload *httpProxyRequest, workerIP string) error {
	method := strings.ToUpper(strings.TrimSpace(payload.Method))
	if method == "" {
		return fmt.Errorf("method is required")
	}
	if _, ok := httpProxyAllowedMethods[method]; !ok {
		return fmt.Errorf("method %s is not allowed", method)
	}
	payload.Method = method

	if strings.TrimSpace(payload.URL) == "" {
		return fmt.Errorf("url is required")
	}
	if len(payload.Body) > httpProxyMaxRequestBodyBytes {
		return fmt.Errorf("request body too large")
	}
	if payload.TimeoutSeconds < 0 {
		return fmt.Errorf("timeout_seconds must be non-negative")
	}
	if payload.TimeoutSeconds > httpProxyMaxTimeoutSeconds {
		return fmt.Errorf("timeout_seconds must be <= %d", httpProxyMaxTimeoutSeconds)
	}

	u, err := url.Parse(payload.URL)
	if err != nil {
		return fmt.Errorf("invalid url")
	}
	if err := validateHTTPProxyDestination(u, workerIP); err != nil {
		return err
	}
	return nil
}

func validateHTTPProxyDestination(u *url.URL, workerIP string) error {
	if u == nil {
		return fmt.Errorf("invalid url")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("url scheme must be http or https")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("url host is required")
	}
	if !httpProxyHostAllowed(host, workerIP) {
		return fmt.Errorf("url host must be the allocation worker IP")
	}
	return nil
}

func httpProxyHostAllowed(host, workerIP string) bool {
	host = strings.TrimSpace(host)
	workerIP = strings.TrimSpace(workerIP)
	if host == "" || workerIP == "" {
		return false
	}
	// Literal IP match only — no DNS resolution (SSRF / open-proxy mitigation).
	if host != workerIP {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return true
}

func isHTTPProxyHopByHopHeader(name string) bool {
	_, ok := httpProxyHopByHopHeaders[strings.ToLower(name)]
	return ok
}

func newHTTPProxyClient(gate *txGate, tlsOpts httpProxyTLSOptions, workerIP string) (*http.Client, error) {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default HTTP transport is not an *http.Transport")
	}
	transport = transport.Clone()

	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12} //nolint:gosec // MinVersion set
	if tlsOpts.InsecureSkipVerify {
		tlsCfg.InsecureSkipVerify = true //nolint:gosec // explicit escape hatch
	} else {
		roots, err := x509.SystemCertPool()
		if err != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		caMode := strings.TrimSpace(tlsOpts.CA)
		if caMode == "" || caMode == "bucket" {
			caPEM, err := loadWorkerCATrustPEM(gate)
			if err != nil {
				return nil, err
			}
			if len(caPEM) > 0 {
				if ok := roots.AppendCertsFromPEM(caPEM); !ok {
					return nil, fmt.Errorf("bucket CA contains no valid certificates")
				}
			}
		} else {
			return nil, fmt.Errorf("tls.ca must be \"bucket\" or omitted")
		}
		tlsCfg.RootCAs = roots
		if name := strings.TrimSpace(tlsOpts.ServerName); name != "" {
			tlsCfg.ServerName = name
		}
	}
	transport.TLSClientConfig = tlsCfg

	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= httpProxyMaxRedirects {
				return fmt.Errorf("stopped after %d redirects", httpProxyMaxRedirects)
			}
			if err := validateHTTPProxyDestination(req.URL, workerIP); err != nil {
				return err
			}
			return nil
		},
	}
	return client, nil
}

func loadWorkerCATrustPEM(gate *txGate) ([]byte, error) {
	var pem []byte
	err := gate.Do(func(tx *sql.Tx) error {
		_ = tx
		store := kv.GetKVStore()
		if store == nil {
			return fmt.Errorf("kv store not initialized")
		}
		item, err := store.Get("kive/worker", certs.WorkerCATrustKVKey)
		if err != nil {
			if errors.Is(err, kv.ErrNotFound) {
				return nil
			}
			return err
		}
		pem = []byte(item.Value)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("load bucket CA: %w", err)
	}
	return pem, nil
}
