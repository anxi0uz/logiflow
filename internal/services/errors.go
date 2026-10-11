package services

import "errors"

type BusinessError struct {
	Code string
}

func (e *BusinessError) Error() string { return e.Code }

var (
	ErrCounterpartyArchived       = &BusinessError{Code: "COUNTERPARTY_ARCHIVED"}
	ErrTransportDetailsRequired   = &BusinessError{Code: "TRANSPORT_DETAILS_REQUIRED"}
	ErrDeliveryReceiptRequired    = &BusinessError{Code: "DELIVERY_RECEIPT_REQUIRED"}
	ErrDeliveryReceiptConflict    = &BusinessError{Code: "DELIVERY_RECEIPT_CONFLICT"}
	ErrIncidentOpen               = &BusinessError{Code: "INCIDENT_OPEN"}
	ErrIncidentStale              = &BusinessError{Code: "INCIDENT_STALE"}
	ErrIncidentResolutionInvalid  = &BusinessError{Code: "INCIDENT_RESOLUTION_INVALID"}
	ErrWarehouseOperationRequired = &BusinessError{Code: "WAREHOUSE_OPERATION_REQUIRED"}
	ErrWarehouseOperationConflict = &BusinessError{Code: "WAREHOUSE_OPERATION_CONFLICT"}
	ErrWarehouseInactive          = &BusinessError{Code: "WAREHOUSE_INACTIVE"}
	ErrWarehouseInUse             = &BusinessError{Code: "WAREHOUSE_IN_USE"}
	ErrForbidden                  = &BusinessError{Code: "FORBIDDEN_ACTOR"}
	ErrInvalidOrderTransition     = &BusinessError{Code: "INVALID_ORDER_TRANSITION"}
	ErrAssignmentStale            = &BusinessError{Code: "ASSIGNMENT_STALE"}
	ErrAssignmentExpired          = &BusinessError{Code: "ASSIGNMENT_EXPIRED"}
	ErrResourceAlreadyReserved    = &BusinessError{Code: "RESOURCE_ALREADY_RESERVED"}
	ErrDriverNotEligible          = &BusinessError{Code: "DRIVER_NOT_ELIGIBLE"}
	ErrDriverLicenseExpired       = &BusinessError{Code: "DRIVER_LICENSE_EXPIRED"}
	ErrDriverOutsideShift         = &BusinessError{Code: "DRIVER_OUTSIDE_SHIFT"}
	ErrVehicleNotOperational      = &BusinessError{Code: "VEHICLE_NOT_OPERATIONAL"}
	ErrVehicleCapacityExceeded    = &BusinessError{Code: "VEHICLE_CAPACITY_EXCEEDED"}
	ErrInvalidTimeWindow          = &BusinessError{Code: "INVALID_TIME_WINDOW"}
	ErrInvalidOrderInput          = &BusinessError{Code: "INVALID_ORDER_INPUT"}
	ErrVehicleDocumentInvalid     = &BusinessError{Code: "VEHICLE_DOCUMENT_INVALID"}
)

func BusinessErrorCode(err error) (string, bool) {
	var businessErr *BusinessError
	if errors.As(err, &businessErr) {
		return businessErr.Code, true
	}
	return "", false
}
