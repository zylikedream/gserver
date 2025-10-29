package logic

import "github.com/pkg/errors"

var (
	ErrVersionConflict = errors.New("optimistic lock version conflict")
)

const (
	ROLE_GRAIN_TYPE = "role"
)
