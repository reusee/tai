package changes

// ChangeBlock represents a single modification unit parsed from a change
// block emitted by the model. It carries the operation, target, file path,
// body, and optional find string extracted from the block's attributes and
// body. The Find field is used by text-level operations (REPLACE,
// INSERT_BEFORE, INSERT_AFTER) to locate a unique string anchor in the file.
type ChangeBlock struct {
	Op       string
	Target   string
	FilePath string
	Body     string
	Find     string
}
