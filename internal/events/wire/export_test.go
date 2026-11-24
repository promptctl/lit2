package wire

// The reader's refusals and the writer's precondition, for the tests that
// name them.
var (
	ErrFieldNumber  = errFieldNumber
	ErrKeyNotVarint = errKeyNotVarint
	ErrTornVarint   = errTornVarint
	ErrLengthPast   = errLengthPast
	ErrWireType     = errWireType
	ErrReadAsOther  = errReadAsOther
)
