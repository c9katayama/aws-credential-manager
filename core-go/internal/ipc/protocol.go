package ipc

import "encoding/json"

type Request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	ID     string      `json:"id"`
	Result any         `json:"result,omitempty"`
	Error  *ErrorReply `json:"error,omitempty"`
}

type ErrorReply struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func Success(id string, result any) Response {
	return Response{
		ID:     id,
		Result: result,
	}
}

func Failure(id, code, message string) Response {
	return Response{
		ID: id,
		Error: &ErrorReply{
			Code:    code,
			Message: message,
		},
	}
}
