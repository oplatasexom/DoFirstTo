package ui

import (
	"context"
	"errors"
)

type NoUpdates struct{}

func (NoUpdates) Check(context.Context) (UpdateInfo, error) {
	return UpdateInfo{}, nil
}

func (NoUpdates) Apply(context.Context) error {
	return errors.New("updates are not configured")
}
