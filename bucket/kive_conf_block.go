// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package bucket

import (
	"fmt"
	"os"
	"strings"
	"time"

	"kive/conf"
)

var kiveConfKnownBlocks = []string{
	"ssh", "log_config", "backup", "health", "deploy", "interpreters", "job_signer", "features",
}

var kiveConfTopLevelKnown = []string{
	"port_range",
	"certs_ttl", "certs_renewal_buffer", "iptables", "jobs_profile", "timezone",
}

var kiveConfLegacyFlatHints = map[string]string{
	"use_sudo":                 "use ssh { use_sudo(...); }",
	"ssh_user":                 "use ssh { user(...); }",
	"ssh_key":                  "use ssh { key(...); }",
	"ssh_port":                 "use ssh { port(...); }",
	"strict_host_key_checking": "use ssh { strict_host_key_checking(...); }",
	"log_format":               "use log_config { format(...); }",
	"log_run_retention_count":  "use log_config { run_retention_count(...); }",
	"log_run_retention_days":   "use log_config { run_retention_days(...); }",
	"backup_retention_count":   "use backup { retention_count(...); }",
	"health_wait_seconds":      "use health { wait_seconds(...); }",
	"max_concurrent_syncs":     "use deploy { max_concurrent_syncs(...); }",
	"python_path":              "use interpreters { python_path(...); }",
	"js_path":                  "use interpreters { js_path(...); }",
	"ruby_path":                "use interpreters { ruby_path(...); }",
	"bash_path":                "use interpreters { bash_path(...); }",
	"job_signer_ca":            "use job_signer { ca(...); }",
	"job_signer_ca_trust":      "use job_signer { ca_trust(...); }",
}

var kiveConfSSHKnown = []string{
	"user", "key", "port", "use_sudo", "strict_host_key_checking",
}

var kiveConfLogConfigKnown = []string{
	"format", "run_retention_count", "run_retention_days",
}

var kiveConfBackupKnown = []string{"retention_count"}

var kiveConfHealthKnown = []string{"wait_seconds", "poll_interval_seconds"}

var kiveConfDeployKnown = []string{"max_concurrent_syncs"}

var kiveConfInterpretersKnown = []string{
	"python_path", "js_path", "ruby_path", "bash_path",
}

var kiveConfJobSignerKnown = []string{"ca", "ca_trust"}

var kiveConfFeaturesKnown = []string{"promotion", "observe"}

// ParseKiveConf lowers kive.conf block dialect into KiveConf.
// Top-level calls are limited to first-class ungrouped settings (port_range, certs_*, …).
func ParseKiveConf(filePath string, data []byte) (KiveConf, map[string]struct{}, error) {
	f, err := conf.Parse(filePath, data)
	if err != nil {
		return KiveConf{}, nil, err
	}
	present := map[string]struct{}{}
	var c KiveConf
	seenBlocks := map[string]struct{}{}
	for _, stmt := range f.Stmts {
		switch s := stmt.(type) {
		case *conf.Block:
			if s.ID != "" {
				return KiveConf{}, nil, conf.Err(filePath, s.NamePos.Line, s.NamePos.Column,
					fmt.Sprintf("kive.conf block %q must not have an id", s.Name))
			}
			if _, ok := seenBlocks[s.Name]; ok {
				return KiveConf{}, nil, conf.Err(filePath, s.NamePos.Line, s.NamePos.Column,
					fmt.Sprintf("duplicate %q block", s.Name))
			}
			seenBlocks[s.Name] = struct{}{}
			var err error
			switch s.Name {
			case "ssh":
				err = lowerSSHBlock(filePath, s, &c, present)
			case "log_config":
				err = lowerLogConfigBlock(filePath, s, &c, present)
			case "backup":
				err = lowerBackupBlock(filePath, s, &c, present)
			case "health":
				err = lowerHealthBlock(filePath, s, &c, present)
			case "deploy":
				err = lowerDeployBlock(filePath, s, &c, present)
			case "interpreters":
				err = lowerInterpretersBlock(filePath, s, &c, present)
			case "job_signer":
				err = lowerJobSignerBlock(filePath, s, &c, present)
			case "features":
				err = lowerFeaturesBlock(filePath, s, &c, present)
			default:
				return KiveConf{}, nil, conf.UnknownSetting(filePath, s.NamePos, s.Name, kiveConfKnownBlocks)
			}
			if err != nil {
				return KiveConf{}, nil, err
			}
		case *conf.Call:
			if err := applyTopLevelKiveCall(filePath, s, &c, present); err != nil {
				return KiveConf{}, nil, err
			}
		default:
			return KiveConf{}, nil, conf.Err(filePath, stmt.Pos().Line, stmt.Pos().Column,
				"kive.conf only allows setting calls and known blocks")
		}
	}
	return c, present, nil
}

func markPresent(present map[string]struct{}, filePath string, call *conf.Call, key string) error {
	if _, ok := present[key]; ok {
		return conf.Err(filePath, call.NamePos.Line, call.NamePos.Column,
			fmt.Sprintf("%q set more than once", key))
	}
	present[key] = struct{}{}
	return nil
}

func applyTopLevelKiveCall(filePath string, call *conf.Call, c *KiveConf, present map[string]struct{}) error {
	if hint, legacy := kiveConfLegacyFlatHints[call.Name]; legacy {
		return conf.Err(filePath, call.NamePos.Line, call.NamePos.Column,
			fmt.Sprintf("legacy flat setting %q is not allowed", call.Name)).
			WithHint(hint)
	}
	if err := markPresent(present, filePath, call, call.Name); err != nil {
		return err
	}
	switch call.Name {
	case "port_range":
		s, err := parsePortRangeCall(call, filePath)
		if err != nil {
			return err
		}
		c.PortRange = s
	case "certs_ttl":
		n, err := conf.SingleIntArg(call, filePath)
		if err != nil {
			return err
		}
		c.CertsTTL = n
	case "certs_renewal_buffer":
		n, err := conf.SingleIntArg(call, filePath)
		if err != nil {
			return err
		}
		c.CertsRenewalBuffer = n
	case "iptables":
		v, err := conf.SingleBoolArg(call, filePath)
		if err != nil {
			return err
		}
		c.Iptables = v
	case "jobs_profile":
		s, err := conf.SingleStringArg(call, filePath)
		if err != nil {
			return err
		}
		profile, err := SanitizeJobsProfile(s)
		if err != nil {
			return err
		}
		c.JobsProfile = profile
	case "timezone":
		s, err := conf.SingleStringArg(call, filePath)
		if err != nil {
			return err
		}
		c.Timezone = strings.TrimSpace(s)
	default:
		return conf.UnknownSetting(filePath, call.NamePos, call.Name, kiveConfTopLevelKnown)
	}
	return nil
}

func lowerSSHBlock(filePath string, b *conf.Block, c *KiveConf, present map[string]struct{}) error {
	for _, stmt := range b.Body {
		call, ok := stmt.(*conf.Call)
		if !ok {
			return conf.Err(filePath, stmt.Pos().Line, stmt.Pos().Column, "ssh body only allows calls")
		}
		switch call.Name {
		case "user":
			if err := markPresent(present, filePath, call, "ssh_user"); err != nil {
				return err
			}
			s, err := conf.SingleStringArg(call, filePath)
			if err != nil {
				return err
			}
			c.SSHUser = s
		case "key":
			if err := markPresent(present, filePath, call, "ssh_key"); err != nil {
				return err
			}
			s, err := conf.SingleStringArg(call, filePath)
			if err != nil {
				return err
			}
			c.SSHKeyFile = s
		case "port":
			if err := markPresent(present, filePath, call, "ssh_port"); err != nil {
				return err
			}
			n, err := conf.SingleIntArg(call, filePath)
			if err != nil {
				return err
			}
			c.SSHPort = n
		case "use_sudo":
			if err := markPresent(present, filePath, call, "use_sudo"); err != nil {
				return err
			}
			v, err := conf.SingleBoolArg(call, filePath)
			if err != nil {
				return err
			}
			c.UseSUDO = v
		case "strict_host_key_checking":
			if err := markPresent(present, filePath, call, "strict_host_key_checking"); err != nil {
				return err
			}
			s, err := conf.SingleStringArg(call, filePath)
			if err != nil {
				return err
			}
			c.StrictHostKeyChecking = s
		default:
			return conf.UnknownSetting(filePath, call.NamePos, call.Name, kiveConfSSHKnown)
		}
	}
	return nil
}

func lowerLogConfigBlock(filePath string, b *conf.Block, c *KiveConf, present map[string]struct{}) error {
	for _, stmt := range b.Body {
		call, ok := stmt.(*conf.Call)
		if !ok {
			return conf.Err(filePath, stmt.Pos().Line, stmt.Pos().Column, "log_config body only allows calls")
		}
		switch call.Name {
		case "format":
			if err := markPresent(present, filePath, call, "log_format"); err != nil {
				return err
			}
			s, err := conf.SingleStringArg(call, filePath)
			if err != nil {
				return err
			}
			c.LogFormat = s
		case "run_retention_count":
			if err := markPresent(present, filePath, call, "log_run_retention_count"); err != nil {
				return err
			}
			n, err := conf.SingleIntArg(call, filePath)
			if err != nil {
				return err
			}
			c.LogRunRetentionCount = n
		case "run_retention_days":
			if err := markPresent(present, filePath, call, "log_run_retention_days"); err != nil {
				return err
			}
			n, err := conf.SingleIntArg(call, filePath)
			if err != nil {
				return err
			}
			c.LogRunRetentionDays = n
		default:
			return conf.UnknownSetting(filePath, call.NamePos, call.Name, kiveConfLogConfigKnown)
		}
	}
	return nil
}

func lowerBackupBlock(filePath string, b *conf.Block, c *KiveConf, present map[string]struct{}) error {
	for _, stmt := range b.Body {
		call, ok := stmt.(*conf.Call)
		if !ok {
			return conf.Err(filePath, stmt.Pos().Line, stmt.Pos().Column, "backup body only allows calls")
		}
		switch call.Name {
		case "retention_count":
			if err := markPresent(present, filePath, call, "backup_retention_count"); err != nil {
				return err
			}
			n, err := conf.SingleIntArg(call, filePath)
			if err != nil {
				return err
			}
			c.BackupRetentionCount = n
		default:
			return conf.UnknownSetting(filePath, call.NamePos, call.Name, kiveConfBackupKnown)
		}
	}
	return nil
}

func lowerHealthBlock(filePath string, b *conf.Block, c *KiveConf, present map[string]struct{}) error {
	for _, stmt := range b.Body {
		call, ok := stmt.(*conf.Call)
		if !ok {
			return conf.Err(filePath, stmt.Pos().Line, stmt.Pos().Column, "health body only allows calls")
		}
		switch call.Name {
		case "wait_seconds":
			if err := markPresent(present, filePath, call, "health_wait_seconds"); err != nil {
				return err
			}
			n, err := conf.SingleIntArg(call, filePath)
			if err != nil {
				return err
			}
			c.HealthWaitSeconds = n
		case "poll_interval_seconds":
			if err := markPresent(present, filePath, call, "health_poll_interval_seconds"); err != nil {
				return err
			}
			n, err := conf.SingleIntArg(call, filePath)
			if err != nil {
				return err
			}
			c.HealthPollIntervalSeconds = n
		default:
			return conf.UnknownSetting(filePath, call.NamePos, call.Name, kiveConfHealthKnown)
		}
	}
	return nil
}

func lowerDeployBlock(filePath string, b *conf.Block, c *KiveConf, present map[string]struct{}) error {
	for _, stmt := range b.Body {
		call, ok := stmt.(*conf.Call)
		if !ok {
			return conf.Err(filePath, stmt.Pos().Line, stmt.Pos().Column, "deploy body only allows calls")
		}
		switch call.Name {
		case "max_concurrent_syncs":
			if err := markPresent(present, filePath, call, "max_concurrent_syncs"); err != nil {
				return err
			}
			n, err := conf.SingleIntArg(call, filePath)
			if err != nil {
				return err
			}
			c.MaxConcurrentSyncs = n
		default:
			return conf.UnknownSetting(filePath, call.NamePos, call.Name, kiveConfDeployKnown)
		}
	}
	return nil
}

func lowerInterpretersBlock(filePath string, b *conf.Block, c *KiveConf, present map[string]struct{}) error {
	for _, stmt := range b.Body {
		call, ok := stmt.(*conf.Call)
		if !ok {
			return conf.Err(filePath, stmt.Pos().Line, stmt.Pos().Column, "interpreters body only allows calls")
		}
		switch call.Name {
		case "python_path":
			if err := markPresent(present, filePath, call, "python_path"); err != nil {
				return err
			}
			s, err := conf.SingleStringArg(call, filePath)
			if err != nil {
				return err
			}
			c.PythonPath = s
		case "js_path":
			if err := markPresent(present, filePath, call, "js_path"); err != nil {
				return err
			}
			s, err := conf.SingleStringArg(call, filePath)
			if err != nil {
				return err
			}
			c.JSPath = s
		case "ruby_path":
			if err := markPresent(present, filePath, call, "ruby_path"); err != nil {
				return err
			}
			s, err := conf.SingleStringArg(call, filePath)
			if err != nil {
				return err
			}
			c.RubyPath = s
		case "bash_path":
			if err := markPresent(present, filePath, call, "bash_path"); err != nil {
				return err
			}
			s, err := conf.SingleStringArg(call, filePath)
			if err != nil {
				return err
			}
			c.BashPath = s
		default:
			return conf.UnknownSetting(filePath, call.NamePos, call.Name, kiveConfInterpretersKnown)
		}
	}
	return nil
}

func lowerJobSignerBlock(filePath string, b *conf.Block, c *KiveConf, present map[string]struct{}) error {
	for _, stmt := range b.Body {
		call, ok := stmt.(*conf.Call)
		if !ok {
			return conf.Err(filePath, stmt.Pos().Line, stmt.Pos().Column, "job_signer body only allows calls")
		}
		switch call.Name {
		case "ca":
			if err := markPresent(present, filePath, call, "job_signer_ca"); err != nil {
				return err
			}
			s, err := conf.SingleStringArg(call, filePath)
			if err != nil {
				return err
			}
			c.JobSignerCA = s
		case "ca_trust":
			if err := markPresent(present, filePath, call, "job_signer_ca_trust"); err != nil {
				return err
			}
			ss, err := conf.AsStrings(call, filePath)
			if err != nil {
				return err
			}
			c.JobSignerCATrust = ss
		default:
			return conf.UnknownSetting(filePath, call.NamePos, call.Name, kiveConfJobSignerKnown)
		}
	}
	return nil
}

func lowerFeaturesBlock(filePath string, b *conf.Block, c *KiveConf, present map[string]struct{}) error {
	for _, stmt := range b.Body {
		call, ok := stmt.(*conf.Call)
		if !ok {
			return conf.Err(filePath, stmt.Pos().Line, stmt.Pos().Column, "features body only allows calls")
		}
		switch call.Name {
		case "promotion":
			if err := markPresent(present, filePath, call, "features_promotion"); err != nil {
				return err
			}
			v, err := conf.SingleBoolArg(call, filePath)
			if err != nil {
				return err
			}
			c.Features.Promotion = v
		case "observe":
			if err := markPresent(present, filePath, call, "features_observe"); err != nil {
				return err
			}
			v, err := conf.SingleBoolArg(call, filePath)
			if err != nil {
				return err
			}
			c.Features.Observe = BoolPtr(v)
		default:
			return conf.UnknownSetting(filePath, call.NamePos, call.Name, kiveConfFeaturesKnown)
		}
	}
	return nil
}

// EmitKiveConf serializes KiveConf to block dialect.
func EmitKiveConf(c KiveConf) string {
	f := &conf.File{}

	f.Stmts = append(f.Stmts, conf.CallStmt("timezone", ResolveTimezone(c.Timezone)))

	sshBody := []conf.Stmt{conf.CallStmtBool("use_sudo", c.UseSUDO)}
	if c.SSHUser != "" {
		sshBody = append(sshBody, conf.CallStmt("user", c.SSHUser))
	}
	if c.SSHKeyFile != "" {
		sshBody = append(sshBody, conf.CallStmt("key", c.SSHKeyFile))
	}
	if c.SSHPort != 0 {
		sshBody = append(sshBody, conf.CallStmtInt("port", c.SSHPort))
	}
	if c.StrictHostKeyChecking != "" {
		sshBody = append(sshBody, conf.CallStmt("strict_host_key_checking", c.StrictHostKeyChecking))
	}
	f.Stmts = append(f.Stmts, &conf.Block{Name: "ssh", Body: sshBody})

	logBody := []conf.Stmt{}
	if c.LogFormat != "" {
		logBody = append(logBody, conf.CallStmt("format", c.LogFormat))
	}
	logBody = append(logBody,
		conf.CallStmtInt("run_retention_count", c.LogRunRetentionCount),
		conf.CallStmtInt("run_retention_days", c.LogRunRetentionDays),
	)
	f.Stmts = append(f.Stmts, &conf.Block{Name: "log_config", Body: logBody})

	f.Stmts = append(f.Stmts, &conf.Block{
		Name: "backup",
		Body: []conf.Stmt{conf.CallStmtInt("retention_count", c.BackupRetentionCount)},
	})
	healthBody := []conf.Stmt{
		conf.CallStmtInt("wait_seconds", c.HealthWaitSeconds),
		conf.CallStmtInt("poll_interval_seconds", c.HealthPollIntervalSeconds),
	}
	f.Stmts = append(f.Stmts, &conf.Block{
		Name: "health",
		Body: healthBody,
	})
	if c.MaxConcurrentSyncs >= 1 {
		f.Stmts = append(f.Stmts, &conf.Block{
			Name: "deploy",
			Body: []conf.Stmt{conf.CallStmtInt("max_concurrent_syncs", c.MaxConcurrentSyncs)},
		})
	}

	interpBody := []conf.Stmt{}
	if c.PythonPath != "" {
		interpBody = append(interpBody, conf.CallStmt("python_path", c.PythonPath))
	}
	if c.JSPath != "" {
		interpBody = append(interpBody, conf.CallStmt("js_path", c.JSPath))
	}
	if c.RubyPath != "" {
		interpBody = append(interpBody, conf.CallStmt("ruby_path", c.RubyPath))
	}
	if c.BashPath != "" {
		interpBody = append(interpBody, conf.CallStmt("bash_path", c.BashPath))
	}
	if len(interpBody) > 0 {
		f.Stmts = append(f.Stmts, &conf.Block{Name: "interpreters", Body: interpBody})
	}

	signerBody := []conf.Stmt{}
	if c.JobSignerCA != "" {
		signerBody = append(signerBody, conf.CallStmt("ca", c.JobSignerCA))
	}
	if len(c.JobSignerCATrust) > 0 {
		signerBody = append(signerBody, conf.CallStmt("ca_trust", c.JobSignerCATrust...))
	}
	if len(signerBody) > 0 {
		f.Stmts = append(f.Stmts, &conf.Block{Name: "job_signer", Body: signerBody})
	}

	if c.Features.Any() {
		featBody := []conf.Stmt{}
		if c.Features.Promotion {
			featBody = append(featBody, conf.CallStmtBool("promotion", true))
		}
		if c.Features.Observe != nil && !*c.Features.Observe {
			featBody = append(featBody, conf.CallStmtBool("observe", false))
		}
		f.Stmts = append(f.Stmts, &conf.Block{Name: "features", Body: featBody})
	}

	if c.PortRange != "" {
		if stmt, ok := emitPortRangeStmt(c.PortRange); ok {
			f.Stmts = append(f.Stmts, stmt)
		}
	}

	certsTTL := c.CertsTTL
	if certsTTL == 0 {
		certsTTL = defaultCertsTTL
	}
	f.Stmts = append(f.Stmts,
		conf.CallStmtInt(keyCertsTTL, certsTTL),
		conf.CallStmtInt(keyCertsRenewalBuffer, c.CertsRenewalBuffer),
		conf.CallStmtBool(keyIptables, c.Iptables),
	)
	if c.JobsProfile != "" {
		f.Stmts = append(f.Stmts, conf.CallStmt(keyJobsProfile, c.JobsProfile))
	}

	return conf.Emit(f, conf.EmitOptions{})
}

func loadKiveConfFromDisk(confPath string) (KiveConf, error) {
	kiveData, err := conf.ReadBytes(confPath)
	if err != nil {
		if os.IsNotExist(err) {
			return KiveConf{}, nil
		}
		if _, ok := err.(*conf.Error); ok {
			return KiveConf{}, fmt.Errorf("%w: %v", ErrInvalidKiveConf, err)
		}
		return KiveConf{}, fmt.Errorf("%w: %w", ErrUnexpectedError, err)
	}
	confVal, present, err := ParseKiveConf(confPath, kiveData)
	if err != nil {
		return KiveConf{}, fmt.Errorf("%w: %v", ErrInvalidKiveConf, err)
	}
	if err := applyKiveConfDefaultsPresent(&confVal, present); err != nil {
		return KiveConf{}, err
	}
	if _, err := confVal.SSHStrictHostKeyChecking(); err != nil {
		return KiveConf{}, err
	}
	WarnIfHostKeyCheckingDisabled(confVal.StrictHostKeyChecking)
	if _, err := SanitizeSSHKeyFilename(confVal.SSHKeyFile); err != nil {
		return KiveConf{}, err
	}
	if strings.TrimSpace(confVal.PortRange) != "" {
		if _, err := ParsePortRangeValue(confVal.PortRange); err != nil {
			return KiveConf{}, fmt.Errorf("%w: %v", ErrInvalidKiveConf, err)
		}
	}
	if err := ValidateJobSignerConfig(confVal); err != nil {
		return KiveConf{}, err
	}
	if _, err := time.LoadLocation(ResolveTimezone(confVal.Timezone)); err != nil {
		return KiveConf{}, fmt.Errorf("%w: timezone: invalid IANA timezone %q", ErrInvalidKiveConf, confVal.Timezone)
	}
	return confVal, nil
}

func applyKiveConfDefaultsPresent(conf *KiveConf, present map[string]struct{}) error {
	if _, ok := present["log_run_retention_count"]; !ok {
		conf.LogRunRetentionCount = DefaultLogRunRetentionCount
	}
	if _, ok := present["log_run_retention_days"]; !ok {
		conf.LogRunRetentionDays = DefaultLogRunRetentionDays
	}
	if _, ok := present["backup_retention_count"]; !ok {
		conf.BackupRetentionCount = DefaultBackupRetentionCount
	}
	if _, ok := present["health_wait_seconds"]; !ok {
		conf.HealthWaitSeconds = DefaultHealthWaitSeconds
	}
	if _, ok := present["health_poll_interval_seconds"]; !ok {
		conf.HealthPollIntervalSeconds = DefaultHealthPollIntervalSeconds
	}
	if _, ok := present["max_concurrent_syncs"]; !ok {
		conf.MaxConcurrentSyncs = DefaultMaxConcurrentSyncs
	}
	if _, ok := present[keyCertsTTL]; !ok {
		conf.CertsTTL = defaultCertsTTL
	} else if conf.CertsTTL == 0 {
		conf.CertsTTL = defaultCertsTTL
	}
	if _, ok := present[keyCertsRenewalBuffer]; !ok {
		conf.CertsRenewalBuffer = defaultCertsRenewalBuffer
	}
	if _, ok := present["timezone"]; !ok {
		conf.Timezone = DefaultTimezone
	} else {
		conf.Timezone = ResolveTimezone(conf.Timezone)
	}
	// iptables / jobs_profile zero values are the defaults when absent
	if conf.LogRunRetentionCount < 0 {
		return fmt.Errorf("%w: log_run_retention_count must be >= 0", ErrInvalidKiveConf)
	}
	if conf.LogRunRetentionDays < 0 {
		return fmt.Errorf("%w: log_run_retention_days must be >= 0", ErrInvalidKiveConf)
	}
	if conf.BackupRetentionCount < 0 {
		return fmt.Errorf("%w: backup_retention_count must be >= 0", ErrInvalidKiveConf)
	}
	if conf.HealthWaitSeconds < 0 {
		return fmt.Errorf("%w: health_wait_seconds must be >= 0", ErrInvalidKiveConf)
	}
	if conf.HealthPollIntervalSeconds < 0 {
		return fmt.Errorf("%w: health_poll_interval_seconds must be >= 0", ErrInvalidKiveConf)
	}
	if conf.MaxConcurrentSyncs < 1 {
		return fmt.Errorf("%w: max_concurrent_syncs must be >= 1", ErrInvalidKiveConf)
	}
	if conf.CertsTTL < 0 {
		return fmt.Errorf("%w: %s must be >= 0", ErrInvalidKiveConf, keyCertsTTL)
	}
	if conf.CertsRenewalBuffer < 0 {
		return fmt.Errorf("%w: %s must be >= 0", ErrInvalidKiveConf, keyCertsRenewalBuffer)
	}
	return nil
}
