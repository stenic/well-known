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

func (reg wkRegistry) encode() map[string]string {
	d := make(map[string]string, len(reg))

	for name, data := range reg {
		file, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			klog.Error(err)
		} else {
			d[name+".json"] = string(file)
		}
	}

	return d
}

func mergeMapsRecursive(x1, x2 interface{}) interface{} {
	switch x1 := x1.(type) {
	case map[string]interface{}:
		x2, ok := x2.(map[string]interface{})
		if !ok {
			return x1
		}
		for k, v2 := range x2 {
			if v1, ok := x1[k]; ok {
				x1[k] = mergeMapsRecursive(v1, v2)
			} else {
				x1[k] = v2
			}
		}
	case nil:
		// merge(nil, map[string]interface{...}) -> map[string]interface{...}
		x2, ok := x2.(map[string]interface{})
		if ok {
			return x2
		}
	}
	return x1
}

func mergeStructs(x1, x2 interface{}) interface{} {
	if reflect.TypeOf(x1) != reflect.TypeOf(x2) {
		return x1
	}

	switch x1 := x1.(type) {
	case []interface{}:
		x1 = append(x1, x2.([]interface{})...)
	case string:
		x1 = x2.(string)
	case map[string]interface{}:
		x2 := x2.(map[string]interface{})
		for k, v2 := range x2 {
			if v1, ok := x1[k]; ok {
				x1[k] = mergeStructs(v1, v2)
			} else {
				x1[k] = v2
			}
		}
	default:
		klog.Warningf("unknown type: %T", x1)
	}
	return x1
}
