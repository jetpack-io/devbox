package cuecfg

import (
	"encoding/xml"
)

func marshalXML(v interface{}) ([]byte, error) {
	return xml.Marshal(v)
}

func unmarshalXML(data []byte, v interface{}) error {
	return xml.Unmarshal(data, v)
}
