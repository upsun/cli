package store

import (
	"errors"

	"golang.org/x/sys/windows"
)

func isSharingViolation(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}
