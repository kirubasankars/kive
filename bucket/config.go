// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package bucket

import (
	"fmt"
	"os"
	"path"
	"strings"
	"sync"
)

const (
	DefaultLogRunRetentionCount      = 100
	DefaultLogRunRetentionDays       = 30
	DefaultKVRetainDays              = 7
	DefaultBackupRetentionCount      = 7
	DefaultHealthWaitSeconds         = 180
	DefaultHealthPollIntervalSeconds = 60
	DefaultMaxConcurrentSyncs        = 16
	DefaultTimezone                  = "UTC"
)

type KiveConf struct {
	UseSUDO                   bool     `toml:"use_sudo" json:"use_sudo"`
	SSHUser                   string   `toml:"ssh_user" json:"ssh_user"`
	SSHKeyFile                string   `toml:"ssh_key" json:"ssh_key"`
	SSHPort                   int      `toml:"ssh_port,omitempty" json:"ssh_port,omitempty"`
	StrictHostKeyChecking     string   `toml:"strict_host_key_checking,omitempty" json:"strict_host_key_checking,omitempty"`
	LogFormat                 string   `toml:"log_format,omitempty" json:"log_format,omitempty"`
	LogRunRetentionCount      int      `toml:"log_run_retention_count,omitempty" json:"log_run_retention_count,omitempty"`
	LogRunRetentionDays       int      `toml:"log_run_retention_days,omitempty" json:"log_run_retention_days,omitempty"`
	BackupRetentionCount      int      `toml:"backup_retention_count" json:"backup_retention_count"`
	HealthWaitSeconds         int      `toml:"health_wait_seconds,omitempty" json:"health_wait_seconds,omitempty"`
	HealthPollIntervalSeconds int      `toml:"health_poll_interval_seconds,omitempty" json:"health_poll_interval_seconds,omitempty"`
	MaxConcurrentSyncs        int      `toml:"max_concurrent_syncs,omitempty" json:"max_concurrent_syncs,omitempty"`
	PythonPath                string   `toml:"python_path,omitempty" json:"python_path,omitempty"`
	JSPath                    string   `toml:"js_path,omitempty" json:"js_path,omitempty"`
	RubyPath                  string   `toml:"ruby_path,omitempty" json:"ruby_path,omitempty"`
	BashPath                  string   `toml:"bash_path,omitempty" json:"bash_path,omitempty"`
	JobSignerCA               string   `toml:"job_signer_ca,omitempty" json:"job_signer_ca,omitempty"`
	JobSignerCATrust          []string `toml:"job_signer_ca_trust,omitempty" json:"job_signer_ca_trust,omitempty"`
	// PortRange is "min,max" for the inclusive kive-assigned port pool (port_range in kive.conf).
	PortRange          string `toml:"port_range,omitempty" json:"port_range,omitempty"`
	CertsTTL           int    `toml:"certs_ttl,omitempty" json:"certs_ttl,omitempty"`
	CertsRenewalBuffer int    `toml:"certs_renewal_buffer,omitempty" json:"certs_renewal_buffer,omitempty"`
	Iptables           bool   `toml:"iptables" json:"iptables"`
	JobsProfile        string `toml:"jobs_profile,omitempty" json:"jobs_profile,omitempty"`
	// Timezone is the bucket default IANA timezone for schedules (promotion + hooks).
	Timezone string `toml:"timezone,omitempty" json:"timezone,omitempty"`
	// Features toggles optional product surfaces (UI + API).
	// Promotion defaults to false; enable with features { promotion(true); }.
	// Observe defaults to true; disable with features { observe(false); }.
	Features KiveFeatures `toml:"features,omitempty" json:"features"`
}

// KiveFeatures holds optional product feature flags from kive.conf features { … }.
type KiveFeatures struct {
	// Promotion enables the Promotion UI, APIs, and auto-promote scheduler.
	// Default false when the features block is omitted or promotion is unset.
	Promotion bool `toml:"promotion,omitempty" json:"promotion"`
	// Observe enables the Observe + Alerts UI and observe APIs.
	// nil means default on; explicit false disables. JSON omitempty keeps nil absent.
	Observe *bool `toml:"observe,omitempty" json:"observe,omitempty"`
}

// PromotionFeature reports whether features.promotion is enabled.
func (c KiveConf) PromotionFeature() bool {
	return c.Features.Promotion
}

// ObserveFeature reports whether features.observe is enabled (default true).
func (c KiveConf) ObserveFeature() bool {
	if c.Features.Observe == nil {
		return true
	}
	return *c.Features.Observe
}

// Any reports whether a features block should be emitted (non-default flags).
func (f KiveFeatures) Any() bool {
	return f.Promotion || (f.Observe != nil && !*f.Observe)
}

// BoolPtr returns a pointer to v (for features.observe and tests).
func BoolPtr(v bool) *bool {
	return &v
}

// SSHStrictHostKeyChecking returns the OpenSSH StrictHostKeyChecking value (default yes).
func (c KiveConf) SSHStrictHostKeyChecking() (string, error) {
	s := strings.TrimSpace(strings.ToLower(c.StrictHostKeyChecking))
	switch s {
	case "", "yes":
		return "yes", nil
	case "accept-new", "no":
		return s, nil
	default:
		return "", fmt.Errorf("%w: strict_host_key_checking must be yes, accept-new, or no (got %q)", ErrInvalidKiveConf, c.StrictHostKeyChecking)
	}
}

var warnDisabledHostKeyChecking sync.Once

// WarnIfHostKeyCheckingDisabled prints a loud stderr warning once per process when
// strict_host_key_checking is "no" (disables MITM protection).
func WarnIfHostKeyCheckingDisabled(value string) {
	s := strings.TrimSpace(strings.ToLower(value))
	if s != "no" {
		return
	}
	warnDisabledHostKeyChecking.Do(func() {
		fmt.Fprintln(os.Stderr, "WARNING: kive.conf strict_host_key_checking=no disables SSH host key verification (MITM risk). Prefer yes or accept-new.")
	})
}

// SSHPort returns the SSH port from kive.conf (default 22).
func SSHPort() (int, error) {
	conf, err := GetKiveConf()
	if err != nil {
		return 22, err
	}
	if conf.SSHPort <= 0 {
		return 22, nil
	}
	return conf.SSHPort, nil
}

// KiveConfPath returns the path to kive.conf at the bucket root.
func KiveConfPath() string {
	return path.Join(Location, "kive.conf")
}

// PromotionConfPath returns the path to promotion.conf at the bucket root.
func PromotionConfPath() string {
	return path.Join(Location, "promotion.conf")
}

// PromotionJSONPath is deprecated; use PromotionConfPath.
func PromotionJSONPath() string {
	return PromotionConfPath()
}

// WebhookConfPath returns the path to webhook.conf at the bucket root.
func WebhookConfPath() string {
	return path.Join(Location, "webhook.conf")
}

// WebhookJSONPath is deprecated; use WebhookConfPath.
func WebhookJSONPath() string {
	return WebhookConfPath()
}

// ObserveConfPath returns the path to observe.conf at the bucket root.
func ObserveConfPath() string {
	return path.Join(Location, "observe.conf")
}

// ClickHouseConfPath is deprecated; use ObserveConfPath.
func ClickHouseConfPath() string {
	return ObserveConfPath()
}

// PushConfPath returns the path to push.conf at the bucket root.
func PushConfPath() string {
	return path.Join(Location, "push.conf")
}

// KiveConfExists reports whether kive.conf is present at the bucket root.
func KiveConfExists() bool {
	_, err := os.Stat(KiveConfPath())
	return err == nil
}

func GetKiveConf() (KiveConf, error) {
	return loadKiveConfFromDisk(KiveConfPath())
}

// GetKiveConfAt loads kive.conf under root without mutating Location.
func GetKiveConfAt(root string) (KiveConf, error) {
	return loadKiveConfFromDisk(path.Join(root, "kive.conf"))
}

func WriteKiveConf(conf *KiveConf) error {
	data := []byte(EmitKiveConf(*conf))
	confPath := KiveConfPath()
	if _, err := os.Stat(confPath); os.IsNotExist(err) {
		err = os.WriteFile(confPath, data, 0o600)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrUnexpectedError, err)
		}
	}
	return nil
}

// ReplaceKiveConf overwrites kive.conf (used by kive edit).
func ReplaceKiveConf(conf *KiveConf) error {
	if err := os.WriteFile(KiveConfPath(), []byte(EmitKiveConf(*conf)), 0o600); err != nil {
		return fmt.Errorf("%w: %w", ErrUnexpectedError, err)
	}
	return nil
}

// BackupRetentionCountFromConfig returns how many generation-named kive.db backups
// to keep on kive-labeled workers. When kive.conf is missing, DefaultBackupRetentionCount (7) applies.
// Explicit 0 means keep forever (no count-based prune).
func BackupRetentionCountFromConfig() (int, error) {
	conf, err := GetKiveConf()
	if err != nil {
		return DefaultBackupRetentionCount, err
	}
	if !KiveConfExists() {
		return DefaultBackupRetentionCount, nil
	}
	return conf.BackupRetentionCount, nil
}

// HealthWaitSecondsFromConfig returns the default wait budget for worker SSH and job
// health checks when waiting (one attempt per second unless a job manifest overrides).
// When kive.conf is missing, DefaultHealthWaitSeconds (180) applies.
// Explicit 0 means a single attempt (no extra retries).
func HealthWaitSecondsFromConfig() (int, error) {
	conf, err := GetKiveConf()
	if err != nil {
		return DefaultHealthWaitSeconds, err
	}
	if !KiveConfExists() {
		return DefaultHealthWaitSeconds, nil
	}
	return conf.HealthWaitSeconds, nil
}

// MinHealthPollIntervalSeconds is the server-enforced floor for scheduled polls.
const MinHealthPollIntervalSeconds = 60

// HealthPollIntervalSecondsFromConfig returns health.poll_interval_seconds from kive.conf.
// When the key is omitted the default (60) applies; an explicit 0 disables scheduled polling.
func HealthPollIntervalSecondsFromConfig() (int, error) {
	conf, err := GetKiveConf()
	if err != nil {
		return DefaultHealthPollIntervalSeconds, err
	}
	if !KiveConfExists() {
		return DefaultHealthPollIntervalSeconds, nil
	}
	return conf.HealthPollIntervalSeconds, nil
}

// ResolveTimezone returns a trimmed IANA timezone, falling back to DefaultTimezone.
func ResolveTimezone(tz string) string {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		return DefaultTimezone
	}
	return tz
}

// TimezoneFromConfig returns the bucket default timezone from kive.conf (UTC when unset/missing).
func TimezoneFromConfig() (string, error) {
	conf, err := GetKiveConf()
	if err != nil {
		return DefaultTimezone, err
	}
	if !KiveConfExists() {
		return DefaultTimezone, nil
	}
	return ResolveTimezone(conf.Timezone), nil
}

// TimezoneAt returns the bucket default timezone under root without mutating Location.
func TimezoneAt(root string) (string, error) {
	conf, err := GetKiveConfAt(root)
	if err != nil {
		return DefaultTimezone, err
	}
	return ResolveTimezone(conf.Timezone), nil
}

// HealthPollIntervalSecondsAt returns health.poll_interval_seconds for root.
// When the key is omitted the default (60) applies; an explicit 0 disables scheduled polling.
func HealthPollIntervalSecondsAt(root string) (int, error) {
	conf, err := GetKiveConfAt(root)
	if err != nil {
		return DefaultHealthPollIntervalSeconds, err
	}
	return conf.HealthPollIntervalSeconds, nil
}

// LogRunRetention returns resolved run-log retention limits and whether cleanup is disabled.
// When kive.conf is missing, defaults apply (100 runs, 30 days).
func LogRunRetentionFromConfig() (count, days int, disabled bool, err error) {
	conf, err := GetKiveConf()
	if err != nil {
		return 0, 0, true, err
	}
	if !KiveConfExists() {
		return DefaultLogRunRetentionCount, DefaultLogRunRetentionDays, false, nil
	}
	count = conf.LogRunRetentionCount
	days = conf.LogRunRetentionDays
	if count == 0 && days == 0 {
		return 0, 0, true, nil
	}
	return count, days, false, nil
}

// KVRetainDaysFromConfig returns how long deleted/stale KV rows are kept before purge.
// When workspace/bucket.conf is missing or omits kv_retain_days, DefaultKVRetainDays (7) applies.
// Explicit 0 means purge eligible rows immediately.
func KVRetainDaysFromConfig() (int, error) {
	settings, err := LoadBucketSettings()
	if err != nil {
		return DefaultKVRetainDays, err
	}
	return settings.KVRetainDays, nil
}

// KVRetainDaysFromConfigAt is KVRetainDaysFromConfig for a bucket root without
// mutating Location / WorkspaceLocation.
func KVRetainDaysFromConfigAt(root string) (int, error) {
	settings, err := LoadBucketSettingsAt(root)
	if err != nil {
		return DefaultKVRetainDays, err
	}
	return settings.KVRetainDays, nil
}
