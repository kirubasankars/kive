// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package bucket

import (
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"

	"kive/conf"
)

const (
	keyCertsTTL                = "certs_ttl"
	keyCertsRenewalBuffer      = "certs_renewal_buffer"
	keyIptables                = "iptables"
	keyJobsProfile             = "jobs_profile"
	keyJobConfigSelectorLegacy = "job_config_selector" // deprecated alias for jobs_profile
	keyKVRetainDays            = "kv_retain_days"

	defaultCertsTTL           = 60
	defaultCertsRenewalBuffer = 10
)

// reservedBucketVarKeys are operational bucket.conf keys that are not synced to vars/bucket.
var reservedBucketVarKeys = map[string]struct{}{
	keyCertsTTL:                {},
	keyCertsRenewalBuffer:      {},
	keyIptables:                {},
	keyJobsProfile:             {},
	keyJobConfigSelectorLegacy: {},
	keyKVRetainDays:            {},
	portRangeKey:               {}, // resolved from kive.conf (bucket.conf fallback); injected at build
}

// BucketSettings holds resolved operational settings (certs/iptables/jobs_profile
// from kive.conf with bucket.conf fallback; kv_retain_days from bucket.conf).
type BucketSettings struct {
	CertsTTL           int
	CertsRenewalBuffer int
	Iptables           bool
	JobsProfile        string
	KVRetainDays       int
}

// BucketConfPath returns workspace/bucket.conf.
func BucketConfPath() string {
	return path.Join(WorkspaceLocation, "bucket.conf")
}

// BucketConfPathAt returns workspace/bucket.conf under root.
func BucketConfPathAt(root string) string {
	return path.Join(root, "workspace", "bucket.conf")
}

// LoadBucketSettings reads resolved operational settings.
// certs_ttl, certs_renewal_buffer, iptables, and jobs_profile prefer kive.conf;
// when unset there, leftover workspace/bucket.conf values are used, then defaults.
// kv_retain_days remains in workspace/bucket.conf only.
func LoadBucketSettings() (BucketSettings, error) {
	return LoadBucketSettingsAt("")
}

// LoadBucketSettingsAt reads settings for root without mutating Location /
// WorkspaceLocation. Empty root uses the current bucket paths.
func LoadBucketSettingsAt(root string) (BucketSettings, error) {
	bucketConfPath := BucketConfPath()
	kiveConfPath := KiveConfPath()
	if strings.TrimSpace(root) != "" {
		bucketConfPath = BucketConfPathAt(root)
		kiveConfPath = path.Join(root, "kive.conf")
	}
	raw, err := loadBucketConfRaw(bucketConfPath)
	if err != nil {
		return BucketSettings{}, err
	}
	s, err := parseBucketSettings(raw)
	if err != nil {
		return BucketSettings{}, err
	}

	kiveData, err := conf.ReadBytes(kiveConfPath)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		if _, ok := err.(*conf.Error); ok {
			return BucketSettings{}, fmt.Errorf("%w: %v", ErrInvalidKiveConf, err)
		}
		return BucketSettings{}, fmt.Errorf("%w: %w", ErrUnexpectedError, err)
	}
	kc, present, err := ParseKiveConf(kiveConfPath, kiveData)
	if err != nil {
		return BucketSettings{}, fmt.Errorf("%w: %v", ErrInvalidKiveConf, err)
	}
	if _, ok := present[keyCertsTTL]; ok {
		n := kc.CertsTTL
		if n == 0 {
			n = defaultCertsTTL
		}
		if n < 0 {
			return BucketSettings{}, fmt.Errorf("%w: %s must be >= 0", ErrInvalidKiveConf, keyCertsTTL)
		}
		s.CertsTTL = n
	}
	if _, ok := present[keyCertsRenewalBuffer]; ok {
		if kc.CertsRenewalBuffer < 0 {
			return BucketSettings{}, fmt.Errorf("%w: %s must be >= 0", ErrInvalidKiveConf, keyCertsRenewalBuffer)
		}
		s.CertsRenewalBuffer = kc.CertsRenewalBuffer
	}
	if _, ok := present[keyIptables]; ok {
		s.Iptables = kc.Iptables
	}
	if _, ok := present[keyJobsProfile]; ok {
		s.JobsProfile = kc.JobsProfile
	}
	return s, nil
}

// LoadBucketConfVars returns string settings synced to KV namespace vars/bucket.
// Operational keys (certs_*, iptables, jobs_profile, kv_retain_days, port_range) are excluded.
// Build injects the resolved port_range from LoadPortRange.
func LoadBucketConfVars() (map[string]string, error) {
	raw, err := loadBucketConfRaw(BucketConfPath())
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		if _, reserved := reservedBucketVarKeys[k]; reserved {
			continue
		}
		s, err := bucketConfStringValue(k, v)
		if err != nil {
			return nil, err
		}
		out[k] = s
	}
	return out, nil
}

func loadBucketConfRaw(confPath string) (map[string]any, error) {
	f, err := conf.ReadFile(confPath)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		if _, ok := err.(*conf.Error); ok {
			return nil, fmt.Errorf("%w: %v", ErrInvalidBucketConf, err)
		}
		return nil, fmt.Errorf("%w: %w", ErrUnexpectedError, err)
	}
	raw := map[string]any{}
	for _, stmt := range f.Stmts {
		call, ok := stmt.(*conf.Call)
		if !ok {
			return nil, conf.Err(confPath, stmt.Pos().Line, stmt.Pos().Column,
				"bucket.conf only allows setting calls, not blocks")
		}
		if call.Name == portRangeKey {
			s, err := parsePortRangeCall(call, confPath)
			if err != nil {
				return nil, err
			}
			raw[portRangeKey] = s
			continue
		}
		if len(call.Args) != 1 {
			return nil, conf.Err(confPath, call.NamePos.Line, call.NamePos.Column,
				fmt.Sprintf("%s expects one argument", call.Name))
		}
		switch v := call.Args[0].Value.(type) {
		case *conf.StringLit:
			raw[call.Name] = v.Value
		case *conf.NumberLit:
			if v.IsFloat {
				raw[call.Name] = v.Float
			} else {
				raw[call.Name] = v.Int
			}
		case *conf.BoolLit:
			raw[call.Name] = v.Value
		case *conf.IdentLit:
			raw[call.Name] = v.Name
		default:
			s, err := conf.AsString(call.Args[0].Value, confPath)
			if err != nil {
				return nil, err
			}
			raw[call.Name] = s
		}
	}
	return raw, nil
}

// parsePortRangeCall requires port_range(min, max) with two integer literals.
func parsePortRangeCall(call *conf.Call, confPath string) (string, error) {
	if len(call.Args) != 2 {
		return "", conf.Err(confPath, call.NamePos.Line, call.NamePos.Column,
			"port_range expects two integers, e.g. port_range(30000,39999)").
			WithHint("write port_range(30000,39999)")
	}
	min, err := bucketConfPortArg(call.Args[0].Value, confPath, "min")
	if err != nil {
		return "", err
	}
	max, err := bucketConfPortArg(call.Args[1].Value, confPath, "max")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d,%d", min, max), nil
}

func bucketConfPortArg(v conf.Value, confPath, which string) (int, error) {
	n, ok := v.(*conf.NumberLit)
	if !ok || n.IsFloat {
		return 0, conf.Err(confPath, v.Pos().Line, v.Pos().Column,
			fmt.Sprintf("port_range %s must be an integer", which)).
			WithHint("write port_range(30000,39999)")
	}
	if n.Int > int64(^uint(0)>>1) || n.Int < int64(^int(0)) {
		return 0, conf.Err(confPath, n.At.Line, n.At.Column, "integer out of range")
	}
	return int(n.Int), nil
}

// EmitBucketConfRaw emits a flat string map as bucket.conf.
func EmitBucketConfRaw(raw map[string]string) string {
	f := &conf.File{}
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	// stable-ish: port_range first then alpha — keep simple alpha
	sortStrings(keys)
	for _, k := range keys {
		if k == portRangeKey {
			if stmt, ok := emitPortRangeStmt(raw[k]); ok {
				f.Stmts = append(f.Stmts, stmt)
				continue
			}
		}
		f.Stmts = append(f.Stmts, conf.CallStmt(k, raw[k]))
	}
	return conf.Emit(f, conf.EmitOptions{})
}

func emitPortRangeStmt(raw string) (*conf.Call, bool) {
	r, err := ParsePortRangeValue(raw)
	if err != nil {
		return nil, false
	}
	return &conf.Call{
		Name:        portRangeKey,
		TrailingSem: true,
		Args: []conf.Arg{
			{Value: conf.Int(r.Min)},
			{Value: conf.Int(r.Max)},
		},
	}, true
}

func sortStrings(s []string) {
	for i := 0; i < len(s); i++ {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

func parseBucketSettings(raw map[string]any) (BucketSettings, error) {
	s := BucketSettings{
		CertsTTL:           defaultCertsTTL,
		CertsRenewalBuffer: defaultCertsRenewalBuffer,
		Iptables:           false,
		JobsProfile:        "",
		KVRetainDays:       DefaultKVRetainDays,
	}

	if v, ok := raw[keyCertsTTL]; ok && v != nil {
		n, err := bucketConfIntValue(keyCertsTTL, v)
		if err != nil {
			return BucketSettings{}, err
		}
		if n == 0 {
			n = defaultCertsTTL
		}
		s.CertsTTL = n
	}

	if v, ok := raw[keyCertsRenewalBuffer]; ok && v != nil {
		n, err := bucketConfIntValue(keyCertsRenewalBuffer, v)
		if err != nil {
			return BucketSettings{}, err
		}
		if n < 0 {
			return BucketSettings{}, fmt.Errorf("%w: %s must be >= 0", ErrInvalidBucketConf, keyCertsRenewalBuffer)
		}
		s.CertsRenewalBuffer = n
	}

	if v, ok := raw[keyIptables]; ok && v != nil {
		b, err := bucketConfBoolValue(keyIptables, v)
		if err != nil {
			return BucketSettings{}, err
		}
		s.Iptables = b
	}

	if v, ok := raw[keyJobsProfile]; ok && v != nil {
		str, err := bucketConfStringValue(keyJobsProfile, v)
		if err != nil {
			return BucketSettings{}, err
		}
		profile, err := SanitizeJobsProfile(str)
		if err != nil {
			return BucketSettings{}, err
		}
		s.JobsProfile = profile
	} else if v, ok := raw[keyJobConfigSelectorLegacy]; ok && v != nil {
		// Deprecated alias for jobs_profile.
		str, err := bucketConfStringValue(keyJobConfigSelectorLegacy, v)
		if err != nil {
			return BucketSettings{}, err
		}
		profile, err := SanitizeJobsProfile(str)
		if err != nil {
			return BucketSettings{}, err
		}
		s.JobsProfile = profile
	}

	if v, ok := raw[keyKVRetainDays]; ok && v != nil {
		n, err := bucketConfIntValue(keyKVRetainDays, v)
		if err != nil {
			return BucketSettings{}, err
		}
		if n < 0 {
			return BucketSettings{}, fmt.Errorf("%w: %s must be >= 0", ErrInvalidBucketConf, keyKVRetainDays)
		}
		s.KVRetainDays = n
	}

	return s, nil
}

func bucketConfStringValue(key string, v any) (string, error) {
	switch t := v.(type) {
	case string:
		return t, nil
	case int64:
		return strconv.FormatInt(t, 10), nil
	case int:
		return strconv.Itoa(t), nil
	case float64:
		return strconv.FormatInt(int64(t), 10), nil
	case bool:
		return strconv.FormatBool(t), nil
	default:
		return "", fmt.Errorf("%w: %s must be a string (got %T)", ErrInvalidBucketConf, key, v)
	}
}

func bucketConfIntValue(key string, v any) (int, error) {
	switch t := v.(type) {
	case int64:
		return int(t), nil
	case int:
		return t, nil
	case float64:
		return int(t), nil
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		if err != nil {
			return 0, fmt.Errorf("%w: %s must be an integer (got %q)", ErrInvalidBucketConf, key, t)
		}
		return n, nil
	default:
		return 0, fmt.Errorf("%w: %s must be an integer (got %T)", ErrInvalidBucketConf, key, v)
	}
}

func bucketConfBoolValue(key string, v any) (bool, error) {
	switch t := v.(type) {
	case bool:
		return t, nil
	case string:
		switch strings.TrimSpace(strings.ToLower(t)) {
		case "true", "1", "yes":
			return true, nil
		case "false", "0", "no", "":
			return false, nil
		default:
			return false, fmt.Errorf("%w: %s must be true or false (got %q)", ErrInvalidBucketConf, key, t)
		}
	default:
		return false, fmt.Errorf("%w: %s must be true or false (got %T)", ErrInvalidBucketConf, key, v)
	}
}
