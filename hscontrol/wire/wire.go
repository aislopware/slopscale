// Package wire encodes and decodes the Tailscale control protocol messages
// (MapRequest, MapResponse, RegisterRequest and friends) with
// encoding/json/v2 under v1 semantics: the bytes on the wire stay exactly
// what the Tailscale client's encoding/json produces and expects, while
// the v2 encoder, which compiles a type's layout once instead of walking
// reflection on every call, does the work. Every message on the map and
// registration hot paths goes through here; cold paths such as debug dumps
// may keep encoding/json.
package wire

import (
	jsonv1 "encoding/json"
	"encoding/json/v2"
	"fmt"
	"io"
)

// options is [jsonv1.DefaultOptionsV1] minus map-key sorting. Sorting is
// part of v1 semantics only so that output is reproducible; the client
// does not depend on member order and the sort costs an allocation and a
// comparison pass for every map in every message. TestMarshalMatchesV1
// adds the sort back to compare byte for byte.
var options = json.JoinOptions(jsonv1.DefaultOptionsV1(), json.Deterministic(false))

// Marshal returns the JSON encoding of v.
func Marshal(v any) ([]byte, error) {
	data, err := json.Marshal(v, options)
	if err != nil {
		return nil, fmt.Errorf("encoding %T: %w", v, err)
	}

	return data, nil
}

// MarshalWrite writes the JSON encoding of v to w.
func MarshalWrite(w io.Writer, v any) error {
	err := json.MarshalWrite(w, v, options)
	if err != nil {
		return fmt.Errorf("encoding %T: %w", v, err)
	}

	return nil
}

// Unmarshal decodes data into v.
func Unmarshal(data []byte, v any) error {
	err := json.Unmarshal(data, v, options)
	if err != nil {
		return fmt.Errorf("decoding %T: %w", v, err)
	}

	return nil
}

// UnmarshalRead decodes one JSON value from r into v and reports trailing
// data as an error.
func UnmarshalRead(r io.Reader, v any) error {
	err := json.UnmarshalRead(r, v, options)
	if err != nil {
		return fmt.Errorf("decoding %T: %w", v, err)
	}

	return nil
}
