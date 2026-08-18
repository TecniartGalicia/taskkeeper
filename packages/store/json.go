package store

import "encoding/json"

func jsonMarshalString(s string) (string, error) {
	b, err := json.Marshal(s)
	return string(b), err
}
