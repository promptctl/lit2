package git

// The refusals the external tests name, which no package outside git
// needs.
var (
	ErrNoGit        = errNoGit
	ErrNoRepository = errNoRepository
	ErrForeignShape = errForeignShape
	ErrHeadMissing  = errHeadMissing
	ErrShallow      = errShallow
	ErrNoFormat     = errNoFormat
	ErrSumSize      = errSumSize
)
