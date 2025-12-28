package taivm

type ListIterator struct {
	List *List
	Idx  int
}

func (it *ListIterator) GetIndex(key any) (any, bool) {
	var idx int
	switch k := key.(type) {
	case int:
		idx = k
	case int64:
		idx = int(k)
	default:
		return nil, false
	}
	if it.List == nil || idx < 0 || idx >= len(it.List.Elements) {
		return nil, false
	}
	return it.List.Elements[idx], true
}

type MapIterator struct {
	Map  any
	Keys []any
	Idx  int
}

func (it *MapIterator) GetIndex(key any) (any, bool) {
	switch m := it.Map.(type) {
	case map[any]any:
		v, ok := m[key]
		return v, ok
	case map[string]any:
		if s, ok := key.(string); ok {
			v, ok := m[s]
			return v, ok
		}
	}
	return nil, false
}

type FuncIterator struct {
	InnerVM *VM
	K       any
	V       any
	Done    bool
	Resumed bool
}

func (it *FuncIterator) GetIndex(key any) (any, bool) {
	return it.V, true
}
