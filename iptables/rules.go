// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

// Package iptables builds per-worker iptables-restore fragments from the catalog.
package iptables

import (
	"database/sql"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"unicode"

	"kive/bucket"
	"kive/data"
	"kive/workspace"
)

const (
	chainPrefix   = "KIVE_"
	maxChainLen   = 28
	maxChainIDLen = maxChainLen - len(chainPrefix) // 22
	rulesFileName = "iptables.rules"
	rulesDirName  = "etc"
)

// PortAllow is one catalog port allow for a worker.
type PortAllow struct {
	Job      string
	Name     string
	Port     int
	Protocol string
	Exposure string
}

// ChainName returns the bucket-scoped iptables user chain (max 28 chars).
func ChainName(bucketID string) string {
	id := sanitizeID(bucketID)
	if len(id) > maxChainIDLen {
		id = id[:maxChainIDLen]
	}
	return chainPrefix + id
}

func sanitizeID(bucketID string) string {
	var b strings.Builder
	b.Grow(len(bucketID))
	for _, r := range bucketID {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '_':
			b.WriteRune(r)
		}
	}
	return b.String()
}

// IPTablesProto maps manifest protocol to iptables -p.
func IPTablesProto(protocol string) string {
	p, err := workspace.NormalizePortProtocol(protocol)
	if err != nil || p == "" {
		p = workspace.DefaultPortProtocol
	}
	switch p {
	case "udp":
		return "udp"
	default:
		return "tcp"
	}
}

// RenderFragment builds an iptables-restore filter fragment for one worker.
func RenderFragment(bucketID, workerIP string, peers []string, allows []PortAllow) string {
	chain := ChainName(bucketID)
	var b strings.Builder
	b.WriteString("*filter\n")
	b.WriteString(":")
	b.WriteString(chain)
	b.WriteString(" - [0:0]\n")
	b.WriteString("-F ")
	b.WriteString(chain)
	b.WriteByte('\n')
	b.WriteString("-A ")
	b.WriteString(chain)
	b.WriteString(" -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT\n")

	sortedPeers := append([]string(nil), peers...)
	sort.Strings(sortedPeers)

	for _, a := range allows {
		proto := IPTablesProto(a.Protocol)
		exposure := a.Exposure
		if exposure == "" {
			exposure = workspace.DefaultPortExposure
		}
		comment := fmt.Sprintf("kive:%s:%s:%s", bucketID, a.Job, a.Name)
		switch exposure {
		case "public":
			fmt.Fprintf(&b, "-A %s -p %s --dport %d -m comment --comment %q -j ACCEPT\n",
				chain, proto, a.Port, comment)
		default: // cluster
			for _, peer := range sortedPeers {
				if peer == "" || peer == workerIP {
					continue
				}
				fmt.Fprintf(&b, "-A %s -s %s -p %s --dport %d -m comment --comment %q -j ACCEPT\n",
					chain, peer, proto, a.Port, comment)
			}
		}
	}

	b.WriteString("-A ")
	b.WriteString(chain)
	b.WriteString(" -j RETURN\n")
	b.WriteString("COMMIT\n")
	return b.String()
}

// ListWorkerPortAllows returns ports for non-removed allocations on workerIP.
func ListWorkerPortAllows(tx *sql.Tx, workerIP string) ([]PortAllow, error) {
	rows, err := tx.Query(`
		SELECT a.job, jp.name, jp.port, jp.protocol, jp.exposure
		FROM allocations a
		JOIN jobs j ON j.name = a.job
		JOIN job_ports jp ON jp.job_id = j.job_id
		WHERE a.worker_ip = ? AND a.removed = 0
		ORDER BY a.job, jp.name`, workerIP)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]PortAllow, 0)
	for rows.Next() {
		var a PortAllow
		if err := rows.Scan(&a.Job, &a.Name, &a.Port, &a.Protocol, &a.Exposure); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		if a.Protocol == "" {
			a.Protocol = workspace.DefaultPortProtocol
		}
		if a.Exposure == "" {
			a.Exposure = workspace.DefaultPortExposure
		}
		out = append(out, a)
	}
	if err := data.RowsErr(rows); err != nil {
		return nil, err
	}
	return out, nil
}

// StageWorkerRules writes etc/iptables.rules under the worker staging directory.
func StageWorkerRules(tx *sql.Tx, bucketID, workerIP string) error {
	allows, err := ListWorkerPortAllows(tx, workerIP)
	if err != nil {
		return err
	}
	peers, err := data.GetWorkers(tx, nil)
	if err != nil {
		return err
	}
	fragment := RenderFragment(bucketID, workerIP, peers, allows)
	dir := path.Join(bucket.GetTempWorkerPath(workerIP), rulesDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return bucket.UnexpectedError(err)
	}
	if err := os.WriteFile(path.Join(dir, rulesFileName), []byte(fragment), 0o644); err != nil {
		return bucket.UnexpectedError(err)
	}
	return nil
}

// RulesPath returns the on-worker path to iptables.rules.
func RulesPath(bucketID string) string {
	return path.Join(bucket.WorkerBucketPath(bucketID), rulesDirName, rulesFileName)
}

// ApplyScriptPath returns the on-worker path to apply_iptables.
func ApplyScriptPath(bucketID string) string {
	return path.Join(bucket.WorkerBucketPath(bucketID), "bin", "apply_iptables")
}
