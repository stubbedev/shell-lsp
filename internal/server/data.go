package server

import (
	"encoding/json"
	"fmt"

	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
)

func encodeData[T any](value T) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encoding %T: %w", value, err)
	}
	return data, nil
}

func decodeData[T any](what string, raw json.RawMessage) (T, error) {
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%s data %s is not from this server: %v", what, raw, err)
	}
	return value, nil
}
