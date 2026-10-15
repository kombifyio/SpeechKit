package config

import (
	"maps"
	"reflect"
)

// Clone returns a deep copy of c: maps, slices and pointers are copied, so
// the clone can be read or edited while c changes. The policy binding is
// copied as well, so a clone keeps enforcing the policy c was bound to.
// Clone of nil is nil.
func (c *Config) Clone() *Config {
	if c == nil {
		return nil
	}
	out := new(Config)
	deepCopy(reflect.ValueOf(out).Elem(), reflect.ValueOf(c).Elem())
	out.policy = c.policy.clone()
	return out
}

func (b *policyBinding) clone() *policyBinding {
	if b == nil {
		return nil
	}
	out := &policyBinding{user: b.user.Clone()}
	deepCopy(reflect.ValueOf(&out.values).Elem(), reflect.ValueOf(b.values))
	out.values.origins = maps.Clone(b.values.origins)
	return out
}

// mergeEdited copies onto dst every exported setting (a struct is walked
// field by field; a map, slice, pointer or scalar is one setting) where
// edited differs from base.
func mergeEdited(dst, base, edited *Config) {
	if dst == nil || base == nil || edited == nil {
		return
	}
	mergeValue(reflect.ValueOf(dst).Elem(), reflect.ValueOf(base).Elem(), reflect.ValueOf(edited).Elem())
}

func mergeValue(dst, base, edited reflect.Value) {
	if edited.Kind() == reflect.Struct {
		t := edited.Type()
		for i := range t.NumField() {
			if t.Field(i).IsExported() {
				mergeValue(dst.Field(i), base.Field(i), edited.Field(i))
			}
		}
		return
	}
	if !reflect.DeepEqual(base.Interface(), edited.Interface()) {
		deepCopy(dst, edited)
	}
}

// deepCopy sets dst to a copy of src that shares no map, slice or pointer
// with it. Unexported struct fields are copied by value only; the types that
// carry one (Config.policy, PolicyValues.origins) copy it themselves.
// Interface values (the errors in PolicyRejection) are immutable and shared.
func deepCopy(dst, src reflect.Value) {
	switch src.Kind() {
	case reflect.Struct:
		dst.Set(src)
		t := src.Type()
		for i := range t.NumField() {
			if t.Field(i).IsExported() {
				deepCopy(dst.Field(i), src.Field(i))
			}
		}
	case reflect.Pointer:
		if src.IsNil() {
			dst.SetZero()
			return
		}
		p := reflect.New(src.Type().Elem())
		deepCopy(p.Elem(), src.Elem())
		dst.Set(p)
	case reflect.Slice:
		if src.IsNil() {
			dst.SetZero()
			return
		}
		s := reflect.MakeSlice(src.Type(), src.Len(), src.Len())
		for i := range src.Len() {
			deepCopy(s.Index(i), src.Index(i))
		}
		dst.Set(s)
	case reflect.Array:
		for i := range src.Len() {
			deepCopy(dst.Index(i), src.Index(i))
		}
	case reflect.Map:
		if src.IsNil() {
			dst.SetZero()
			return
		}
		m := reflect.MakeMapWithSize(src.Type(), src.Len())
		iter := src.MapRange()
		for iter.Next() {
			v := reflect.New(src.Type().Elem()).Elem()
			deepCopy(v, iter.Value())
			m.SetMapIndex(iter.Key(), v)
		}
		dst.Set(m)
	default:
		dst.Set(src)
	}
}
