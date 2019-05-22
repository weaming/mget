package main

type FileBrokenError struct {
	msg string // description of error
}

func (e *FileBrokenError) Error() string {
	return e.msg
}
