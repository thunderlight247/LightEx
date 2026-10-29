package service

import (
	"errors"

	"github.com/light-group/light-ex-backend/internal/modules/payment/repository/postgres"
)

// mapErr converts repository-level not-found errors into service-level errors.
func mapErr(err error) error {
	if errors.Is(err, postgres.ErrPaymentNotFound) {
		return ErrPaymentNotFound
	}
	return err
}
