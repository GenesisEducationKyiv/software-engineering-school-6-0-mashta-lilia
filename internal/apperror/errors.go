// Package apperror defines sentinel errors that cross layer boundaries.
// Keeping them in a neutral package prevents the service layer from
// importing infrastructure (repository) just to compare error values.
package apperror

import "errors"

// ErrNotFound is returned by repositories when a record does not exist.
// Service code uses errors.Is(err, apperror.ErrNotFound) to detect this.
var ErrNotFound = errors.New("not found")
