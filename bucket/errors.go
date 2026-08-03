// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package bucket

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidWorkerJSON                = errors.New("invalid worker.json")
	ErrInvalidManifest                  = errors.New("invalid job.conf")
	ErrInvalidJobVars                   = errors.New("invalid vars.conf")
	ErrInvalidKiveConf                 = errors.New("invalid kive.conf")
	ErrInvalidBucketConf                = errors.New("invalid bucket.conf")
	ErrUnexpectedError                  = errors.New("unexpected error")
	ErrKeyNotFound                      = errors.New("key not found")
	ErrNotInitialized                   = errors.New("kive is not initialized")
	ErrSchemaUpgradeRequired            = errors.New("database schema upgrade required")
	ErrSchemaTooNew                     = errors.New("database schema is newer than this kive binary")
	ErrDatabase                         = errors.New("database error")
	ErrNotFound                         = errors.New("not found")
	ErrPortCollision                    = errors.New("port collision")
	ErrPortKeyFormat                    = errors.New("invalid port key format")
	ErrInvalidPortRange                 = errors.New("invalid port range in config")
	ErrPortRangeExhausted               = errors.New("port range exhausted")
	ErrInvalidManifestPort                = errors.New("invalid manifest port declaration")
	ErrInvalidHookConfiguration         = errors.New("invalid hook configuration")
	ErrInvalidHookDemand                = errors.New("invalid hook demand")
	ErrHookDemandVersionMismatch        = errors.New("hook demand version mismatch")
	ErrInvalidJobVersion                = errors.New("invalid job version")
	ErrInvalidVersionSpecifier          = errors.New("invalid version specifier")
	ErrBackwardCompatibility            = errors.New("backward compatibility check failed")
	ErrVersionRollbackNotAllowed        = errors.New("version rollback is not allowed")
	ErrCircularHookDependency           = errors.New("circular hook dependency")
	ErrHookFileNotFound                 = errors.New("hook file not found")
	ErrInvalidJob                       = errors.New("invalid job")
	ErrInvalidJobName                   = errors.New("invalid job name")
	ErrInsufficientResource             = errors.New("insufficient resource")
	ErrInsufficientAllocations          = errors.New("missing allocations")
	ErrHealthCheckFailed                = errors.New("health check failed")
	ErrUnsupportedResourceConfiguration = errors.New("unsupported resource configuration")
	ErrRunCommand                       = errors.New("run command failed")
	ErrHookFailed                       = errors.New("hook failed")
	ErrWorkerPrerequisites              = errors.New("worker prerequisites not met")
	ErrHostPrerequisites                = errors.New("host prerequisites not met")
	ErrBucketBusy                       = errors.New("bucket busy: another kive operation is in progress")
)

// Deprecated: use ErrInvalidWorkerJSON.
var ErrInvaildWorkerJSON = ErrInvalidWorkerJSON

// Deprecated: use ErrInsufficientResource.
var ErrInSufficientResource = ErrInsufficientResource

// Deprecated: use ErrUnsupportedResourceConfiguration.
var ErrUnsupportedResourceConfigration = ErrUnsupportedResourceConfiguration

func KeyNotFoundError(namespace, key string) error {
	return fmt.Errorf("%w: namespace %s, key %s", ErrKeyNotFound, namespace, key)
}

func DatabaseError(err error) error {
	return fmt.Errorf("%w: %w", ErrDatabase, err)
}

func UnexpectedError(err error) error {
	return fmt.Errorf("%w: %w", ErrUnexpectedError, err)
}

func NotFoundError(domain string) error {
	return fmt.Errorf("%w: %s", ErrNotFound, domain)
}
