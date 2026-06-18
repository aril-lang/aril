package arilrt

import "encoding/json"

// json.go — JSONParse backs json.parse<T> (binding-surface.md
// §encoding/json), and Option carries Marshal/Unmarshal so an
// Option-typed field round-trips with *bare* JSON — None ⇄ null,
// Some(v) ⇄ the JSON of v — rather than exposing the internal Tag/V
// shape (lowering-go.md §Container types).

// JSONParse unmarshals data into a fresh T, folding (value, error) into
// Result<T, error>.
func JSONParse[T any](data []byte) Result[T, error] {
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return Result[T, error]{Tag: 1, E: err}
	}
	return Result[T, error]{Tag: 0, V: v}
}

func (o Option[T]) MarshalJSON() ([]byte, error) {
	if o.Tag == 0 {
		return []byte("null"), nil
	}
	return json.Marshal(o.V)
}

func (o *Option[T]) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		var zero T
		o.Tag, o.V = 0, zero
		return nil
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	o.Tag, o.V = 1, v
	return nil
}
