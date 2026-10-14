package generators

import (
	"github.com/reusee/dscope"
	"github.com/reusee/tai/apps"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/debugs"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/nets"
)

const TheoryOfGeneratorLogging = `
Generator logging policy:
- Info carries decisions and inputs: config loading, model resolution, the
  generate phase, request parameters, response status, finish reasons, and
  usage. One info stream explains a session from the model name to the flush.
- Debug carries raw payloads: request bodies, stream deltas, and parsed
  content. Debug records are gated by the debug flags because their volume
  grows with the response size.
- Providers obtain the logger as a dscope dependency: a Module method takes
  logs.Logger as a parameter and the closure captures it, so no global
  logger exists, and tests route records to the test output through
  logs.Writer.
- A failure record names the operation and the values that drove it, then
  the error. API-level failures also reach the event recorder; see
  TheoryOfEventRecorder.
- State layers hold no logger: their constructors are exported and consumed
  by the pipeline package, and their user-visible output already prints
  function calls, results, usage, and finish reasons; see
  TheoryOfStateImmutability.
`

type Module struct {
	dscope.Module
	Configs configs.Module
	Nets    nets.Module
	Logs    logs.Module
	Debugs  debugs.Module
	Apps    apps.Module
	Flags   flags.Module
}
