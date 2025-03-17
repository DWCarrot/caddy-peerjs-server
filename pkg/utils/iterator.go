package utils

import "iter"

type Iterable[V any] interface {

	// Get an iterator for the collection
	// Returns: iter.
	Iterator() iter.Seq[V]
}

type Iterable2[K any, V any] interface {

	// Get an iterator for the collection
	// Returns: iter.
	Iterator() iter.Seq2[K, V]
}

type IteratorTransform[I any, O any] struct {
	Inner     Iterable[I]
	Transform func(I) O
}

func (it *IteratorTransform[I, O]) Iterator() iter.Seq[O] {
	return func(yield func(O) bool) {
		it.Inner.Iterator()(func(x I) bool {
			return yield(it.Transform(x))
		})
	}
}
