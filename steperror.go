package main

import "github.com/bitrise-io/bitrise-init/step"

func newStepError(tag string, err error, shortMsg string) *step.Error {
	return step.NewError("project-scanner", tag, err, shortMsg)
}
