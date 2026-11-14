package heapref

// FuncID names a function held by a Funcs in four bytes and may be stored
// off-heap. The zero FuncID is nil.
type FuncID[F any] uint32

// IsNil reports whether id refers to nothing.
func (id FuncID[F]) IsNil() bool { return id == 0 }

// Funcs registers functions of type F for good and names each by a FuncID, for
// callbacks named from off-heap memory. Registering the same function twice
// yields two IDs: func values cannot be compared, and two closures over different
// variables share their code, so register each function once, typically at
// initialisation, and keep its ID. The zero Funcs is empty and ready to use; it
// must not be copied after first use. It is not safe for concurrent use.
type Funcs[F any] struct {
	t Table[F]
}

// Len returns the number of registered functions.
func (fs *Funcs[F]) Len() int { return fs.t.Len() }

// Register stores f and returns its FuncID. It fails as Table.Put does.
func (fs *Funcs[F]) Register(f F) (FuncID[F], error) {
	r, err := fs.t.Put(f)
	return FuncID[F](r.slot), err
}

// Get returns the function id names. It panics if id is nil, and with the
// guards compiled in, if id comes from another Funcs with more functions.
func (fs *Funcs[F]) Get(id FuncID[F]) F {
	return fs.t.Get(Ref[F]{slot: uint32(id), gen: fs.t.id})
}
