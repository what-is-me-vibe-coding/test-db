package storage

// sliceIterator iterates over an in-memory slice of ScanEntry.
type sliceIterator struct {
	entries []ScanEntry
	pos     int
}

func newSliceIterator(entries []ScanEntry) *sliceIterator {
	return &sliceIterator{entries: entries, pos: -1}
}

func (it *sliceIterator) Next() bool {
	it.pos++
	return it.pos < len(it.entries)
}

func (it *sliceIterator) Key() string {
	if it.pos < 0 || it.pos >= len(it.entries) {
		return ""
	}
	return it.entries[it.pos].Key
}

func (it *sliceIterator) Entry() ScanEntry {
	if it.pos < 0 || it.pos >= len(it.entries) {
		return ScanEntry{}
	}
	return ScanEntry{Key: it.entries[it.pos].Key, Value: it.entries[it.pos].Value}
}

func (it *sliceIterator) Err() error { return nil }
func (it *sliceIterator) Close()     { it.pos = -1 }
