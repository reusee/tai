package changes

// ChangeBlock represents a single modification unit parsed from a change
// block emitted by the model. It carries the operation, target, file path,
// and body extracted from the block's attributes and body.
type ChangeBlock struct {
	Op       string
	Target   string
	FilePath string
	Body     string
}
