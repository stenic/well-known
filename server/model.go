package main

import (
	"encoding/json"
	"reflect"

	klog "k8s.io/klog/v2"
)

type wkData map[string]interface{}

func (d wkData) append(data map[string]interface{}) {
	for k, v := range data {
		if _, ok := d[k]; ok {
			d[k] = mergeStructs(d[k], v)
		} else {
			d[k] = v
		}
	}
}

type wkRegistry map[string]wkData

func (reg wkRegistry) encode() (map[string]string, error) {
	d := make(map[string]string, len(reg))

	for name, data := range reg {
		file, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return nil, err
		}
		d[name+".json"] = string(file)
	}

	return d, nil
}

func mergeStructs(x1, x2 interface{}) interface{} {
	if reflect.TypeOf(x1) != reflect.TypeOf(x2) {
		return x1
	}

	switch value := x1.(type) {
	case []interface{}:
		x1 = append(value, x2.([]interface{})...)
	case string:
		x1 = x2.(string)
	case map[string]interface{}:
		x2 := x2.(map[string]interface{})
		for k, v2 := range x2 {
			if v1, ok := value[k]; ok {
				value[k] = mergeStructs(v1, v2)
			} else {
				value[k] = v2
			}
		}
	default:
		klog.Warningf("unknown type: %T", value)
	}
	return x1
}
