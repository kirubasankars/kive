// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package data

import (
	"database/sql"
	"fmt"
	"strconv"

	"kive/bucket"
)

// PortAssignments maps job name → port key → assigned number.
type PortAssignments map[string]map[string]int

// GetAllPortAssignments loads current job_ports rows keyed by job and port name.
func GetAllPortAssignments(tx *sql.Tx) (PortAssignments, error) {
	rows, err := tx.Query(`
		SELECT j.name, jp.name, jp.port
		FROM job_ports jp
		JOIN jobs j ON j.job_id = jp.job_id
	`)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	assignments := make(PortAssignments)
	for rows.Next() {
		var jobName, portName string
		var port int
		if err := rows.Scan(&jobName, &portName, &port); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		if assignments[jobName] == nil {
			assignments[jobName] = make(map[string]int)
		}
		assignments[jobName][portName] = port
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	return assignments, nil
}

// GetPortMap returns port name → number for one job.
func GetPortMap(tx *sql.Tx, jobName string) (map[string]string, error) {
	rows, err := tx.Query(
		`SELECT name, port FROM job_ports WHERE job_id = (SELECT job_id FROM jobs WHERE name = ?)`,
		jobName,
	)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	ports := make(map[string]string)
	for rows.Next() {
		var name string
		var port int
		if err := rows.Scan(&name, &port); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		ports[name] = strconv.Itoa(port)
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	return ports, nil
}

// GetPortMapInt returns port name → number for one job.
func GetPortMapInt(tx *sql.Tx, jobName string) (map[string]int, error) {
	stringPorts, err := GetPortMap(tx, jobName)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int, len(stringPorts))
	for name, value := range stringPorts {
		port, err := strconv.Atoi(value)
		if err != nil {
			return nil, bucket.DatabaseError(err)
		}
		out[name] = port
	}
	return out, nil
}

// GetPortNumber returns the assigned port number for one job port name.
func GetPortNumber(tx *sql.Tx, jobName, portName string) (int, error) {
	var port int
	err := tx.QueryRow(
		`SELECT port FROM job_ports WHERE job_id = (SELECT job_id FROM jobs WHERE name = ?) AND name = ?`,
		jobName, portName,
	).Scan(&port)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("%w: job %s port %q", bucket.ErrInvalidManifest, jobName, portName)
	}
	if err != nil {
		return 0, bucket.DatabaseError(err)
	}
	return port, nil
}
