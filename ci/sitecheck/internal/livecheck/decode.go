package livecheck

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
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
// fields are pointers. When keys are given, the object's own keys must each
// be one of them exactly, case included, and none may appear twice:
// encoding/json alone matches keys without regard to case and keeps the last
// of a duplicated key.
func decodeStrict(r io.Reader, maxBytes int64, v any, keys ...string) error {
	data, err := readCapped(r, maxBytes)
	if err != nil {
		return err
	}

	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return errors.New("not a JSON object")
	}

	if len(keys) > 0 {
		if err := checkKeys(data, keys); err != nil {
			return err
		}
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

// checkKeys returns nil when the keys of the JSON object data are each one of
// keys, exactly, and none appears twice. The values are skipped, not checked.
func checkKeys(data []byte, keys []string) error {
	dec := json.NewDecoder(bytes.NewReader(data))

	if _, err := dec.Token(); err != nil {
		return err
	}

	seen := make(map[string]bool, len(keys))

	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}

		key, ok := tok.(string)
		if !ok {
			return errors.New("not a JSON object")
		}

		switch {
		case !slices.Contains(keys, key):
			return fmt.Errorf("unknown field %q, want exactly %q", key, keys)
		case seen[key]:
			return fmt.Errorf("the field %q appears twice", key)
		}

		seen[key] = true

		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return err
		}
	}

	return nil
}
