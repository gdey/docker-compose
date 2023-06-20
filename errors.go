package dockercompose

import (
	"bytes"
	"errors"
	"fmt"
)

var (
	ErrServiceNameRequired = errors.New("service name required")
)

type ErrInvalidPortEntry string

func (err ErrInvalidPortEntry) Error() string {
	return fmt.Sprintf("invalid port entry: %s", string(err))
}

type ErrFailedToStartService struct {
	Service string
	Log     *bytes.Buffer
	Err     error
}

func (err ErrFailedToStartService) Unwrap() error {
	return err.Err
}

func (err ErrFailedToStartService) Error() string {
	return fmt.Sprintf("failed to start service %v", err.Service)
}

type ErrFailedToStopService struct {
	Service string
	Log     *bytes.Buffer
	Err     error
}

func (err ErrFailedToStopService) Unwrap() error {
	return err.Err
}

func (err ErrFailedToStopService) Error() string {
	return fmt.Sprintf("failed to stop service %v", err.Service)
}

type ErrDockerCmd struct {
	Log *bytes.Buffer
	Err error
}

func (err ErrDockerCmd) Error() string {
	return fmt.Sprintf("%v:\n%v", err.Err.Error(), err.Log.String())
}

func (err ErrDockerCmd) Unwrap() error {
	return err.Err
}
