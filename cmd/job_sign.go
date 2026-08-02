// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package cmd

import (
	"fmt"
	"os"
	"time"

	"kive/bucket"
	"kive/jobsign"

	"github.com/spf13/cobra"
)

var jobSignCmd = &cobra.Command{
	Use:   "sign <job>",
	Short: "Sign a job's source content",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runJobSign(args[0])
	},
}

var jobSignGenerateCACertCmd = &cobra.Command{
	Use:   "generate-ca-cert",
	Short: "Generate the configured job signing CA",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runJobSignGenerateCA()
	},
}

func runJobSign(jobName string) error {
	conf, err := bucket.GetKiveConf()
	if err != nil {
		return err
	}
	certPath, keyPath, _, err := bucket.JobSignerPaths(conf)
	if err != nil {
		return err
	}
	if err := bucket.CheckJobSignerPrivateKeyPermissions(keyPath); err != nil {
		return err
	}
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return fmt.Errorf("read job signer certificate: %w", err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("read job signer key: %w", err)
	}
	snapshot, err := jobsign.Capture(jobName)
	if err != nil {
		return err
	}
	bundle, result, err := jobsign.Sign(snapshot, certPEM, keyPEM)
	if err != nil {
		return err
	}
	target, err := jobsign.AtomicWriteSignature(jobName, bundle)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmdOutput(), "signed job=%s vendor=%q sha256=%s certificate=%s\n",
		jobName, result.Signer, result.Digest, target)
	return nil
}

func runJobSignGenerateCA() error {
	conf, err := bucket.GetKiveConf()
	if err != nil {
		return err
	}
	certPath, keyPath, signerName, err := bucket.JobSignerPaths(conf)
	if err != nil {
		return err
	}
	if err := jobsign.GenerateCA(certPath, keyPath, signerName, time.Now()); err != nil {
		return err
	}
	fmt.Fprintf(cmdOutput(), "generated job signer certificate=%s key=%s\n", certPath, keyPath)
	fmt.Fprintln(cmdOutput(), "distribute only the public .crt file to bucket operators")
	return nil
}

func cmdOutput() *os.File {
	return os.Stdout
}

func init() {
	jobCmd.AddCommand(jobSignCmd)
	jobSignCmd.AddCommand(jobSignGenerateCACertCmd)
}
