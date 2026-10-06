package fix

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// omap is a JSON object that keeps its member order, so a merge into a
// hand-written manifest adds keys without reshuffling the ones it has.
// Numbers stay json.Number and are written back as they were.
type omap struct {
	keys []string
	vals map[string]any
}

func (o *omap) UnmarshalJSON(b []byte) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok != json.Delim('{') {
		return errors.New("not a JSON object")
	}
	o.keys, o.vals = nil, map[string]any{}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := keyTok.(string)
		if !ok {
			return fmt.Errorf("object key %v is not a string", keyTok)
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		val, err := decodeOrdered(raw)
		if err != nil {
			return err
		}
		if _, dup := o.vals[key]; !dup {
			o.keys = append(o.keys, key)
		}
		o.vals[key] = val
	}
	_, err = dec.Token() // the closing brace
	return err
}

// decodeOrdered turns a raw value into an omap for objects, a slice with
// ordered elements for arrays, and a scalar otherwise.
func decodeOrdered(raw json.RawMessage) (any, error) {
	trimmed := bytes.TrimSpace(raw)
	switch {
	case len(trimmed) > 0 && trimmed[0] == '{':
		var m omap
		if err := json.Unmarshal(trimmed, &m); err != nil {
			return nil, err
		}
		return &m, nil
	case len(trimmed) > 0 && trimmed[0] == '[':
		var items []json.RawMessage
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return nil, err
		}
		out := make([]any, 0, len(items))
		for _, it := range items {
			v, err := decodeOrdered(it)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

func (o *omap) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		b.Write(kb)
		b.WriteByte(':')
		vb, err := json.Marshal(o.vals[k])
		if err != nil {
			return nil, err
		}
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// set adds or replaces a member, keeping the position of an existing one.
func (o *omap) set(key string, v any) {
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = v
}
