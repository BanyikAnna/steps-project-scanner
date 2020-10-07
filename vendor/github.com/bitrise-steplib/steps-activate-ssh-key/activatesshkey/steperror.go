package activatesshkey

import "fmt"

// StepError is an error occuring top level in a step
type StepError struct {
	StepID, Tag, ShortMsg string
	Err                   error
}

// NewStepError constructs a git-clone step error
func NewStepError(tag string, err error, shortMsg string) *StepError {
	return &StepError{
		StepID:   "activate-ssh-key",
		Tag:      tag,
		Err:      err,
		ShortMsg: shortMsg,
	}
}

func (e *StepError) Error() string {
	return fmt.Sprintf("%s, %s", e.ShortMsg, e.Err.Error())
}