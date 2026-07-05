package main

import (
	"fmt"
	"path"
	"runtime"
	"strings"
)

type Frame struct {
	file     string
	line     int
	funcName string
	cause    error
}

func (frame Frame) Error() string {
	return fmt.Sprintf("%v in function %v, line %v:\n%v", frame.file, frame.funcName, frame.line, frame.cause.Error())
}

type Stack struct {
	errors []Frame
}

func (stack Stack) Origin() error {
	return stack.errors[0].cause
}

func (stack Stack) Error() string {
	var s strings.Builder
	for i, f := range stack.errors {
		s.WriteString(f.Error())
		if i < len(stack.errors)-1 {
			s.WriteString("\n")
		}
	}
	return s.String()
}

func AddError(cause error) Stack {
	pc, file, line, ok := runtime.Caller(1)
	file = path.Base(file)
	switch c := cause.(type) {
	case Stack:
		if !ok {
			return Stack{
				append(c.errors, Frame{"", 0, "", cause}),
			}
		}
		return Stack{
			append(c.errors, Frame{file, line, runtime.FuncForPC(pc).Name(), cause}),
		}
	default:
		if !ok {
			return Stack{
				[]Frame{
					{
						"", 0, "", cause,
					},
				},
			}
		}
		return Stack{
			[]Frame{
				{file, line, runtime.FuncForPC(pc).Name(), cause},
			},
		}
	}
}
