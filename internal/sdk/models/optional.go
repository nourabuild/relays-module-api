package models

import "encoding/json"

// Optional distinguishes the three states a PATCH field can be in: absent
// from the request (Set=false, leave unchanged), explicit null (Set=true,
// Value=nil, clear the column), or a value (Set=true, Value set).
type Optional[T any] struct {
	Set   bool
	Value *T
}

// UnmarshalJSON is only invoked when the key is present in the payload, so
// absent fields keep the zero value Set=false.
func (o *Optional[T]) UnmarshalJSON(data []byte) error {
	o.Set = true
	if string(data) == "null" {
		o.Value = nil
		return nil
	}
	return json.Unmarshal(data, &o.Value)
}
