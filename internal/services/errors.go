package services

import "errors"

type BusinessError struct {
	Code string
}

func (e *BusinessError) Error() string { return e.Code }

var (
	ErrForbidden               = &BusinessError{Code: "FORBIDDEN_ACTOR"}
	ErrInvalidOrderTransition  = &BusinessError{Code: "INVALID_ORDER_TRANSITION"}
	ErrAssignmentStale         = &BusinessError{Code: "ASSIGNMENT_STALE"}
	ErrAssignmentExpired       = &BusinessError{Code: "ASSIGNMENT_EXPIRED"}
	ErrResourceAlreadyReserved = &BusinessError{Code: "RESOURCE_ALREADY_RESERVED"}
	ErrDriverNotEligible       = &BusinessError{Code: "DRIVER_NOT_ELIGIBLE"}
	ErrDriverLicenseExpired    = &BusinessError{Code: "DRIVER_LICENSE_EXPIRED"}
	ErrDriverOutsideShift      = &BusinessError{Code: "DRIVER_OUTSIDE_SHIFT"}
	ErrVehicleNotOperational   = &BusinessError{Code: "VEHICLE_NOT_OPERATIONAL"}
	ErrVehicleCapacityExceeded = &BusinessError{Code: "VEHICLE_CAPACITY_EXCEEDED"}
	ErrInvalidTimeWindow       = &BusinessError{Code: "INVALID_TIME_WINDOW"}
)

func BusinessErrorCode(err error) (string, bool) {
	var businessErr *BusinessError
	if errors.As(err, &businessErr) {
		return businessErr.Code, true
	}
	return "", false
}
