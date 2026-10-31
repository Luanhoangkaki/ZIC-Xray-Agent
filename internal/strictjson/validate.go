// Package strictjson rejects duplicate keys and excessive nesting before a
// configuration can acquire a different meaning in another decoder/component.
package strictjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

func Validate(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	budget := 250000
	var value func(int) error
	value = func(depth int) error {
		budget--
		if depth > 32 || budget < 0 {
			return errors.New("JSON nesting/token limit exceeded")
		}
		token, err := d.Token()
		if err != nil {
			return errors.New("invalid JSON")
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			keys := make(map[string]bool)
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return errors.New("invalid JSON key")
				}
				name, ok := key.(string)
				if !ok || keys[name] {
					return errors.New("duplicate JSON key")
				}
				keys[name] = true
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		default:
			return errors.New("unexpected JSON delimiter")
		}
		end, err := d.Token()
		if err != nil {
			return errors.New("invalid JSON container")
		}
		if (delim == '{' && end != json.Delim('}')) || (delim == '[' && end != json.Delim(']')) {
			return errors.New("invalid JSON container")
		}
		return nil
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("extra JSON content")
	}
	return nil
}
