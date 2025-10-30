package logic

import "github.com/pkg/errors"

var (
	ErrVersionConflict = errors.New("optimistic lock version conflict")
)
