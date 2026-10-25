package helper

import (
	"encoding/json"
	"errors"
	"io"

	"eflow.local/new-api/common"
)

type RequestValidationError struct {
	err error
}

func (e *RequestValidationError) Error() string {
	return e.err.Error()
}

func (e *RequestValidationError) Unwrap() error {
	return e.err
}

func NewRequestValidationError(err error) error {
	if err == nil || IsRequestValidationError(err) {
		return err
	}
	return &RequestValidationError{err: err}
}

func IsRequestValidationError(err error) bool {
	var validationErr *RequestValidationError
	return errors.As(err, &validationErr)
}

func normalizeRequestValidationError(err error) error {
	if err == nil || common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
		return err
	}
	if IsRequestValidationError(err) {
		return err
	}

	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &syntaxErr) || errors.As(err, &typeErr) || errors.Is(err, io.EOF) {
		return NewRequestValidationError(err)
	}
	return err
}
