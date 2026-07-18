package generators

const TheoryOfOutputColors = `
Terminal output uses ANSI colors to visually distinguish content roles. Each role
has a dedicated color to aid quick visual scanning. Thoughts use a distinct light
color separate from all role colors, so reasoning content stands out without
conflicting with role-based coloring. Color codes are only applied when the output
writer is a terminal; non-terminal output (pipes, files) remains uncolored.
`

const (
	ColorReset   = "\033[0m"
	ColorUser    = "\033[34m" // Blue
	ColorTool    = "\033[33m" // Yellow
	ColorSystem  = "\033[36m" // Cyan
	ColorThought = "\033[95m" // Bright magenta, a light color for thoughts
	ColorLog     = "\033[31m" // Red
)
