package livecheck

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// readCapped reads all of r, at most maxBytes: more is an error. It reads at
// most maxBytes+1 bytes of r.
func readCapped(r io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, err
	}

	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("longer than %d bytes", maxBytes)
	}

	return data, nil
}

// decodeStrict decodes exactly one JSON object from r, at most maxBytes, into
// v: an unknown field, a value of the wrong type or anything but white space
// after the object is an error. Missing fields are left to the caller, whose
// fields are pointers.
func decodeStrict(r io.Reader, maxBytes int64, v any) error {
	data, err := readCapped(r, maxBytes)
	if err != nil {
		return err
	}

	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return errors.New("not a JSON object")
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	if err := dec.Decode(v); err != nil {
		return err
	}

	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("data after the JSON object")
	}

	return nil
}
