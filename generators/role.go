package generators

type Role string

const (
	RoleUser      Role = "user"
	RoleSystem    Role = "system"
	RoleAssistant Role = "assistant" // for OpenAI
	RoleModel     Role = "model"     // for Gemini
	RoleTool      Role = "tool"
	RoleLog       Role = "log"
)
