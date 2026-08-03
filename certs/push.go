// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package certs

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"kive/bucket"
	"kive/data"
	"kive/kv"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/prometheus/prompb"
)

const (
	prometheusAdminUserKey = "admin_username"
	prometheusAdminPassKey = "admin_password"
	metricCertNotAfter     = "kive_cert_not_after_seconds"
	metricCertExpiring     = "kive_cert_expiring"
	metricCertExpired      = "kive_cert_expired"
	certMetricsPushTimeout = 15 * time.Second
)

var (
	certMetricsPushAttempts    = 5
	certMetricsRetryBackoff    = 2 * time.Second
	certMetricsRetryMaxBackoff = 15 * time.Second
)

type remoteWriteError struct {
	statusCode int
	body       string
}

func (e *remoteWriteError) Error() string {
	status := fmt.Sprintf("%d %s", e.statusCode, http.StatusText(e.statusCode))
	if e.body != "" {
		return fmt.Sprintf("remote write status %s: %s", status, e.body)
	}
	return fmt.Sprintf("remote write status %s", status)
}

// PushMetrics sends current certificate expiry gauges to Prometheus remote write.
// Called after deploy; failures are logged and do not fail deploy.
func PushMetrics(db *sql.DB) {
	PushMetricsContext(context.Background(), db)
}

// PushMetricsContext sends certificate metrics and stops retries on cancellation.
func PushMetricsContext(ctx context.Context, db *sql.DB) {
	if err := pushMetricsContext(ctx, db); err != nil {
		if ctx.Err() != nil {
			return
		}
		log.Printf("cert metrics: %v", err)
	}
}

func pushMetrics(db *sql.DB) error {
	return pushMetricsContext(context.Background(), db)
}

func pushMetricsContext(ctx context.Context, db *sql.DB) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return bucket.DatabaseError(err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	writeURL, err := discoverPrometheusRemoteWriteURL(tx)
	if err != nil {
		return err
	}
	if writeURL == "" {
		return nil
	}

	metrics, err := ListCertMetrics(tx, nil, nil)
	if err != nil {
		return err
	}
	if len(metrics) == 0 {
		return nil
	}

	username, password, err := prometheusRemoteWriteAuth(db)
	if err != nil {
		return err
	}

	client, err := newRemoteWriteHTTPClient()
	if err != nil {
		return err
	}

	return remoteWriteMetricsWithRetryContext(ctx, client, writeURL, metrics, username, password)
}

func newRemoteWriteHTTPClient() (*http.Client, error) {
	caPEM, err := ReadDedupedCATrustBundle()
	if err != nil {
		return nil, fmt.Errorf("read ca-trust.crt for prometheus remote write: %w", err)
	}

	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}
	if ok := roots.AppendCertsFromPEM(caPEM); !ok {
		return nil, errors.New("ca-trust.crt for prometheus remote write contains no valid certificates")
	}

	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("default HTTP transport is not an *http.Transport")
	}
	transport = transport.Clone()
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{RootCAs: roots}
	} else {
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
		transport.TLSClientConfig.RootCAs = roots
	}

	return &http.Client{Transport: transport}, nil
}

func prometheusRemoteWriteAuth(db *sql.DB) (username, password string, err error) {
	tx, err := db.Begin()
	if err != nil {
		return "", "", bucket.DatabaseError(err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	jobName, err := findPrometheusServerJob(tx)
	if err != nil {
		return "", "", err
	}
	if jobName == "" {
		return "", "", nil
	}

	store, err := kv.LoadFromTransaction(tx)
	if err != nil {
		return "", "", err
	}

	ns := kv.SecretJobNamespace(jobName)
	username, err = store.GetSecret(ns, prometheusAdminUserKey)
	if errors.Is(err, kv.ErrNotFound) {
		username = ""
	} else if err != nil {
		return "", "", err
	}

	password, err = store.GetSecret(ns, prometheusAdminPassKey)
	if errors.Is(err, kv.ErrNotFound) {
		password = ""
	} else if err != nil {
		return "", "", err
	}

	if username == "" && password == "" {
		return "", "", nil
	}
	if username == "" || password == "" {
		return "", "", fmt.Errorf(
			"cert metrics: prometheus remote write requires both %s and %s in %s",
			prometheusAdminUserKey, prometheusAdminPassKey, ns,
		)
	}
	return username, password, nil
}

func remoteWriteMetricsWithRetry(writeURL string, metrics []Metric, username, password string) error {
	return remoteWriteMetricsWithRetryContext(context.Background(), http.DefaultClient, writeURL, metrics, username, password)
}

func remoteWriteMetricsWithRetryContext(ctx context.Context, client *http.Client, writeURL string, metrics []Metric, username, password string) error {
	backoff := certMetricsRetryBackoff
	var lastErr error
	for attempt := 1; attempt <= certMetricsPushAttempts; attempt++ {
		lastErr = remoteWriteMetricsContext(ctx, client, writeURL, metrics, username, password)
		if lastErr == nil {
			if attempt > 1 {
				log.Printf("cert metrics: push succeeded on attempt %d/%d", attempt, certMetricsPushAttempts)
			}
			return nil
		}
		if attempt == certMetricsPushAttempts || !isRetryableRemoteWriteError(lastErr) {
			return lastErr
		}
		log.Printf("cert metrics: attempt %d/%d failed: %v; retrying in %s", attempt, certMetricsPushAttempts, lastErr, backoff)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return ctx.Err()
		}
		if next := backoff * 2; next > certMetricsRetryMaxBackoff {
			backoff = certMetricsRetryMaxBackoff
		} else {
			backoff = next
		}
	}
	return lastErr
}

func isRetryableRemoteWriteError(err error) bool {
	var rw *remoteWriteError
	if errors.As(err, &rw) {
		return rw.statusCode == http.StatusTooManyRequests || rw.statusCode >= http.StatusInternalServerError
	}
	return true
}

func discoverPrometheusRemoteWriteURL(tx *sql.Tx) (string, error) {
	jobName, err := findPrometheusServerJob(tx)
	if err != nil {
		return "", err
	}
	if jobName == "" {
		return "", nil
	}

	workers, err := data.GetNonRemovedAllocations(tx, jobName)
	if err != nil {
		return "", err
	}
	if len(workers) == 0 {
		return "", nil
	}

	port, err := data.GetPortNumber(tx, jobName, jobName+"_port_http")
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("https://%s:%d/api/v1/write", workers[0], port), nil
}

func findPrometheusServerJob(tx *sql.Tx) (string, error) {
	jobs, err := data.GetJobs(tx)
	if err != nil {
		return "", err
	}
	for _, jobName := range jobs {
		hasConfig, err := data.JobHasPrometheusServerConfig(tx, jobName)
		if err != nil {
			return "", err
		}
		if hasConfig {
			return jobName, nil
		}
	}
	return "", nil
}

func remoteWriteMetrics(writeURL string, metrics []Metric, username, password string) error {
	return remoteWriteMetricsContext(context.Background(), http.DefaultClient, writeURL, metrics, username, password)
}

func remoteWriteMetricsContext(ctx context.Context, client *http.Client, writeURL string, metrics []Metric, username, password string) error {
	now := time.Now()
	series := make([]prompb.TimeSeries, 0, len(metrics)*3)
	for _, metric := range metrics {
		labels := metricLabels(metric)
		if metric.Status != StatusInvalid && !metric.NotAfter.IsZero() {
			series = append(series, newGaugeSeries(labels, metricCertNotAfter, float64(metric.NotAfter.Unix()), now))
		}
		expiring := float64(0)
		if metric.Status == StatusExpiring {
			expiring = 1
		}
		series = append(series, newGaugeSeries(labels, metricCertExpiring, expiring, now))

		expired := float64(0)
		if metric.Status == StatusExpired {
			expired = 1
		}
		series = append(series, newGaugeSeries(labels, metricCertExpired, expired, now))
	}

	req := &prompb.WriteRequest{Timeseries: series}
	payload, err := proto.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal remote write: %w", err)
	}
	encoded := snappy.Encode(nil, payload)

	ctx, cancel := context.WithTimeout(ctx, certMetricsPushTimeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, writeURL, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/x-protobuf")
	httpReq.Header.Set("Content-Encoding", "snappy")
	httpReq.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")
	if username != "" {
		httpReq.SetBasicAuth(username, password)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("remote write request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &remoteWriteError{
			statusCode: resp.StatusCode,
			body:       strings.TrimSpace(string(body)),
		}
	}
	return nil
}

func metricLabels(metric Metric) []prompb.Label {
	labels := []prompb.Label{
		{Name: "__name__", Value: metricCertNotAfter},
		{Name: "scope", Value: metric.Scope},
		{Name: "job", Value: metric.Job},
		{Name: "worker", Value: metric.WorkerIP},
		{Name: "cert", Value: metric.CertName},
		{Name: "common_name", Value: metric.CommonName},
		{Name: "status", Value: metric.Status},
	}
	sort.Slice(labels, func(i, j int) bool {
		return labels[i].Name < labels[j].Name
	})
	return labels
}

func newGaugeSeries(baseLabels []prompb.Label, metricName string, value float64, ts time.Time) prompb.TimeSeries {
	labels := make([]prompb.Label, len(baseLabels))
	copy(labels, baseLabels)
	for i := range labels {
		if labels[i].Name == "__name__" {
			labels[i].Value = metricName
			break
		}
	}
	sort.Slice(labels, func(i, j int) bool {
		return labels[i].Name < labels[j].Name
	})
	return prompb.TimeSeries{
		Labels: labels,
		Samples: []prompb.Sample{{
			Value:     value,
			Timestamp: ts.UnixMilli(),
		}},
	}
}
