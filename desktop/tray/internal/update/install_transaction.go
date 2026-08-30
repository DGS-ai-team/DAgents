package update

// installTransaction represents an extracted release that has replaced the
// current installation but has not yet been committed by a successful Node
// restart.
type installTransaction struct {
	commitFn   func()
	rollbackFn func() error
}

func (t installTransaction) Commit() {
	if t.commitFn != nil {
		t.commitFn()
	}
}

func (t installTransaction) Rollback() error {
	if t.rollbackFn == nil {
		return nil
	}
	return t.rollbackFn()
}
