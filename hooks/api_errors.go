// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package hooks

import "net/http"

type apiResponseError struct {
	message    string
	statusCode int
}

func (e *apiResponseError) write(w http.ResponseWriter) {
	http.Error(w, e.message, e.statusCode)
}

var runtimeAPIErrors = struct {
	invalidContentType *apiResponseError
	missingAllocation  *apiResponseError
	unknownAllocation  *apiResponseError
	emptyBody          *apiResponseError
	invalidJSON        *apiResponseError
	missingKeyFields   *apiResponseError
	invalidTTL         *apiResponseError
	namespaceDenied    *apiResponseError
	storeKeyNotFound   *apiResponseError
	writeDuringHealth  *apiResponseError
	semaphoreTimeout   *apiResponseError
	semaphoreConflict  *apiResponseError
	missingHookHeaders *apiResponseError
	internalError      *apiResponseError
}{
	invalidContentType: &apiResponseError{"Content-Type must be application/json", http.StatusUnsupportedMediaType},
	missingAllocation:  &apiResponseError{"X-ALLOCATION-ID header is missing", http.StatusBadRequest},
	unknownAllocation:  &apiResponseError{"Invalid allocation ID", http.StatusNotFound},
	emptyBody:          &apiResponseError{"Failed to read request body", http.StatusBadRequest},
	invalidJSON:        &apiResponseError{"Invalid JSON format", http.StatusBadRequest},
	missingKeyFields:   &apiResponseError{"Both namespace and key are required", http.StatusBadRequest},
	invalidTTL:         &apiResponseError{"ttl must be a non-negative integer (seconds)", http.StatusBadRequest},
	namespaceDenied:    &apiResponseError{"Invalid or unauthorized namespace", http.StatusBadRequest},
	storeKeyNotFound:   &apiResponseError{"KV get operation failed", http.StatusNotFound},
	writeDuringHealth:  &apiResponseError{"KV writes are not allowed during liveness or readiness", http.StatusBadRequest},
	semaphoreTimeout:   &apiResponseError{"Timed out waiting for semaphore", http.StatusRequestTimeout},
	semaphoreConflict:  &apiResponseError{"Semaphore acquire or release failed", http.StatusConflict},
	missingHookHeaders: &apiResponseError{"EVENT and HOOK headers are required", http.StatusBadRequest},
	internalError:      &apiResponseError{"Internal server error", http.StatusInternalServerError},
}
