package domain

import "fmt"

type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string   { return e.Code + ": " + e.Message }
func Err(code, msg string) error { return &Error{Code: code, Message: msg} }
func Conflict(msg string) error  { return Err("CONFLICT", msg) }
func Invalid(msg string) error   { return Err("INVALID", msg) }
func NotFound(id string) error   { return Err("NOT_FOUND", fmt.Sprintf("case %s not found", id)) }
